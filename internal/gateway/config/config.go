// Package config loads the gateway's versioned device desired-state profile.
package config

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

var (
	ErrNonMonotonicVersion = errors.New("gateway config: version is not greater than the active version")
	ErrUninitializedStore  = errors.New("gateway config: store has no validated initial profile")
)

// Snapshot is an immutable, complete desired-state profile.
type Snapshot struct {
	version   uint64
	defaults  protocol.DeviceConfig
	overrides map[string]override
}

// Version returns the profile's operator-managed revision.
func (s Snapshot) Version() uint64 { return s.version }

// Effective returns the complete desired configuration for deviceID.
func (s Snapshot) Effective(deviceID string) protocol.DeviceConfig {
	result := s.defaults
	if value, ok := s.overrides[deviceID]; ok {
		result = value.apply(result)
	}
	return result
}

// EffectiveFor returns complete configurations for the supplied connected
// devices. It de-duplicates device IDs so callers can push each device once.
func (s Snapshot) EffectiveFor(deviceIDs []string) map[string]protocol.DeviceConfig {
	result := make(map[string]protocol.DeviceConfig, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		result[deviceID] = s.Effective(deviceID)
	}
	return result
}

// Load decodes and validates one strict TOML profile.
func Load(input io.Reader) (Snapshot, error) {
	var document rawDocument
	decoder := toml.NewDecoder(input).DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Snapshot{}, fmt.Errorf("decode gateway TOML profile: %w", err)
	}
	if document.Version == 0 {
		return Snapshot{}, errors.New("gateway config: version must be greater than zero")
	}
	defaults, err := document.Defaults.complete(document.Version)
	if err != nil {
		return Snapshot{}, fmt.Errorf("gateway config: invalid defaults: %w", err)
	}

	result := Snapshot{version: document.Version, defaults: defaults, overrides: make(map[string]override, len(document.Devices))}
	for deviceID, value := range document.Devices {
		if strings.TrimSpace(deviceID) == "" {
			return Snapshot{}, errors.New("gateway config: device ID is required")
		}
		deviceOverride, err := value.toOverride()
		if err != nil {
			return Snapshot{}, fmt.Errorf("gateway config: device %q: %w", deviceID, err)
		}
		candidate := deviceOverride.apply(defaults)
		if err := candidate.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("gateway config: device %q effective config: %w", deviceID, err)
		}
		result.overrides[deviceID] = deviceOverride
	}
	return result, nil
}

// Store retains the active profile and only swaps it after complete validation.
type Store struct {
	mu          sync.RWMutex
	current     Snapshot
	initialized bool
}

// NewStore creates a store from a validated profile.
func NewStore(initial Snapshot) (*Store, error) {
	if err := initial.validate(); err != nil {
		return nil, fmt.Errorf("gateway config: initial profile: %w", err)
	}
	return &Store{current: initial, initialized: true}, nil
}

// Snapshot returns the active immutable profile.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.clone()
}

// Reload validates a replacement profile, requires a higher revision, and then
// swaps it atomically. It returns each connected device's new effective config.
func (s *Store) Reload(input io.Reader, deviceIDs []string) (map[string]protocol.DeviceConfig, error) {
	candidate, err := Load(input)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return nil, ErrUninitializedStore
	}
	if candidate.version <= s.current.version {
		return nil, fmt.Errorf("%w: candidate %d, active %d", ErrNonMonotonicVersion, candidate.version, s.current.version)
	}
	s.current = candidate
	return candidate.EffectiveFor(deviceIDs), nil
}

func (s Snapshot) clone() Snapshot {
	return Snapshot{version: s.version, defaults: s.defaults, overrides: maps.Clone(s.overrides)}
}

func (s Snapshot) validate() error {
	if s.version == 0 {
		return errors.New("version must be greater than zero")
	}
	if err := s.defaults.Validate(); err != nil {
		return fmt.Errorf("validate defaults: %w", err)
	}
	for deviceID, deviceOverride := range s.overrides {
		if strings.TrimSpace(deviceID) == "" {
			return errors.New("device ID is required")
		}
		if err := deviceOverride.apply(s.defaults).Validate(); err != nil {
			return fmt.Errorf("validate device %q effective config: %w", deviceID, err)
		}
	}
	return nil
}

type rawDocument struct {
	Version  uint64                 `toml:"version"`
	Defaults rawComplete            `toml:"defaults"`
	Devices  map[string]rawOverride `toml:"devices"`
}

// rawComplete uses pointers so TOML defaults must state every field, including
// false booleans and zero-valued numbers.
type rawComplete struct {
	Wake        rawWake        `toml:"wake"`
	Endpointing rawEndpointing `toml:"endpointing"`
	Logs        rawLogs        `toml:"logs"`
}

func (r rawComplete) complete(version uint64) (protocol.DeviceConfig, error) {
	value := protocol.DeviceConfig{Version: version}
	var err error
	if value.Wake, err = r.Wake.complete(); err != nil {
		return protocol.DeviceConfig{}, fmt.Errorf("wake: %w", err)
	}
	if value.Endpointing, err = r.Endpointing.complete(); err != nil {
		return protocol.DeviceConfig{}, fmt.Errorf("endpointing: %w", err)
	}
	if value.Logs, err = r.Logs.complete(); err != nil {
		return protocol.DeviceConfig{}, fmt.Errorf("logs: %w", err)
	}
	if err := value.Validate(); err != nil {
		return protocol.DeviceConfig{}, fmt.Errorf("validate complete configuration: %w", err)
	}
	return value, nil
}

type rawOverride struct {
	Wake        rawWake        `toml:"wake"`
	Endpointing rawEndpointing `toml:"endpointing"`
	Logs        rawLogs        `toml:"logs"`
}

func (r rawOverride) toOverride() (override, error) {
	value := override{wake: r.Wake, endpointing: r.Endpointing, logs: r.Logs}
	if value.empty() {
		return override{}, errors.New("override must set at least one value")
	}
	return value, nil
}

type override struct {
	wake        rawWake
	endpointing rawEndpointing
	logs        rawLogs
}

func (o override) empty() bool { return o.wake.empty() && o.endpointing.empty() && o.logs.empty() }

func (o override) apply(value protocol.DeviceConfig) protocol.DeviceConfig {
	o.wake.apply(&value.Wake)
	o.endpointing.apply(&value.Endpointing)
	o.logs.apply(&value.Logs)
	return value
}

type rawWake struct {
	Engine          *string  `toml:"engine"`
	Model           *string  `toml:"model"`
	Threshold       *float64 `toml:"threshold"`
	VADEnabled      *bool    `toml:"vad_enabled"`
	VADThreshold    *float64 `toml:"vad_threshold"`
	VADLookbackMS   *int     `toml:"vad_lookback_ms"`
	PreRollMS       *int     `toml:"pre_roll_ms"`
	MinIntervalMS   *int     `toml:"min_interval_ms"`
	AlwaysScoreWake *bool    `toml:"always_score_wake"`
}

func (r rawWake) empty() bool {
	return r.Engine == nil && r.Model == nil && r.Threshold == nil && r.VADEnabled == nil && r.VADThreshold == nil && r.VADLookbackMS == nil && r.PreRollMS == nil && r.MinIntervalMS == nil && r.AlwaysScoreWake == nil
}

func (r rawWake) complete() (protocol.WakeSettings, error) {
	if r.empty() || r.Engine == nil || r.Model == nil || r.Threshold == nil || r.VADEnabled == nil || r.VADThreshold == nil || r.VADLookbackMS == nil || r.PreRollMS == nil || r.MinIntervalMS == nil || r.AlwaysScoreWake == nil {
		return protocol.WakeSettings{}, errors.New("all wake settings are required")
	}
	value := protocol.WakeSettings{Engine: *r.Engine, Model: *r.Model, Threshold: *r.Threshold, VADEnabled: *r.VADEnabled, VADThreshold: *r.VADThreshold, VADLookbackMS: *r.VADLookbackMS, PreRollMS: *r.PreRollMS, MinIntervalMS: *r.MinIntervalMS, AlwaysScoreWake: *r.AlwaysScoreWake}
	if err := value.Validate(); err != nil {
		return protocol.WakeSettings{}, fmt.Errorf("validate wake settings: %w", err)
	}
	return value, nil
}

func (r rawWake) apply(value *protocol.WakeSettings) {
	if r.Engine != nil {
		value.Engine = *r.Engine
	}
	if r.Model != nil {
		value.Model = *r.Model
	}
	if r.Threshold != nil {
		value.Threshold = *r.Threshold
	}
	if r.VADEnabled != nil {
		value.VADEnabled = *r.VADEnabled
	}
	if r.VADThreshold != nil {
		value.VADThreshold = *r.VADThreshold
	}
	if r.VADLookbackMS != nil {
		value.VADLookbackMS = *r.VADLookbackMS
	}
	if r.PreRollMS != nil {
		value.PreRollMS = *r.PreRollMS
	}
	if r.MinIntervalMS != nil {
		value.MinIntervalMS = *r.MinIntervalMS
	}
	if r.AlwaysScoreWake != nil {
		value.AlwaysScoreWake = *r.AlwaysScoreWake
	}
}

type rawEndpointing struct {
	SpeechThreshold   *float64 `toml:"speech_threshold"`
	SpeechOnsetMS     *int     `toml:"speech_onset_ms"`
	TrailingSilenceMS *int     `toml:"trailing_silence_ms"`
	NoSpeechTimeoutMS *int     `toml:"no_speech_timeout_ms"`
	MaxTurnMS         *int     `toml:"max_turn_ms"`
}

func (r rawEndpointing) empty() bool {
	return r.SpeechThreshold == nil && r.SpeechOnsetMS == nil && r.TrailingSilenceMS == nil && r.NoSpeechTimeoutMS == nil && r.MaxTurnMS == nil
}
func (r rawEndpointing) complete() (protocol.EndpointingConfig, error) {
	if r.empty() || r.SpeechThreshold == nil || r.SpeechOnsetMS == nil || r.TrailingSilenceMS == nil || r.NoSpeechTimeoutMS == nil || r.MaxTurnMS == nil {
		return protocol.EndpointingConfig{}, errors.New("all endpointing settings are required")
	}
	value := protocol.EndpointingConfig{SpeechThreshold: *r.SpeechThreshold, SpeechOnsetMS: *r.SpeechOnsetMS, TrailingSilenceMS: *r.TrailingSilenceMS, NoSpeechTimeoutMS: *r.NoSpeechTimeoutMS, MaxTurnMS: *r.MaxTurnMS}
	if err := value.Validate(); err != nil {
		return protocol.EndpointingConfig{}, fmt.Errorf("validate endpointing settings: %w", err)
	}
	return value, nil
}
func (r rawEndpointing) apply(value *protocol.EndpointingConfig) {
	if r.SpeechThreshold != nil {
		value.SpeechThreshold = *r.SpeechThreshold
	}
	if r.SpeechOnsetMS != nil {
		value.SpeechOnsetMS = *r.SpeechOnsetMS
	}
	if r.TrailingSilenceMS != nil {
		value.TrailingSilenceMS = *r.TrailingSilenceMS
	}
	if r.NoSpeechTimeoutMS != nil {
		value.NoSpeechTimeoutMS = *r.NoSpeechTimeoutMS
	}
	if r.MaxTurnMS != nil {
		value.MaxTurnMS = *r.MaxTurnMS
	}
}

type rawLogs struct {
	ForwardLevel *protocol.LogLevel `toml:"forward_level"`
}

func (r rawLogs) empty() bool { return r.ForwardLevel == nil }
func (r rawLogs) complete() (protocol.LogSettings, error) {
	if r.ForwardLevel == nil {
		return protocol.LogSettings{}, errors.New("log forward level is required")
	}
	value := protocol.LogSettings{ForwardLevel: *r.ForwardLevel}
	if !value.ForwardLevel.Valid() {
		return protocol.LogSettings{}, fmt.Errorf("invalid log forward level %q", value.ForwardLevel)
	}
	return value, nil
}
func (r rawLogs) apply(value *protocol.LogSettings) {
	if r.ForwardLevel != nil {
		value.ForwardLevel = *r.ForwardLevel
	}
}

// DeviceIDs returns overridden IDs in deterministic order for diagnostics.
func (s Snapshot) DeviceIDs() []string {
	ids := slices.Collect(maps.Keys(s.overrides))
	slices.Sort(ids)
	return ids
}
