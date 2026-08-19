package release

import (
	"errors"
	"fmt"
)

// Errors returned when a release cannot safely be installed on a device.
var (
	// ErrArchitectureMismatch is returned when the artifact targets another architecture.
	ErrArchitectureMismatch = errors.New("release: artifact architecture does not match device")
	// ErrProtocolIncompatible is returned when the release cannot speak the device's protocol.
	ErrProtocolIncompatible = errors.New("release: protocol range excludes device")
	// ErrSupervisorTooOld is returned when the release needs a newer supervisor than
	// the device runs. The gateway marks such a release ineligible rather than
	// attempting it: the supervisor is recovery infrastructure and is never
	// updated as part of a normal agent rollout.
	ErrSupervisorTooOld = errors.New("release: device supervisor is older than the release requires")
)

// Device is what an installation-safety decision needs to know about a target.
// The values come from the device's own hello, not from an inventory guess.
type Device struct {
	Architecture      string
	Protocol          int
	SupervisorVersion int
}

// Eligible reports whether a release may be installed on a device.
//
// These are installation-safety constraints only. They never decide which
// features are used with a device: that is negotiated by capability
// announcement, and adding a version comparison here to gate behavior would
// reintroduce exactly the coupling capabilities exist to avoid.
func Eligible(m Manifest, dev Device) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if dev.Architecture != "" && m.Architecture != dev.Architecture {
		return fmt.Errorf("%w: artifact %s, device %s", ErrArchitectureMismatch, m.Architecture, dev.Architecture)
	}
	if dev.Protocol < m.ProtocolMin || dev.Protocol > m.ProtocolMax {
		return fmt.Errorf("%w: device speaks %d, release supports %d..%d",
			ErrProtocolIncompatible, dev.Protocol, m.ProtocolMin, m.ProtocolMax)
	}
	if dev.SupervisorVersion < m.SupervisorMin {
		return fmt.Errorf("%w: device has %d, release needs %d",
			ErrSupervisorTooOld, dev.SupervisorVersion, m.SupervisorMin)
	}
	return nil
}
