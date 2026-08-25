package wake

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const SidecarSchemaVersion = 1

var (
	ErrInvalidModel    = errors.New("wake: invalid model")
	ErrSidecarMismatch = errors.New("wake: sidecar mismatch")
)

// Model is one installed wake classifier and its operator-visible metadata.
type Model struct {
	ID         string
	Path       string
	Kind       Kind
	Phrase     string
	Languages  []string
	SampleRate int
	SHA256     string
	Size       int64
	Source     string
	License    string
}

// Sidecar is the stable JSON metadata stored alongside a classifier.
type Sidecar struct {
	Schema     int      `json:"schema"`
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Phrase     string   `json:"phrase,omitempty"`
	Languages  []string `json:"languages,omitempty"`
	SampleRate int      `json:"sample_rate,omitempty"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size,omitempty"`
	Source     string   `json:"source,omitempty"`
	License    string   `json:"license,omitempty"`
}

// ParseSidecar strictly parses trusted model metadata.
func ParseSidecar(data []byte) (Sidecar, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var sidecar Sidecar
	if err := decoder.Decode(&sidecar); err != nil {
		return Sidecar{}, fmt.Errorf("%w: decode sidecar: %w", ErrInvalidModel, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Sidecar{}, err
	}
	if err := sidecar.validateSchema(); err != nil {
		return Sidecar{}, err
	}
	if _, err := sidecar.Model(""); err != nil {
		return Sidecar{}, err
	}
	return sidecar, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", ErrInvalidModel)
		}
		return fmt.Errorf("%w: trailing JSON: %w", ErrInvalidModel, err)
	}
	return nil
}

// Model converts sidecar metadata to the runtime representation.
func (s Sidecar) Model(path string) (Model, error) {
	kind, err := ParseKind(s.Kind)
	if err != nil {
		return Model{}, err
	}
	model := Model{
		ID: s.ID, Path: path, Kind: kind, Phrase: s.Phrase,
		Languages: append([]string(nil), s.Languages...), SampleRate: s.SampleRate,
		SHA256: strings.ToLower(s.SHA256), Size: s.Size, Source: s.Source, License: s.License,
	}
	if err := model.Validate(); err != nil {
		return Model{}, err
	}
	return model, nil
}

// Validate checks metadata before it can influence filesystem paths or runtime selection.
func (m Model) Validate() error {
	if m.ID == "" || filepath.Base(m.ID) != m.ID || m.ID == "." || m.ID == ".." {
		return fmt.Errorf("%w: invalid id %q", ErrInvalidModel, m.ID)
	}
	switch m.ID {
	case "index", "melspectrogram", "embedding_model":
		return fmt.Errorf("%w: id %q is reserved by the model store", ErrInvalidModel, m.ID)
	}
	if m.Kind != KindOpenWakeWord && m.Kind != KindMicroWakeWord {
		return fmt.Errorf("%w: unknown kind %d", ErrInvalidModel, m.Kind)
	}
	if m.SampleRate != 0 && m.SampleRate != SampleRate {
		return fmt.Errorf("%w: sample rate %d, want %d", ErrInvalidModel, m.SampleRate, SampleRate)
	}
	decoded, err := hex.DecodeString(m.SHA256)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("%w: sha256 must be 64 hexadecimal characters", ErrInvalidModel)
	}
	if m.Size < 0 {
		return fmt.Errorf("%w: negative size %d", ErrInvalidModel, m.Size)
	}
	return nil
}

func (s Sidecar) validateSchema() error {
	if s.Schema != SidecarSchemaVersion {
		return fmt.Errorf("%w: unsupported sidecar schema %d", ErrInvalidModel, s.Schema)
	}
	return nil
}
