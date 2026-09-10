// Package update stages verified Echo Satellite agent releases and atomically
// replaces the one installed executable. It deliberately provides no rollback:
// a successful Rename is the irreversible installation commit point.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

const (
	// DefaultAgentPath is the only executable an ordinary device deployment replaces.
	DefaultAgentPath = "/data/local/bin/echod"
	// DefaultMetadataPath is diagnostic state, never a recovery authority.
	DefaultMetadataPath = "/data/local/etc/echo-satellite/installed-release.json"
	maxControlBytes     = 1 << 20
	minimumSpaceMargin  = 16 << 20
	maxInt64            = int64(^uint64(0) >> 1)
)

var (
	ErrBusy              = errors.New("update: device is busy")
	ErrCommitted         = errors.New("update: agent replacement committed; manual recovery may be required")
	ErrInvalidOffer      = errors.New("update: invalid offer")
	ErrInsufficientSpace = errors.New("update: insufficient free space")
	ErrOfferMismatch     = errors.New("update: offer does not match signed manifest")
	ErrTooLarge          = errors.New("update: artifact exceeds declared maximum size")
	ErrDownloadFailed    = errors.New("update: download failed")
)

// Downloader reuses the paired gateway's TLS and authentication configuration.
// Its implementation must not log request URLs or headers because those may
// contain deployment-scoped credentials.
type Downloader interface {
	Fetch(context.Context, string) (io.ReadCloser, error)
}

// File is the narrow set of file operations required by an installation.
type File interface {
	io.Reader
	io.Writer
	Sync() error
	Close() error
}

// FileSystem is owned by this consumer so failure paths are host-testable.
type FileSystem interface {
	CreateTemp(dir, pattern string, perm fs.FileMode) (File, string, error)
	Open(string) (File, error)
	ReadFile(string) ([]byte, error)
	Rename(oldpath, newpath string) error
	Remove(string) error
	Chmod(string, fs.FileMode) error
	SyncDir(string) error
}

// Space reports available bytes in the target filesystem.
type Space interface{ Available(string) (int64, error) }

// Clock supplies diagnostic timestamps.
type Clock interface{ Now() time.Time }

// Trust verifies a parsed manifest and its detached signature.
type Trust interface {
	Check(release.Manifest, []byte) error
}

// VoiceState reports whether a voice turn currently owns the device.
type VoiceState interface{ Active() bool }

// Config contains immutable installer dependencies. TargetPath and
// MetadataPath default to the standard device paths.
type Config struct {
	Downloader Downloader
	FS         FileSystem
	Space      Space
	Clock      Clock
	Trust      Trust
	Voice      VoiceState
	Device     release.Device

	TargetPath   string
	MetadataPath string
	// GatewayAuthority is the paired gateway authority (host[:port]). Every
	// offered resource must remain on this authority.
	GatewayAuthority string
	MaxArtifactSize  int64
	DirectorySync    bool
}

// Installer serializes offers for one device.
type Installer struct {
	config Config
	mu     sync.Mutex
	active bool
}

// Metadata records what was installed for diagnostics. It is intentionally not
// used to select a binary or recover an installation.
type Metadata struct {
	Schema       int       `json:"schema"`
	Version      string    `json:"version"`
	BuildID      string    `json:"build_id"`
	Architecture string    `json:"architecture"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256"`
	InstalledAt  time.Time `json:"installed_at"`
}

// Result says whether the replacement rename happened. If Committed is true,
// callers must never claim that the prior executable remains installed.
type Result struct {
	Committed bool
	Metadata  Metadata
}

// New validates the static dependencies of an installer.
func New(config Config) (*Installer, error) {
	if config.Downloader == nil || config.FS == nil || config.Space == nil || config.Clock == nil || config.Trust == nil {
		return nil, errors.New("update: downloader, filesystem, space, clock, and trust are required")
	}
	if config.TargetPath == "" {
		config.TargetPath = DefaultAgentPath
	}
	if config.MetadataPath == "" {
		config.MetadataPath = DefaultMetadataPath
	}
	if !filepath.IsAbs(config.TargetPath) || !filepath.IsAbs(config.MetadataPath) {
		return nil, errors.New("update: target and metadata paths must be absolute")
	}
	if config.MaxArtifactSize <= 0 {
		return nil, errors.New("update: maximum artifact size must be positive")
	}
	if config.Device.Architecture == "" {
		return nil, errors.New("update: device architecture is required")
	}
	if _, err := canonicalAuthority(config.GatewayAuthority); err != nil {
		return nil, err
	}
	return &Installer{config: config}, nil
}

// Install verifies and stages offer. Cancellation is honored until Rename;
// after it succeeds errors are wrapped in ErrCommitted.
func (i *Installer) Install(ctx context.Context, offer protocol.UpdateOffer) (Result, error) {
	if !i.acquire() {
		return Result{}, ErrBusy
	}
	defer i.release()
	if i.config.Voice != nil && i.config.Voice.Active() {
		return Result{}, ErrBusy
	}
	manifest, err := i.preflight(ctx, offer)
	if err != nil {
		return Result{}, err
	}
	return i.stageAndCommit(ctx, offer, manifest)
}

func (i *Installer) preflight(ctx context.Context, offer protocol.UpdateOffer) (release.Manifest, error) {
	if err := offer.Validate(); err != nil {
		return release.Manifest{}, fmt.Errorf("%w: %w", ErrInvalidOffer, err)
	}
	if err := i.validateOfferURLs(offer); err != nil {
		return release.Manifest{}, err
	}
	if offer.Size > i.config.MaxArtifactSize {
		return release.Manifest{}, fmt.Errorf("%w: %d > %d", ErrTooLarge, offer.Size, i.config.MaxArtifactSize)
	}

	manifest, err := i.fetchManifest(ctx, offer.ManifestURL)
	if err != nil {
		return release.Manifest{}, err
	}
	signature, err := i.fetchControl(ctx, offer.SignatureURL)
	if err != nil {
		return release.Manifest{}, err
	}
	trustErr := i.config.Trust.Check(manifest, signature)
	if trustErr != nil {
		return release.Manifest{}, fmt.Errorf("verify release signature: %w", trustErr)
	}
	eligibilityErr := release.Eligible(manifest, i.config.Device)
	if eligibilityErr != nil {
		return release.Manifest{}, fmt.Errorf("check release eligibility: %w", eligibilityErr)
	}
	matchErr := matchOffer(offer, manifest)
	if matchErr != nil {
		return release.Manifest{}, matchErr
	}
	available, err := i.config.Space.Available(filepath.Dir(i.config.TargetPath))
	if err != nil {
		return release.Manifest{}, fmt.Errorf("query free space: %w", err)
	}
	required, err := stagingSpace(manifest.Size)
	if err != nil {
		return release.Manifest{}, err
	}
	if available < required {
		return release.Manifest{}, fmt.Errorf("%w: have %d, need %d", ErrInsufficientSpace, available, required)
	}
	if err := ctx.Err(); err != nil {
		return release.Manifest{}, fmt.Errorf("update canceled before commit: %w", err)
	}
	return manifest, nil
}

func (i *Installer) stageAndCommit(ctx context.Context, offer protocol.UpdateOffer, manifest release.Manifest) (result Result, err error) {
	part, partPath, err := i.config.FS.CreateTemp(filepath.Dir(i.config.TargetPath), "."+filepath.Base(i.config.TargetPath)+"-*.part", 0o600)
	if err != nil {
		return result, fmt.Errorf("create staging file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			closeErr := part.Close()
			removeErr := i.config.FS.Remove(partPath)
			if closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close staging file during cleanup: %w", closeErr))
			}
			if removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove staging file during cleanup: %w", removeErr))
			}
		}
	}()
	if err := i.downloadArtifact(ctx, offer.ArtifactURL, part, manifest); err != nil {
		return result, err
	}
	if err := part.Close(); err != nil {
		return result, fmt.Errorf("close staging file: %w", err)
	}
	if err := i.config.FS.Chmod(partPath, 0o755); err != nil {
		return result, fmt.Errorf("make staged agent executable: %w", err)
	}
	staged, openErr := i.config.FS.Open(partPath)
	if openErr != nil {
		return result, fmt.Errorf("reopen staging file for fsync: %w", openErr)
	}
	if syncErr := staged.Sync(); syncErr != nil {
		_ = staged.Close()
		return result, fmt.Errorf("fsync staged agent: %w", syncErr)
	}
	if closeErr := staged.Close(); closeErr != nil {
		return result, fmt.Errorf("close staged agent after fsync: %w", closeErr)
	}
	// This is the final cancellation observation. From here through Rename the
	// installer is in its tiny non-cancellable commit critical section.
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("update canceled before commit: %w", err)
	}
	if err := i.config.FS.Rename(partPath, i.config.TargetPath); err != nil {
		return result, fmt.Errorf("commit staged agent: %w", err)
	}
	committed = true
	result.Committed = true
	if i.config.DirectorySync {
		if err := i.config.FS.SyncDir(filepath.Dir(i.config.TargetPath)); err != nil {
			return result, committedError("fsync agent directory", err)
		}
	}
	result.Metadata = metadataFor(manifest, i.config.Clock.Now())
	if err := i.persistMetadata(result.Metadata); err != nil {
		return result, committedError("persist installed-release metadata", err)
	}
	return result, nil
}

func (i *Installer) acquire() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.active {
		return false
	}
	i.active = true
	return true
}
func (i *Installer) release() { i.mu.Lock(); i.active = false; i.mu.Unlock() }

func (i *Installer) fetchManifest(ctx context.Context, rawURL string) (release.Manifest, error) {
	raw, err := i.fetchControl(ctx, rawURL)
	if err != nil {
		return release.Manifest{}, err
	}
	m, err := release.ParseManifest(raw)
	if err != nil {
		return release.Manifest{}, fmt.Errorf("parse release manifest: %w", err)
	}
	return m, nil
}

func (i *Installer) validateOfferURLs(offer protocol.UpdateOffer) error {
	for _, rawURL := range []string{offer.ArtifactURL, offer.ManifestURL, offer.SignatureURL} {
		if err := validateGatewayURL(rawURL, i.config.GatewayAuthority); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidOffer, err)
		}
	}
	return nil
}

// validateGatewayURL intentionally does not include rawURL in an error: update
// URLs carry short-lived query tokens.
func validateGatewayURL(rawURL, authority string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return errors.New("URL must be an HTTPS URL without userinfo")
	}
	want, err := canonicalAuthority(authority)
	if err != nil {
		return err
	}
	got, err := canonicalAuthority(u.Host)
	if err != nil || got != want {
		return errors.New("URL authority is not the paired gateway")
	}
	return nil
}

func canonicalAuthority(authority string) (string, error) {
	if authority == "" || strings.Contains(authority, "@") {
		return "", errors.New("update: gateway authority is required")
	}
	u, err := url.Parse("https://" + authority)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" {
		return "", errors.New("update: invalid gateway authority")
	}
	return strings.ToLower(u.Host), nil
}

func (i *Installer) fetchControl(ctx context.Context, rawURL string) ([]byte, error) {
	body, err := i.config.Downloader.Fetch(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch release control data: %w", err)
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, maxControlBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read release control data: %w", err)
	}
	if len(raw) > maxControlBytes {
		return nil, errors.New("update: release control data is too large")
	}
	return raw, nil
}

func (i *Installer) downloadArtifact(ctx context.Context, rawURL string, dst io.Writer, m release.Manifest) error {
	body, err := i.config.Downloader.Fetch(ctx, rawURL)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer body.Close()
	hash := sha256.New()
	limited := io.LimitReader(body, m.Size+1)
	n, err := io.Copy(io.MultiWriter(dst, hash), limited)
	if err != nil {
		return fmt.Errorf("write staging artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update canceled during download: %w", err)
	}
	if n > m.Size {
		return fmt.Errorf("%w: got more than %d bytes", ErrTooLarge, m.Size)
	}
	if n != m.Size {
		return fmt.Errorf("%w: got %d bytes, want %d", release.ErrSizeMismatch, n, m.Size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, m.SHA256) {
		return fmt.Errorf("%w: got %s", release.ErrDigestMismatch, got)
	}
	return nil
}

func matchOffer(offer protocol.UpdateOffer, m release.Manifest) error {
	if offer.Version != m.Version || offer.BuildID != m.BuildID || offer.Size != m.Size || !strings.EqualFold(offer.SHA256, m.SHA256) {
		return ErrOfferMismatch
	}
	return nil
}

func stagingSpace(size int64) (int64, error) {
	if size <= 0 || size > maxInt64-minimumSpaceMargin {
		return 0, errors.New("update: invalid artifact size for space calculation")
	}
	margin := max(size/10, int64(minimumSpaceMargin))
	if size > maxInt64-margin {
		return 0, errors.New("update: artifact size overflows space calculation")
	}
	return size + margin, nil
}

func metadataFor(m release.Manifest, installedAt time.Time) Metadata {
	return Metadata{Schema: 1, Version: m.Version, BuildID: m.BuildID, Architecture: m.Architecture, Size: m.Size, SHA256: strings.ToLower(m.SHA256), InstalledAt: installedAt.UTC()}
}

// ParseMetadata reads strict diagnostic metadata. It is deliberately separate
// from installation: metadata cannot authorize or recover an executable.
func ParseMetadata(raw []byte) (Metadata, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var metadata Metadata
	if err := dec.Decode(&metadata); err != nil {
		return Metadata{}, fmt.Errorf("update: parse installed-release metadata: %w", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Metadata{}, errors.New("update: invalid installed-release metadata")
	}
	if metadata.Schema != 1 || metadata.Version == "" || metadata.BuildID == "" || metadata.Architecture == "" || metadata.Size <= 0 || metadata.InstalledAt.IsZero() {
		return Metadata{}, errors.New("update: invalid installed-release metadata")
	}
	if digest, err := hex.DecodeString(metadata.SHA256); err != nil || len(digest) != sha256.Size {
		return Metadata{}, errors.New("update: invalid installed-release metadata digest")
	}
	return metadata, nil
}

// MetadataMatchesRevision reports whether diagnostics describe the running
// executable. Startup callers use the executable's link-time revision as the
// authority and may discard stale metadata; this package never treats it as a
// rollback signal.
func MetadataMatchesRevision(metadata Metadata, revision string) bool {
	return revision != "" && metadata.BuildID == revision
}

// ReconcileMetadata reads diagnostic metadata and compares it to the running
// binary's link-time revision. A mismatch is returned as stale, not repaired or
// used to choose a binary; the executable remains the sole runtime authority.
func (i *Installer) ReconcileMetadata(revision string) (Metadata, bool, error) {
	raw, err := i.config.FS.ReadFile(i.config.MetadataPath)
	if err != nil {
		return Metadata{}, false, fmt.Errorf("read installed-release metadata: %w", err)
	}
	metadata, err := ParseMetadata(raw)
	if err != nil {
		return Metadata{}, false, err
	}
	return metadata, MetadataMatchesRevision(metadata, revision), nil
}

func (i *Installer) persistMetadata(metadata Metadata) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(i.config.MetadataPath)
	file, path, err := i.config.FS.CreateTemp(dir, ".installed-release-*.part", 0o600)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = file.Close()
			_ = i.config.FS.Remove(path)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync metadata: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close metadata: %w", err)
	}
	if err := i.config.FS.Rename(path, i.config.MetadataPath); err != nil {
		return fmt.Errorf("rename metadata: %w", err)
	}
	cleanup = false
	if i.config.DirectorySync {
		if err := i.config.FS.SyncDir(dir); err != nil {
			return fmt.Errorf("fsync metadata directory: %w", err)
		}
	}
	return nil
}

func committedError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrCommitted, operation, err)
}
