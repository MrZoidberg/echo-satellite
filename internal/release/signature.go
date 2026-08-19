package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// embeddedPublicKey is the base64-encoded Ed25519 release public key, injected
// at link time with -ldflags "-X github.com/MrZoidberg/echo-satellite/internal/release.embeddedPublicKey=...".
// It is empty in development builds, which is why an unsigned release is only
// installable when the operator has explicitly enabled the dev escape hatch.
var embeddedPublicKey = ""

// Errors returned while signing or verifying a manifest.
var (
	// ErrNoPublicKey is returned when no release public key is available to verify with.
	ErrNoPublicKey = errors.New("release: no release public key available")
	// ErrInvalidPublicKey is returned when a public key cannot be decoded.
	ErrInvalidPublicKey = errors.New("release: invalid public key")
	// ErrInvalidSignature is returned when a signature cannot be decoded.
	ErrInvalidSignature = errors.New("release: invalid signature encoding")
	// ErrSignatureMismatch is returned when a signature does not verify against the manifest.
	ErrSignatureMismatch = errors.New("release: manifest signature does not verify")
)

// EmbeddedPublicKey returns the link-time release public key, or ErrNoPublicKey
// when this build has none.
func EmbeddedPublicKey() (ed25519.PublicKey, error) {
	if embeddedPublicKey == "" {
		return nil, ErrNoPublicKey
	}
	return ParsePublicKey(embeddedPublicKey)
}

// ParsePublicKey decodes a base64-encoded Ed25519 public key. Surrounding
// whitespace is tolerated so a key read from a file works unchanged.
func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPublicKey, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidPublicKey, len(raw), ed25519.PublicKeySize)
	}
	return raw, nil
}

// EncodeSignature renders a signature for storage in a manifest.sig file.
func EncodeSignature(sig []byte) string { return base64.StdEncoding.EncodeToString(sig) }

// DecodeSignature reads a manifest.sig file body.
func DecodeSignature(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	if len(raw) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidSignature, len(raw), ed25519.SignatureSize)
	}
	return raw, nil
}

// Sign signs a manifest with the release private key. It exists for the release
// tooling and for tests; neither the gateway nor a device ever holds the key.
func Sign(priv ed25519.PrivateKey, m Manifest) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key is %d bytes", ErrInvalidPublicKey, len(priv))
	}
	msg, err := m.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(priv, msg), nil
}

// VerifyManifest checks a manifest signature against a public key.
func VerifyManifest(pub ed25519.PublicKey, m Manifest, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrNoPublicKey
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidSignature, len(sig), ed25519.SignatureSize)
	}
	msg, err := m.CanonicalBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return fmt.Errorf("%w: version %s build %s", ErrSignatureMismatch, m.Version, m.BuildID)
	}
	return nil
}
