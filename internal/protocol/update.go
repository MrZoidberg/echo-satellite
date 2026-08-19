package protocol

import (
	"fmt"
	"slices"
)

// UpdatePhase is the gateway-visible phase of an agent update on one device,
// as listed in docs/DESIGN.md 10.7. The device owns this state machine: the
// gateway observes it and never drives a device past a phase the device has
// not reported.
type UpdatePhase string

// Update phases.
const (
	PhaseIdle        UpdatePhase = "idle"
	PhaseAvailable   UpdatePhase = "available"
	PhaseQueued      UpdatePhase = "queued"
	PhaseDownloading UpdatePhase = "downloading"
	PhaseVerifying   UpdatePhase = "verifying"
	PhaseStaged      UpdatePhase = "staged"
	PhaseRestarting  UpdatePhase = "restarting"
	PhaseTrial       UpdatePhase = "trial"
	PhaseConfirmed   UpdatePhase = "confirmed"
	PhaseFailed      UpdatePhase = "failed"
	PhaseRolledBack  UpdatePhase = "rolled_back"
	PhaseCancelled   UpdatePhase = "cancelled" //nolint:misspell // wire value fixed by docs/DESIGN.md 10.7
)

var allUpdatePhases = []UpdatePhase{
	PhaseIdle, PhaseAvailable, PhaseQueued, PhaseDownloading, PhaseVerifying,
	PhaseStaged, PhaseRestarting, PhaseTrial, PhaseConfirmed, PhaseFailed,
	PhaseRolledBack, PhaseCancelled,
}

// AllUpdatePhases returns every phase, in state-machine order.
func AllUpdatePhases() []UpdatePhase {
	out := make([]UpdatePhase, len(allUpdatePhases))
	copy(out, allUpdatePhases)
	return out
}

// String returns the wire representation of the phase.
func (p UpdatePhase) String() string { return string(p) }

// Valid reports whether the phase is defined by this protocol version.
func (p UpdatePhase) Valid() bool { return slices.Contains(allUpdatePhases, p) }

// Terminal reports whether no further phase follows without a new offer.
func (p UpdatePhase) Terminal() bool {
	switch p {
	case PhaseConfirmed, PhaseFailed, PhaseRolledBack, PhaseCancelled:
		return true
	default:
		return false
	}
}

// ParseUpdatePhase converts a wire value into an UpdatePhase.
func ParseUpdatePhase(s string) (UpdatePhase, error) {
	p := UpdatePhase(s)
	if !p.Valid() {
		return "", fmt.Errorf("protocol: unknown update phase %q", s)
	}
	return p, nil
}
