// Package config keeps the device's last known good gateway configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

var (
	ErrCorruptState       = errors.New("device config: corrupt persisted state")
	ErrStaleVersion       = errors.New("device config: stale version")
	ErrConflictingVersion = errors.New("device config: conflicting version")
)

// Settings is the typed local representation of a complete configuration.
// Version zero is the local, non-persisted bootstrap configuration.
type Settings struct {
	Version     uint64                     `json:"version"`
	Wake        wake.Config                `json:"wake"`
	Endpointing protocol.EndpointingConfig `json:"endpointing"`
	Logs        protocol.LogSettings       `json:"logs"`
}

// Bootstrap returns the local configuration used before a gateway has supplied
// versioned desired state.
func Bootstrap() Settings {
	return Settings{
		Wake: wake.Defaults(),
		Endpointing: protocol.EndpointingConfig{
			SpeechThreshold: 0.50, SpeechOnsetMS: 160, TrailingSilenceMS: 1500,
			NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000,
		},
		Logs: protocol.LogSettings{ForwardLevel: protocol.LogLevelInfo},
	}
}

// FromProtocol validates and converts gateway desired state without exposing
// protocol types throughout device code.
func FromProtocol(value protocol.DeviceConfig) (Settings, error) {
	if err := value.Validate(); err != nil {
		return Settings{}, fmt.Errorf("validate gateway config: %w", err)
	}
	settings := Settings{
		Version: value.Version,
		Wake: wake.Config{Enabled: true, Engine: value.Wake.Engine, Model: value.Wake.Model,
			Threshold: value.Wake.Threshold, VAD: wake.VADConfig{Enabled: value.Wake.VADEnabled,
				Threshold: value.Wake.VADThreshold, LookbackMS: value.Wake.VADLookbackMS},
			PreRollMS: value.Wake.PreRollMS, MinIntervalMS: value.Wake.MinIntervalMS,
			AlwaysScoreWake: value.Wake.AlwaysScoreWake},
		Endpointing: value.Endpointing, Logs: value.Logs,
	}
	if err := settings.Wake.Validate(); err != nil {
		return Settings{}, fmt.Errorf("validate local wake config: %w", err)
	}
	return settings, nil
}

// ToProtocol converts a non-bootstrap setting for acknowledgement and tests.
func (s Settings) ToProtocol() protocol.DeviceConfig {
	return protocol.DeviceConfig{Version: s.Version, Wake: protocol.WakeSettings{
		Engine: s.Wake.Engine, Model: s.Wake.Model, Threshold: s.Wake.Threshold,
		VADEnabled: s.Wake.VAD.Enabled, VADThreshold: s.Wake.VAD.Threshold,
		VADLookbackMS: s.Wake.VAD.LookbackMS, PreRollMS: s.Wake.PreRollMS,
		MinIntervalMS: s.Wake.MinIntervalMS, AlwaysScoreWake: s.Wake.AlwaysScoreWake,
	}, Endpointing: s.Endpointing, Logs: s.Logs}
}

// Store writes state with a staged file and the durability ordering required
// for recovery after power loss.
type Store struct{ Path string }

// Load returns bootstrap when no persisted state exists. Corrupt state is left
// in place for diagnosis and never silently overwritten.
func (s Store) Load(bootstrap Settings) (Settings, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return bootstrap, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read config state: %w", err)
	}
	var value Settings
	if err := json.Unmarshal(data, &value); err != nil || value.Validate() != nil {
		if err == nil {
			err = errors.New("invalid persisted settings")
		}
		return bootstrap, fmt.Errorf("%w: %w", ErrCorruptState, err)
	}
	return value, nil
}

// Save atomically replaces the persisted last-known-good gateway configuration.
func (s Store) Save(value Settings) error {
	if value.Version == 0 {
		return errors.New("device config: refusing to persist bootstrap version")
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate persisted config: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal config state: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(s.Path), 0o750)
	if err != nil {
		return fmt.Errorf("create config state directory: %w", err)
	}
	staged := s.Path + ".part"
	f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: path is supplied by the device composition root.
	if err != nil {
		return fmt.Errorf("open staged config state: %w", err)
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write staged config state: %w", err)
	}
	err = os.Rename(staged, s.Path)
	if err != nil {
		return fmt.Errorf("promote config state: %w", err)
	}
	// Windows does not support opening/syncing directories. The file was
	// durably flushed before rename; Unix additionally flushes the directory
	// entry to make the rename durable across power loss.
	if runtime.GOOS != "windows" {
		dir, err := os.Open(filepath.Dir(s.Path))
		if err != nil {
			return fmt.Errorf("open config state directory: %w", err)
		}
		if err := dir.Sync(); err != nil {
			_ = dir.Close()
			return fmt.Errorf("sync config state directory: %w", err)
		}
		if err := dir.Close(); err != nil {
			return fmt.Errorf("close config state directory: %w", err)
		}
	}
	return nil
}

func (s Settings) Validate() error {
	if s.Version == 0 {
		return errors.New("device config: version must be greater than zero")
	}
	if !s.Wake.Enabled {
		return errors.New("device config: gateway configuration cannot disable wake")
	}
	if err := s.ToProtocol().Validate(); err != nil {
		return fmt.Errorf("validate protocol representation: %w", err)
	}
	if err := s.Wake.Validate(); err != nil {
		return fmt.Errorf("validate local wake settings: %w", err)
	}
	return nil
}

// Compare reports whether candidate may replace current. Equal revisions must
// be byte-for-byte equivalent after typed decoding.
func Compare(current, candidate Settings) error {
	if candidate.Version < current.Version {
		return ErrStaleVersion
	}
	if candidate.Version == current.Version && !reflect.DeepEqual(current, candidate) {
		return ErrConflictingVersion
	}
	return nil
}
