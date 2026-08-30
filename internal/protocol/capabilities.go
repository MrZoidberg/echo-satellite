package protocol

import "slices"

// Capability names one negotiable feature a device supports. Capabilities are
// announced in hello and are the only supported way to decide whether a feature
// may be used with a given device. Deciding by agent version is forbidden: a
// device may run an older or newer build than any gateway expects, and version
// minimums belong in release manifests as installation-safety constraints.
type Capability string

// Capabilities defined by protocol version 1.
const (
	// CapWakeLocal reports the device runs the full local wake stack. Every
	// real satellite announces it; there is no gateway-side wake alternative.
	CapWakeLocal Capability = "wake.local"
	// CapWakeModelSync reports the device can receive wake models from the gateway.
	CapWakeModelSync Capability = "wake.model_sync" //nolint:gosec // G101 false positive: a capability name, not a credential
	// CapAudioCapture reports the device can stream command audio during a turn.
	CapAudioCapture Capability = "audio.capture"
	// CapAudioPlayback reports the device can play gateway-supplied audio.
	CapAudioPlayback Capability = "audio.playback"
	// CapCommandEndpointingLocal reports that command endpointing runs on-device.
	CapCommandEndpointingLocal Capability = "command.endpointing.local"
	// CapUpdateAB reports the device supports application-level A/B agent updates.
	CapUpdateAB Capability = "update.ab"
	// CapLED reports the device can display semantic LED states.
	CapLED Capability = "led"
	// CapButton reports the device can report action-button presses.
	CapButton Capability = "button"
	// CapMute reports the device exposes a microphone mute control.
	CapMute Capability = "mute"
)

// Capabilities is the set of capabilities a device announces. It marshals as a
// JSON array of strings.
type Capabilities []Capability

// NewCapabilities builds a capability set with duplicates removed and entries
// sorted, so two devices announcing the same features produce identical wire
// bytes.
func NewCapabilities(caps ...Capability) Capabilities {
	if len(caps) == 0 {
		return Capabilities{}
	}
	out := make(Capabilities, len(caps))
	copy(out, caps)
	slices.Sort(out)
	return slices.Compact(out)
}

// Has reports whether the capability was announced.
func (c Capabilities) Has(capability Capability) bool {
	return slices.Contains(c, capability)
}
