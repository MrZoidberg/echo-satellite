package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// DeviceConfig is the versioned, gateway-managed desired configuration for one device.
// Version zero is reserved for local bootstrap configuration and is never pushed.
type DeviceConfig struct {
	Version     uint64            `json:"version"`
	Wake        WakeSettings      `json:"wake"`
	Endpointing EndpointingConfig `json:"endpointing"`
	Logs        LogSettings       `json:"logs"`
}

// WakeSettings configures the device-local wake stack.
type WakeSettings struct {
	Engine          string  `json:"engine"`
	Model           string  `json:"model"`
	Threshold       float64 `json:"threshold"`
	VADEnabled      bool    `json:"vad_enabled"`
	VADThreshold    float64 `json:"vad_threshold"`
	VADLookbackMS   int     `json:"vad_lookback_ms"`
	PreRollMS       int     `json:"pre_roll_ms"`
	MinIntervalMS   int     `json:"min_interval_ms"`
	AlwaysScoreWake bool    `json:"always_score_wake"`
}

// EndpointingConfig configures the separate, device-local command endpoint detector.
type EndpointingConfig struct {
	SpeechThreshold   float64 `json:"speech_threshold"`
	SpeechOnsetMS     int     `json:"speech_onset_ms"`
	TrailingSilenceMS int     `json:"trailing_silence_ms"`
	NoSpeechTimeoutMS int     `json:"no_speech_timeout_ms"`
	MaxTurnMS         int     `json:"max_turn_ms"`
}

// LogLevel is the minimum level of device records forwarded to the gateway.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Valid reports whether l is a protocol-defined log level.
func (l LogLevel) Valid() bool {
	switch l {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	default:
		return false
	}
}

// LogSettings controls structured-log forwarding.
type LogSettings struct {
	ForwardLevel LogLevel `json:"forward_level"`
}

// ConfigResultStatus reports how a device handled a config revision.
type ConfigResultStatus string

const (
	ConfigResultPending  ConfigResultStatus = "pending"
	ConfigResultApplied  ConfigResultStatus = "applied"
	ConfigResultRejected ConfigResultStatus = "rejected"
)

// Valid reports whether s is a protocol-defined config result status.
func (s ConfigResultStatus) Valid() bool {
	return s == ConfigResultPending || s == ConfigResultApplied || s == ConfigResultRejected
}

// ConfigResult acknowledges a gateway configuration revision.
type ConfigResult struct {
	Version uint64             `json:"version"`
	Status  ConfigResultStatus `json:"status"`
	Code    string             `json:"code,omitempty"`
	Detail  string             `json:"detail,omitempty"`
}

// AudioStopReason says why a device closed an input audio window.
type AudioStopReason string

const (
	AudioStopEndpointed     AudioStopReason = "endpointed"
	AudioStopNoSpeech       AudioStopReason = "no_speech"
	AudioStopTimeout        AudioStopReason = "timeout"
	AudioStopEOF            AudioStopReason = "eof"
	AudioStopCaptureOverrun AudioStopReason = "capture_overrun"
)

// Valid reports whether r is a protocol-defined audio-stop reason.
func (r AudioStopReason) Valid() bool {
	switch r {
	case AudioStopEndpointed, AudioStopNoSpeech, AudioStopTimeout, AudioStopEOF, AudioStopCaptureOverrun:
		return true
	default:
		return false
	}
}

// LogRecord is a device log record forwarded over the control connection.
type LogRecord struct {
	Level   LogLevel          `json:"level"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Validate checks a config before it is applied atomically.
func (c DeviceConfig) Validate() error {
	if c.Version == 0 {
		return errors.New("protocol: config version must be greater than zero")
	}
	if err := c.Wake.Validate(); err != nil {
		return err
	}
	if err := c.Endpointing.Validate(); err != nil {
		return err
	}
	if !c.Logs.ForwardLevel.Valid() {
		return fmt.Errorf("protocol: invalid log forward level %q", c.Logs.ForwardLevel)
	}
	return nil
}

// UnmarshalJSON rejects incomplete configuration documents before they can be applied.
func (c *DeviceConfig) UnmarshalJSON(data []byte) error {
	type deviceConfigAlias DeviceConfig
	var decoded deviceConfigAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode config: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("protocol: decode config fields: %w", err)
	}
	if err := requireObjectKeys(raw, "config", "version", "wake", "endpointing", "logs"); err != nil {
		return err
	}
	if err := requireNestedObjectKeys(raw["wake"], "wake", "engine", "model", "threshold", "vad_enabled", "vad_threshold", "vad_lookback_ms", "pre_roll_ms", "min_interval_ms", "always_score_wake"); err != nil {
		return err
	}
	if err := requireNestedObjectKeys(raw["endpointing"], "endpointing", "speech_threshold", "speech_onset_ms", "trailing_silence_ms", "no_speech_timeout_ms", "max_turn_ms"); err != nil {
		return err
	}
	if err := requireNestedObjectKeys(raw["logs"], "logs", "forward_level"); err != nil {
		return err
	}
	*c = DeviceConfig(decoded)
	return c.Validate()
}

// Validate checks device-local wake configuration.
func (c WakeSettings) Validate() error {
	if strings.TrimSpace(c.Engine) == "" || strings.TrimSpace(c.Model) == "" {
		return errors.New("protocol: wake engine and model are required")
	}
	if err := validThreshold("wake threshold", c.Threshold); err != nil {
		return err
	}
	if err := validThreshold("wake VAD threshold", c.VADThreshold); err != nil {
		return err
	}
	for _, duration := range []struct {
		name  string
		value int
	}{
		{"wake VAD lookback", c.VADLookbackMS},
		{"wake pre-roll", c.PreRollMS},
		{"wake minimum interval", c.MinIntervalMS},
	} {
		if err := validPositiveDuration(duration.name, duration.value); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks device-local command endpointing configuration.
func (c EndpointingConfig) Validate() error {
	if err := validThreshold("endpointing speech threshold", c.SpeechThreshold); err != nil {
		return err
	}
	for _, duration := range []struct {
		name  string
		value int
	}{
		{"endpointing speech onset", c.SpeechOnsetMS},
		{"endpointing trailing silence", c.TrailingSilenceMS},
		{"endpointing no-speech timeout", c.NoSpeechTimeoutMS},
		{"endpointing maximum turn", c.MaxTurnMS},
	} {
		if err := validPositiveDuration(duration.name, duration.value); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks a configuration acknowledgement.
func (r ConfigResult) Validate() error {
	if r.Version == 0 || !r.Status.Valid() {
		return errors.New("protocol: invalid config result")
	}
	if r.Status == ConfigResultRejected && strings.TrimSpace(r.Code) == "" {
		return errors.New("protocol: rejected config result requires code")
	}
	return nil
}

// Validate checks a forwarded structured log record.
func (r LogRecord) Validate() error {
	if !r.Level.Valid() || strings.TrimSpace(r.Message) == "" {
		return errors.New("protocol: invalid log record")
	}
	for key := range r.Fields {
		if strings.TrimSpace(key) == "" {
			return errors.New("protocol: log field key is required")
		}
	}
	return nil
}

func validThreshold(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf("protocol: %s must be finite and between zero and one", name)
	}
	return nil
}

func validPositiveDuration(name string, value int) error {
	if value <= 0 {
		return fmt.Errorf("protocol: %s duration must be positive", name)
	}
	return nil
}

func requireNestedObjectKeys(raw json.RawMessage, name string, keys ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("protocol: decode %s: %w", name, err)
	}
	return requireObjectKeys(object, name, keys...)
}

func requireObjectKeys(object map[string]json.RawMessage, name string, keys ...string) error {
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("protocol: %s requires %s", name, key)
		}
	}
	return nil
}
