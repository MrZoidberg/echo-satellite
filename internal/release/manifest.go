// Package release defines the release manifest and the trust primitives both
// the gateway and the device apply to an agent artifact before it is installed.
//
// Three checks are deliberately separate:
//
//   - integrity — the artifact bytes match the size and SHA-256 in the manifest
//     (see VerifyArtifact);
//   - authenticity — the manifest carries a valid Ed25519 signature from the
//     release key (see TrustPolicy.Check);
//   - installation safety — the release is applicable to this device at all
//     (see Eligible).
//
// Eligibility is not feature gating. Feature behavior is negotiated by
// capability announcement in hello; protocol_min, protocol_max and
// supervisor_min exist only to stop an installation that would produce a device
// which cannot talk to the gateway or cannot be recovered by its supervisor.
//
// The release private key lives in the controlled build and release process. It
// never lives on the gateway or on a device; both only ever hold the public key.
package release

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion is the manifest schema this build understands.
const SchemaVersion = 1

// signingDomain separates release-manifest signatures from any other signature
// this project may introduce, so a signature can never be replayed across uses.
const signingDomain = "echo-satellite/release-manifest/v1\n"

// Errors returned while parsing or validating a manifest.
var (
	// ErrInvalidManifest is returned when a manifest is structurally unusable.
	ErrInvalidManifest = errors.New("release: invalid manifest")
	// ErrUnsupportedSchema is returned for a manifest schema this build cannot read.
	ErrUnsupportedSchema = errors.New("release: unsupported manifest schema")
)

// Manifest describes one release artifact. The field set is fixed by
// docs/DESIGN.md 11.1.
type Manifest struct {
	Schema        int       `json:"schema"`
	Version       string    `json:"version"`
	BuildID       string    `json:"build_id"`
	Architecture  string    `json:"architecture"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	ProtocolMin   int       `json:"protocol_min"`
	ProtocolMax   int       `json:"protocol_max"`
	SupervisorMin int       `json:"supervisor_min"`
	ReleasedAt    time.Time `json:"released_at"`
}

// ParseManifest reads a manifest. Unknown fields are rejected: a manifest is a
// trust input, and silently ignoring a field this build does not understand
// could hide a constraint the release intended to enforce.
func ParseManifest(data []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks the manifest is complete and internally consistent.
func (m Manifest) Validate() error {
	if m.Schema != SchemaVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedSchema, m.Schema)
	}
	if m.Version == "" {
		return fmt.Errorf("%w: empty version", ErrInvalidManifest)
	}
	if m.Architecture == "" {
		return fmt.Errorf("%w: empty architecture", ErrInvalidManifest)
	}
	if m.Size <= 0 {
		return fmt.Errorf("%w: size %d", ErrInvalidManifest, m.Size)
	}
	if err := validateDigest(m.SHA256); err != nil {
		return err
	}
	if m.ProtocolMin <= 0 || m.ProtocolMax < m.ProtocolMin {
		return fmt.Errorf("%w: protocol range %d..%d", ErrInvalidManifest, m.ProtocolMin, m.ProtocolMax)
	}
	if m.SupervisorMin < 0 {
		return fmt.Errorf("%w: supervisor_min %d", ErrInvalidManifest, m.SupervisorMin)
	}
	if m.ReleasedAt.IsZero() {
		return fmt.Errorf("%w: no released_at", ErrInvalidManifest)
	}
	return nil
}

func validateDigest(digest string) error {
	raw, err := hex.DecodeString(digest)
	if err != nil {
		return fmt.Errorf("%w: sha256 %q is not hex", ErrInvalidManifest, digest)
	}
	if len(raw) != 32 {
		return fmt.Errorf("%w: sha256 must be 32 bytes, got %d", ErrInvalidManifest, len(raw))
	}
	return nil
}

// CanonicalBytes returns the exact bytes covered by a manifest signature.
// Signing and verification must both use it: signing the on-disk file bytes
// instead would make the signature depend on whitespace and key order.
func (m Manifest) CanonicalBytes() ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("release: marshal manifest: %w", err)
	}
	out := make([]byte, 0, len(signingDomain)+len(body))
	out = append(out, signingDomain...)
	out = append(out, body...)
	return out, nil
}
