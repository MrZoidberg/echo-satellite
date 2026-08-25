package wake

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
	"golang.org/x/sys/unix"
)

const DefaultRoot = "/data/local/etc/echo-satellite/wake-models"

var (
	ErrModelNotFound       = errors.New("wake: model not found")
	ErrDigestMismatch      = errors.New("wake: digest mismatch")
	ErrUnsupportedModelOps = errors.New("wake: unsupported model operators")
)

type Store struct{ Root string }

type InstallRequest struct {
	ID             string
	SourcePath     string
	SidecarPath    string
	ExpectedSHA256 string
	Overwrite      bool
}

type storeIndex struct {
	Schema int          `json:"schema"`
	Models []indexModel `json:"models"`
}

type indexModel struct {
	Sidecar
	Artifact string `json:"artifact"`
	Metadata string `json:"metadata"`
}

func (s Store) List() ([]Model, error) {
	index, err := s.readIndex()
	if errors.Is(err, os.ErrNotExist) {
		return []Model{}, nil
	}
	if err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(index.Models))
	for _, entry := range index.Models {
		if schemaErr := entry.validateSchema(); schemaErr != nil {
			return nil, fmt.Errorf("index model %q: %w", entry.ID, schemaErr)
		}
		if !validIndexFilename(entry.Artifact) {
			return nil, fmt.Errorf("index model %q: %w: invalid artifact path", entry.ID, ErrInvalidModel)
		}
		if !validIndexFilename(entry.Metadata) {
			return nil, fmt.Errorf("index model %q: %w: invalid metadata path", entry.ID, ErrInvalidModel)
		}
		model, modelErr := entry.Model(filepath.Join(s.root(), entry.Artifact))
		if modelErr != nil {
			return nil, fmt.Errorf("index model %q: %w", entry.ID, modelErr)
		}
		_, info, _, verifyErr := readVerifiedModel(model.Path, model.SHA256)
		if verifyErr != nil {
			return nil, fmt.Errorf("index model %q: %w", entry.ID, verifyErr)
		}
		if model.Size != info.Size() {
			return nil, fmt.Errorf("index model %q: %w: size got %d, want %d", entry.ID, ErrInvalidModel, info.Size(), model.Size)
		}
		models = append(models, model)
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}

func (s Store) Get(id string) (Model, error) {
	models, err := s.List()
	if err != nil {
		return Model{}, err
	}
	for _, model := range models {
		if model.ID == id {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("%w: %s", ErrModelNotFound, id)
}

func (s Store) SharedPath(name string) string {
	return filepath.Join(s.root(), filepath.Base(name))
}

func (s Store) Install(request InstallRequest) (Model, error) {
	model, sidecar, raw, err := s.prepareInstall(request)
	if err != nil {
		return Model{}, err
	}
	if createErr := os.MkdirAll(s.root(), 0o750); createErr != nil {
		return Model{}, fmt.Errorf("create model store: %w", createErr)
	}
	lock, err := s.lock()
	if err != nil {
		return Model{}, err
	}
	defer lock.close()

	index, err := s.readIndex()
	if errors.Is(err, os.ErrNotExist) {
		index = storeIndex{Schema: 1}
	} else if err != nil {
		return Model{}, err
	}
	for _, installed := range index.Models {
		if installed.ID == model.ID && !request.Overwrite {
			return Model{}, fmt.Errorf("model %q already exists", model.ID)
		}
	}

	digestTag := model.SHA256
	artifact := model.ID + "." + digestTag + ".tflite"
	metadataName := model.ID + "." + digestTag + ".json"
	model.Path = filepath.Join(s.root(), artifact)
	metadata, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return Model{}, fmt.Errorf("marshal model metadata: %w", err)
	}
	metadata = append(metadata, '\n')
	if err := publishImmutable(s.modelPath(model.ID)+".part", model.Path, raw, 0o644); err != nil {
		return Model{}, err
	}
	metadataPath := filepath.Join(s.root(), metadataName)
	if err := publishImmutable(s.sidecarPath(model.ID)+".part", metadataPath, metadata, 0o644); err != nil {
		return Model{}, err
	}
	entry := indexModel{Sidecar: sidecar, Artifact: artifact, Metadata: metadataName}
	if err := s.updateIndex(index, entry); err != nil {
		return Model{}, err
	}
	return model, nil
}

func (s Store) prepareInstall(request InstallRequest) (Model, Sidecar, []byte, error) {
	if err := validateLocalSource(request.SourcePath); err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	sidecarPath := request.SidecarPath
	if sidecarPath == "" {
		sidecarPath = strings.TrimSuffix(request.SourcePath, filepath.Ext(request.SourcePath)) + ".json"
	}
	sidecarData, err := os.ReadFile(sidecarPath) //nolint:gosec // operator explicitly selects local metadata.
	if err != nil {
		return Model{}, Sidecar{}, nil, fmt.Errorf("read model metadata %q: %w", sidecarPath, err)
	}
	sidecar, err := ParseSidecar(sidecarData)
	if err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	if request.ID == "" || request.ID != sidecar.ID {
		return Model{}, Sidecar{}, nil, fmt.Errorf("%w: requested id %q, sidecar id %q", ErrSidecarMismatch, request.ID, sidecar.ID)
	}
	expected := strings.ToLower(request.ExpectedSHA256)
	if expected == "" {
		expected = strings.ToLower(sidecar.SHA256)
	}
	if expected == "" {
		return Model{}, Sidecar{}, nil, fmt.Errorf("%w: expected sha256 is required", ErrInvalidModel)
	}
	if sidecar.SHA256 != "" && !strings.EqualFold(expected, sidecar.SHA256) {
		return Model{}, Sidecar{}, nil, fmt.Errorf("%w: command digest differs from sidecar", ErrDigestMismatch)
	}

	raw, info, digest, err := readVerifiedModel(request.SourcePath, expected)
	if err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	parsed, err := tflite.Parse(raw)
	if err != nil {
		return Model{}, Sidecar{}, nil, fmt.Errorf("parse wake model: %w", err)
	}
	detected, err := detectModelKind(parsed)
	if err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	declared, err := ParseKind(sidecar.Kind)
	if err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	if detected != declared {
		return Model{}, Sidecar{}, nil, fmt.Errorf("%w: tensor shape is %s, sidecar declares %s", ErrSidecarMismatch, detected, declared)
	}
	if unsupported := parsed.UnsupportedOpcodes(); len(unsupported) != 0 {
		return Model{}, Sidecar{}, nil, fmt.Errorf("%w: %s", ErrUnsupportedModelOps, strings.Join(unsupported, ", "))
	}

	sidecar.SHA256, sidecar.Size = digest, info.Size()
	model, err := sidecar.Model("")
	if err != nil {
		return Model{}, Sidecar{}, nil, err
	}
	return model, sidecar, raw, nil
}

func validateLocalSource(path string) error {
	if path == "" {
		return fmt.Errorf("%w: source path is required", ErrInvalidModel)
	}
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme != "" {
		return fmt.Errorf("%w: URL sources are forbidden", ErrInvalidModel)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect local model source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: source is not a regular local file", ErrInvalidModel)
	}
	return nil
}

func readVerifiedModel(path, expected string) ([]byte, os.FileInfo, string, error) {
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != sha256.Size {
		return nil, nil, "", fmt.Errorf("%w: expected sha256 must be 64 hexadecimal characters", ErrInvalidModel)
	}
	file, err := os.Open(path) //nolint:gosec // operator explicitly selects a local model.
	if err != nil {
		return nil, nil, "", fmt.Errorf("open local model source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, "", fmt.Errorf("stat local model source: %w", err)
	}
	hash := sha256.New()
	raw, err := io.ReadAll(io.TeeReader(file, hash))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read local model source: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return nil, nil, "", fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, actual, expected)
	}
	return raw, info, actual, nil
}

func detectModelKind(model *tflite.Model) (Kind, error) {
	if len(model.Subgraphs) == 0 || len(model.Subgraphs[0].Inputs) == 0 {
		return KindUnknown, fmt.Errorf("%w: classifier has no input", ErrUnknownModelKind)
	}
	graph := model.Subgraphs[0]
	input := graph.Inputs[0]
	if input < 0 || input >= len(graph.Tensors) {
		return KindUnknown, fmt.Errorf("%w: classifier input index %d", ErrUnknownModelKind, input)
	}
	shape := graph.Tensors[input].Shape
	if len(shape) == 3 && shape[0] == 1 && shape[1] > 0 && shape[2] == 96 {
		return KindOpenWakeWord, nil
	}
	return KindUnknown, fmt.Errorf("%w: classifier input shape %v", ErrUnknownModelKind, shape)
}

func (s Store) updateIndex(index storeIndex, entry indexModel) error {
	replaced := false
	for i := range index.Models {
		if index.Models[i].ID == entry.ID {
			index.Models[i], replaced = entry, true
		}
	}
	if !replaced {
		index.Models = append(index.Models, entry)
	}
	slices.SortFunc(index.Models, func(a, b indexModel) int { return strings.Compare(a.ID, b.ID) })
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model index: %w", err)
	}
	return writeStaged(filepath.Join(s.root(), "index.json"), append(raw, '\n'), 0o644)
}

func (s Store) readIndex() (storeIndex, error) {
	raw, err := os.ReadFile(filepath.Join(s.root(), "index.json"))
	if err != nil {
		return storeIndex{}, fmt.Errorf("read model index: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var index storeIndex
	if err = decoder.Decode(&index); err != nil {
		return storeIndex{}, fmt.Errorf("parse model index: %w", err)
	}
	if err = ensureJSONEOF(decoder); err != nil {
		return storeIndex{}, fmt.Errorf("parse model index: %w", err)
	}
	if index.Schema != 1 {
		return storeIndex{}, fmt.Errorf("parse model index: unsupported schema %d", index.Schema)
	}
	return index, nil
}

func writeStaged(path string, data []byte, mode os.FileMode) error {
	part := path + ".part"
	if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale staging file %q: %w", part, err)
	}
	// O_EXCL refuses a symlink planted between removal and creation instead of following it while
	// echoctl is privileged. It also makes concurrent installers fail safely rather than interleave.
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // O_EXCL prevents following a planted staging symlink.
	if err != nil {
		return fmt.Errorf("create staging file %q: %w", part, err)
	}
	defer func() { _ = os.Remove(part) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write staging file %q: %w", part, err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync staging file %q: %w", part, err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close staging file %q: %w", part, err)
	}
	if err = os.Rename(part, path); err != nil {
		return fmt.Errorf("promote staging file %q: %w", part, err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open model store for sync: %w", err)
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync model store: %w", err)
	}
	return nil
}

func publishImmutable(part, destination string, data []byte, mode os.FileMode) error {
	if _, err := os.Stat(destination); err == nil {
		existing, readErr := os.ReadFile(destination) //nolint:gosec // destination is a validated immutable store path.
		if readErr != nil {
			return fmt.Errorf("read immutable model asset: %w", readErr)
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("immutable model asset %q exists with different contents", destination)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect immutable model asset: %w", err)
	}
	if err := writeStagedTo(part, destination, data, mode); err != nil {
		return err
	}
	return nil
}

func writeStagedTo(part, destination string, data []byte, mode os.FileMode) error {
	if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale staging file %q: %w", part, err)
	}
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // O_EXCL prevents following a planted staging symlink.
	if err != nil {
		return fmt.Errorf("create staging file %q: %w", part, err)
	}
	defer func() { _ = os.Remove(part) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write staging file %q: %w", part, err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync staging file %q: %w", part, err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close staging file %q: %w", part, err)
	}
	if err = os.Rename(part, destination); err != nil {
		return fmt.Errorf("promote staging file %q: %w", part, err)
	}
	return syncStoreDirectory(filepath.Dir(destination))
}

type storeLock struct{ file *os.File }

func (s Store) lock() (*storeLock, error) {
	file, err := os.OpenFile(filepath.Join(s.root(), ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open model store lock: %w", err)
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock model store: %w", err)
	}
	return &storeLock{file: file}, nil
}

func (l *storeLock) close() {
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	_ = l.file.Close()
}

func syncStoreDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // path is the parent of a validated store publication target.
	if err != nil {
		return fmt.Errorf("open model store for sync: %w", err)
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync model store: %w", err)
	}
	return nil
}

func (s Store) root() string {
	if s.Root == "" {
		return DefaultRoot
	}
	return s.Root
}

func (s Store) modelPath(id string) string   { return filepath.Join(s.root(), id+".tflite") }
func (s Store) sidecarPath(id string) string { return filepath.Join(s.root(), id+".json") }

func validIndexFilename(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}
