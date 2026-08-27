package wake

import (
	"errors"
	"fmt"
	"math"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

var ErrInvalidConfig = errors.New("wake: invalid config")

const MaxVADLookbackMS = 10_000

type VADConfig struct {
	Enabled    bool    `ini-name:"enabled"`
	Threshold  float64 `ini-name:"threshold"`
	LookbackMS int     `ini-name:"lookback-ms"`
}

// Config is backend-independent device-local wake configuration. Its fields are compatible with
// go-flags INI parsing when embedded in the echod composition root.
type Config struct {
	Enabled         bool      `ini-name:"enabled"`
	Engine          string    `ini-name:"engine"`
	Model           string    `ini-name:"model"`
	Threshold       float64   `ini-name:"threshold"`
	VAD             VADConfig `group:"vad" namespace:"vad"`
	PreRollMS       int       `ini-name:"preroll-ms"`
	MinIntervalMS   int       `ini-name:"min-interval-ms"`
	AlwaysScoreWake bool      `ini-name:"always-score-wake"`
}

// Defaults returns the production values qualified on Echo Dot Gen 2 hardware in Task 23.
func Defaults() Config {
	return Config{
		Enabled: true, Engine: KindOpenWakeWord.String(), Model: "okay_nabu",
		Threshold: 0.50, VAD: VADConfig{Enabled: true, Threshold: 0.50, LookbackMS: 1200},
		PreRollMS: 600, MinIntervalMS: 2000, AlwaysScoreWake: true,
	}
}

func (c Config) Validate() error {
	if _, err := ParseKind(c.Engine); err != nil {
		return fmt.Errorf("%w: engine: %w", ErrInvalidConfig, err)
	}
	if c.Enabled && c.Model == "" {
		return fmt.Errorf("%w: enabled wake requires a model", ErrInvalidConfig)
	}
	if err := validateUnit("wake threshold", c.Threshold); err != nil {
		return err
	}
	if err := validateUnit("VAD threshold", c.VAD.Threshold); err != nil {
		return err
	}
	if c.PreRollMS < 0 || c.MinIntervalMS < 0 || c.VAD.LookbackMS < 0 {
		return fmt.Errorf("%w: durations cannot be negative", ErrInvalidConfig)
	}
	if c.VAD.LookbackMS > MaxVADLookbackMS {
		return fmt.Errorf("%w: VAD lookback exceeds %dms", ErrInvalidConfig, MaxVADLookbackMS)
	}
	return nil
}

func validateUnit(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf("%w: %s %.4g is outside [0,1]", ErrInvalidConfig, name, value)
	}
	return nil
}

func (c Config) ToProtocol() protocol.WakeConfig {
	models := []string(nil)
	if c.Enabled && c.Model != "" {
		models = []string{c.Model}
	}
	vadThreshold := float64(0)
	if c.VAD.Enabled {
		vadThreshold = c.VAD.Threshold
	}
	return protocol.WakeConfig{
		Engine: c.Engine, Models: models, WakeThreshold: c.Threshold,
		VADThreshold: vadThreshold, PreRollMS: c.PreRollMS,
	}
}
