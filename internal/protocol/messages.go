package protocol

// MessageType identifies a control frame. The full set mirrors the message
// families in docs/DESIGN.md 8.6; types without a payload struct in this
// package are reserved and gain one when their milestone lands.
type MessageType string

// Message types exchanged over the control channel.
const (
	// session and status

	TypeHello   MessageType = "hello"
	TypeWelcome MessageType = "welcome"
	TypeConfig  MessageType = "config"
	TypeState   MessageType = "state"
	TypeHealth  MessageType = "health"
	TypeLog     MessageType = "log"

	// voice turns; turn.start is always produced by the device

	TypeTurnStart  MessageType = "turn.start"
	TypeTurnCancel MessageType = "turn.cancel"

	// device-local wake stack reporting

	TypeWakeModels MessageType = "wake.models"
	TypeWakeStatus MessageType = "wake.status"

	// audio window markers; PCM travels in binary frames between them

	TypeAudioStart MessageType = "audio.start"
	TypeAudioStop  MessageType = "audio.stop"
	TypePlayStart  MessageType = "play.start"
	TypePlayStop   MessageType = "play.stop"

	// application-level A/B agent updates

	TypeUpdateOffer      MessageType = "update.offer"
	TypeUpdateAccept     MessageType = "update.accept"
	TypeUpdateReject     MessageType = "update.reject"
	TypeUpdateProgress   MessageType = "update.progress"
	TypeUpdateStaged     MessageType = "update.staged"
	TypeUpdateRestarting MessageType = "update.restarting"
	TypeUpdateTrial      MessageType = "update.trial"
	TypeUpdateConfirmed  MessageType = "update.confirmed"
	TypeUpdateRolledBack MessageType = "update.rolled_back"
	TypeUpdateFailed     MessageType = "update.failed"

	// device controls

	TypeButton MessageType = "button"
	TypeMute   MessageType = "mute"
	TypeVolume MessageType = "volume"

	// liveness and errors

	TypePing  MessageType = "ping"
	TypePong  MessageType = "pong"
	TypeError MessageType = "error"
)

// allMessageTypes is the authoritative set used by Known and AllMessageTypes.
var allMessageTypes = []MessageType{
	TypeHello, TypeWelcome, TypeConfig, TypeState, TypeHealth, TypeLog,
	TypeTurnStart, TypeTurnCancel,
	TypeWakeModels, TypeWakeStatus,
	TypeAudioStart, TypeAudioStop, TypePlayStart, TypePlayStop,
	TypeUpdateOffer, TypeUpdateAccept, TypeUpdateReject, TypeUpdateProgress,
	TypeUpdateStaged, TypeUpdateRestarting, TypeUpdateTrial, TypeUpdateConfirmed,
	TypeUpdateRolledBack, TypeUpdateFailed,
	TypeButton, TypeMute, TypeVolume,
	TypePing, TypePong, TypeError,
}

var knownMessageTypes = func() map[MessageType]struct{} {
	m := make(map[MessageType]struct{}, len(allMessageTypes))
	for _, t := range allMessageTypes {
		m[t] = struct{}{}
	}
	return m
}()

// Known reports whether the message type is defined by this protocol version.
// An unknown type is expected when talking to a peer speaking a newer protocol
// and must be ignored rather than treated as a connection error.
func (t MessageType) Known() bool {
	_, ok := knownMessageTypes[t]
	return ok
}

// String returns the wire representation of the message type.
func (t MessageType) String() string { return string(t) }

// AllMessageTypes returns every message type defined by this protocol version,
// in documentation order.
func AllMessageTypes() []MessageType {
	out := make([]MessageType, len(allMessageTypes))
	copy(out, allMessageTypes)
	return out
}

// TurnTrigger names what started a voice turn. Both values are decided on the
// device: the gateway never triggers a turn.
type TurnTrigger string

// Voice turn triggers.
const (
	TriggerWake   TurnTrigger = "wake"
	TriggerButton TurnTrigger = "button"
)

// Valid reports whether the trigger is one this protocol version defines.
func (t TurnTrigger) Valid() bool { return t == TriggerWake || t == TriggerButton }

// DeviceState is the semantic state a device reports, and the state the gateway
// asks it to display.
type DeviceState string

// Device states.
const (
	StateIdle        DeviceState = "idle"
	StateListening   DeviceState = "listening"
	StateThinking    DeviceState = "thinking"
	StateSpeaking    DeviceState = "speaking"
	StateMuted       DeviceState = "muted"
	StateOffline     DeviceState = "offline"
	StateUpdating    DeviceState = "updating"
	StateUpdateTrial DeviceState = "update_trial"
	StateError       DeviceState = "error"
)

var allDeviceStates = []DeviceState{
	StateIdle,
	StateListening,
	StateThinking,
	StateSpeaking,
	StateMuted,
	StateOffline,
	StateError,
	StateUpdating,
	StateUpdateTrial,
}

// AllDeviceStates returns every semantic device state in documentation order.
func AllDeviceStates() []DeviceState {
	out := make([]DeviceState, len(allDeviceStates))
	copy(out, allDeviceStates)
	return out
}

// AudioFormat names the encoding of a binary audio frame.
type AudioFormat string

// AudioFormatPCMS16LE is signed 16-bit little-endian PCM, the only format
// defined for protocol version 1.
const AudioFormatPCMS16LE AudioFormat = "pcm_s16le"

// Hello is the first message a device sends after connecting. It announces
// identity, versions and capabilities, and reports any update currently on
// trial so the gateway learns the device's update state before anything else.
type Hello struct {
	DeviceID          string       `json:"device_id"`
	AgentVersion      string       `json:"agent_version"`
	SupervisorVersion string       `json:"supervisor_version"`
	Protocol          int          `json:"protocol"`
	Capabilities      Capabilities `json:"capabilities"`
	WakeConfig        WakeConfig   `json:"wake_config"`
	UpdateState       UpdatePhase  `json:"update_state"`
}

// WakeConfig summarizes the device-local wake stack. It is reported for
// observability only: the gateway does not score wake words and cannot change
// these values by replying with a different summary.
type WakeConfig struct {
	Engine        string   `json:"engine"`
	Models        []string `json:"models"`
	WakeThreshold float64  `json:"wake_threshold"`
	VADThreshold  float64  `json:"vad_threshold"`
	PreRollMS     int      `json:"pre_roll_ms"`
}

// Welcome is the gateway's reply to hello.
type Welcome struct {
	ServerID string `json:"server_id"`
	Protocol int    `json:"protocol"`
	// Config carries gateway-managed device configuration. Its schema is owned
	// by the gateway config package and is opaque to the protocol layer.
	Config map[string]any `json:"config,omitempty"`
}

// TurnStart opens a voice turn. It is always sent by the device, after the
// local wake stack accepted a wake word or the action button was pressed.
type TurnStart struct {
	Trigger   TurnTrigger `json:"trigger"`
	Model     string      `json:"model,omitempty"`
	WakeScore float64     `json:"wake_score,omitempty"`
	VADScore  float64     `json:"vad_score,omitempty"`
	PreRollMS int         `json:"pre_roll_ms,omitempty"`
}

// AudioStart opens the binary PCM window for the command audio of a turn.
type AudioStart struct {
	SampleRate int         `json:"sample_rate"`
	Channels   int         `json:"channels"`
	Format     AudioFormat `json:"format"`
}

// AudioStop closes the command audio window.
type AudioStop struct {
	Reason string `json:"reason,omitempty"`
}

// PlayStart opens the binary PCM window for gateway-to-device playback.
type PlayStart struct {
	SampleRate int         `json:"sample_rate"`
	Channels   int         `json:"channels"`
	Format     AudioFormat `json:"format"`
}

// PlayStop closes the playback window.
type PlayStop struct {
	Reason string `json:"reason,omitempty"`
}

// State reports or requests a semantic device state such as the LED ring.
type State struct {
	State  DeviceState `json:"state"`
	Detail string      `json:"detail,omitempty"`
}

// UpdateOffer offers a release to a device. The device fetches the artifact
// over authenticated HTTPS rather than through this connection.
type UpdateOffer struct {
	Version     string `json:"version"`
	BuildID     string `json:"build_id"`
	ArtifactURL string `json:"artifact_url"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ManifestURL string `json:"manifest_url,omitempty"`
}

// UpdateProgress reports where a device is in the update state machine.
type UpdateProgress struct {
	Phase   UpdatePhase `json:"phase"`
	Percent int         `json:"percent,omitempty"`
	Detail  string      `json:"detail,omitempty"`
}

// UpdateFailed reports a terminal update failure. It does not imply the device
// is unhealthy: the device keeps its previous slot and stays on it.
type UpdateFailed struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is the generic error frame. It implements the error interface so a
// received frame can be returned directly by a caller.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error returns the error message, prefixed with the code when one is set.
func (e Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}
