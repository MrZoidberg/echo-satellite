package release

import (
	"crypto/ed25519"
	"errors"
	"fmt"
)

// ErrUnsignedRelease is returned when a release carries no signature and the
// development escape hatch is off.
var ErrUnsignedRelease = errors.New("release: unsigned release rejected")

// TrustPolicy decides whether a release may be installed at all. Its zero value
// is the secure one: signatures required, no dev builds.
type TrustPolicy struct {
	// AllowUnsignedDevBuilds accepts a release with no signature. It is the
	// development escape hatch from docs/DESIGN.md 11.3: off by default, and
	// whenever it is on, StatusNotes must be surfaced in status output and UI so
	// an operator can never be unaware that this deployment accepts any binary.
	AllowUnsignedDevBuilds bool

	// PublicKey verifies release manifests. When nil, the link-time embedded
	// release key is used.
	PublicKey ed25519.PublicKey
}

// Check validates the manifest and its signature under this policy. An empty
// signature means the release is unsigned.
func (p TrustPolicy) Check(m Manifest, sig []byte) error {
	if err := m.Validate(); err != nil {
		return err
	}

	if len(sig) == 0 {
		if p.AllowUnsignedDevBuilds {
			return nil
		}
		return fmt.Errorf("%w: version %s build %s", ErrUnsignedRelease, m.Version, m.BuildID)
	}

	pub, err := p.publicKey()
	if err != nil {
		if p.AllowUnsignedDevBuilds {
			return nil
		}
		return err
	}
	return VerifyManifest(pub, m, sig)
}

// StatusNotes returns operator-visible warnings about this policy. It is empty
// for a secure policy, so a caller can render every note it returns without
// deciding which ones matter.
func (p TrustPolicy) StatusNotes() []string {
	var notes []string
	if p.AllowUnsignedDevBuilds {
		notes = append(notes,
			"unsigned development releases are accepted: any binary offered to a device will be installed")
	}
	if _, err := p.publicKey(); err != nil && !p.AllowUnsignedDevBuilds {
		notes = append(notes, "no release public key is configured: every signed release will be rejected")
	}
	return notes
}

func (p TrustPolicy) publicKey() (ed25519.PublicKey, error) {
	if len(p.PublicKey) > 0 {
		return p.PublicKey, nil
	}
	return EmbeddedPublicKey()
}
