package protocol

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// MessageType identifies a control frame. The full set mirrors the message
// families in docs/DESIGN.md 8.6; types without a payload struct in this
// package are reserved and gain one when their milestone lands.
type MessageType string

// Message types exchanged over the control channel.
const (
	// session and status

	TypeHello        MessageType = "hello"
	TypeWelcome      MessageType = "welcome"
	TypeConfig       MessageType = "config"
	TypeConfigResult MessageType = "config.result"
	TypeState        MessageType = "state"
	TypeHealth       MessageType = "health"
	TypeLog          MessageType = "log"

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

	// application-level single-agent updates

	TypeUpdateOffer     MessageType = "update.offer"
	TypeUpdateDecision  MessageType = "update.decision"
	TypeUpdateProgress  MessageType = "update.progress"
	TypeUpdateConfirmed MessageType = "update.confirmed"
	TypeUpdateCancelled MessageType = "update.cancelled"
	TypeUpdateFailed    MessageType = "update.failed"

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
	TypeHello, TypeWelcome, TypeConfig, TypeConfigResult, TypeState, TypeHealth, TypeLog,
	TypeTurnStart, TypeTurnCancel,
	TypeWakeModels, TypeWakeStatus,
	TypeAudioStart, TypeAudioStop, TypePlayStart, TypePlayStop,
	TypeUpdateOffer, TypeUpdateDecision, TypeUpdateProgress, TypeUpdateConfirmed,
	TypeUpdateCancelled, TypeUpdateFailed,
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
	StateIdle      DeviceState = "idle"
	StateListening DeviceState = "listening"
	StateThinking  DeviceState = "thinking"
	StateSpeaking  DeviceState = "speaking"
	StateMuted     DeviceState = "muted"
	StateOffline   DeviceState = "offline"
	StateUpdating  DeviceState = "updating"
	StateError     DeviceState = "error"
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
// identity, versions and capabilities, and reports its update state.
type Hello struct {
	DeviceID     string       `json:"device_id"`
	AgentVersion string       `json:"agent_version"`
	Protocol     int          `json:"protocol"`
	Capabilities Capabilities `json:"capabilities"`
	WakeConfig   WakeConfig   `json:"wake_config"`
	UpdateState  UpdatePhase  `json:"update_state"`
	// InstalledVersion and InstalledBuildID are diagnostic release identity from
	// the last verified installation; AgentVersion remains the running binary's
	// link-time authority.
	InstalledVersion    string `json:"installed_version"`
	InstalledBuildID    string `json:"installed_build_id"`
	PendingDeploymentID string `json:"pending_deployment_id"`
	ConfigVersion       uint64 `json:"config_version"`
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
	ServerID string       `json:"server_id"`
	Protocol int          `json:"protocol"`
	Config   DeviceConfig `json:"config"`
}

// Validate checks the server reply's complete desired configuration.
func (w Welcome) Validate() error {
	if w.ServerID == "" {
		return errors.New("protocol: welcome server ID is required")
	}
	if w.Protocol != ProtocolVersion {
		return fmt.Errorf("protocol: unsupported welcome protocol %d", w.Protocol)
	}
	return w.Config.Validate()
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
	Reason    AudioStopReason `json:"reason"`
	Telemetry *TurnTelemetry  `json:"telemetry,omitempty"`
}

// Validate checks the reason for an input audio window closing.
func (s AudioStop) Validate() error {
	if !s.Reason.Valid() {
		return fmt.Errorf("protocol: invalid audio stop reason %q", s.Reason)
	}
	if s.Telemetry != nil {
		if err := s.Telemetry.Validate(); err != nil {
			return fmt.Errorf("protocol: invalid audio stop telemetry: %w", err)
		}
	}
	return nil
}

// Health is a bounded, device-to-gateway observability snapshot. It contains
// counters and measurements only; it never contains audio, paths, or secrets.
type Health struct {
	Version       int                `json:"version"`
	Capture       CaptureHealth      `json:"capture"`
	Wake          WakeHealth         `json:"wake"`
	Conditioning  ConditioningHealth `json:"conditioning"`
	Resources     ResourceHealth     `json:"resources"`
	TelemetryDrop uint64             `json:"telemetry_drops"`
}

type CaptureHealth struct {
	XRuns       uint64 `json:"xruns"`
	FanoutDrops uint64 `json:"fanout_drops"`
	Frames      uint64 `json:"frames"`
}

type WakeHealth struct {
	Accepted    uint64 `json:"accepted"`
	Rejected    uint64 `json:"rejected"`
	VADActiveMS uint64 `json:"vad_active_ms"`
	InferenceMS uint64 `json:"inference_ms"`
}

type ConditioningHealth struct {
	Profile          string        `json:"profile"`
	AppliedGainDB    float64       `json:"applied_gain_db"`
	PeakDBFS         float64       `json:"peak_dbfs"`
	RMSDBFS          float64       `json:"rms_dbfs"`
	ClippingCount    uint64        `json:"clipping_count"`
	ClippingFraction float64       `json:"clipping_fraction"`
	ProcessingTime   time.Duration `json:"processing_time_ns"`
	MaxBlockTime     time.Duration `json:"max_block_time_ns"`
}

type ResourceHealth struct {
	RSSBytes   uint64  `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
	Available  bool    `json:"available"`
}

// TurnTelemetry is a terminal observation for the turn identified by the
// enclosing audio.stop envelope. Counters are deltas from turn start.
type TurnTelemetry struct {
	Version      int                `json:"version"`
	StoppedAt    time.Time          `json:"stopped_at"`
	Duration     time.Duration      `json:"duration_ns"`
	Capture      CaptureHealth      `json:"capture"`
	Conditioning ConditioningHealth `json:"conditioning"`
	Resources    ResourceHealth     `json:"resources"`
}

func (h Health) Validate() error {
	if h.Version != 1 {
		return errors.New("health version must be 1")
	}
	if err := validateCapture(h.Capture); err != nil {
		return err
	}
	if h.Wake.VADActiveMS > maxHealthMS || h.Wake.InferenceMS > maxHealthMS {
		return errors.New("health: wake timing exceeds limit")
	}
	if err := validateConditioning(h.Conditioning); err != nil {
		return err
	}
	return validateResources(h.Resources)
}

func (t TurnTelemetry) Validate() error {
	if t.Version != 1 {
		return errors.New("turn telemetry version must be 1")
	}
	if t.StoppedAt.IsZero() || t.Duration < 0 || t.Duration > maxTurnDuration || !finiteDuration(t.Duration) {
		return errors.New("turn telemetry: invalid stop time or duration")
	}
	if err := validateCapture(t.Capture); err != nil {
		return err
	}
	if err := validateConditioning(t.Conditioning); err != nil {
		return err
	}
	return validateResources(t.Resources)
}

const (
	maxHealthMS     = 24 * 60 * 60 * 1000
	maxTurnDuration = 10 * time.Minute
	maxProfileBytes = 64
)

func validateCapture(c CaptureHealth) error {
	if c.Frames > 1<<40 {
		return errors.New("health: capture frame count exceeds limit")
	}
	return nil
}
func validateConditioning(c ConditioningHealth) error {
	if c.Profile == "" || len(c.Profile) > maxProfileBytes || strings.ContainsAny(c.Profile, "\r\n/") {
		return errors.New("health: invalid conditioning profile")
	}
	if !finite(c.AppliedGainDB) || c.AppliedGainDB < -60 || c.AppliedGainDB > 60 || !finite(c.PeakDBFS) || c.PeakDBFS < -200 || c.PeakDBFS > 1 || !finite(c.RMSDBFS) || c.RMSDBFS < -200 || c.RMSDBFS > 1 || !finite(c.ClippingFraction) || c.ClippingFraction < 0 || c.ClippingFraction > 1 {
		return errors.New("health: invalid conditioning metric")
	}
	if c.ProcessingTime < 0 || c.MaxBlockTime < 0 || c.ProcessingTime > 24*time.Hour || c.MaxBlockTime > time.Minute {
		return errors.New("health: invalid conditioning duration")
	}
	return nil
}
func validateResources(r ResourceHealth) error {
	if !finite(r.CPUPercent) || r.CPUPercent < 0 || r.CPUPercent > 1000 || r.RSSBytes > 1<<50 {
		return errors.New("health: invalid resource metric")
	}
	return nil
}
func finite(value float64) bool               { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finiteDuration(value time.Duration) bool { return value >= 0 }

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
