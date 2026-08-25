package wake

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_DefaultsMatchDesignSection16(t *testing.T) {
	t.Parallel()
	config := Defaults()
	assert.True(t, config.Enabled)
	assert.Equal(t, "openwakeword", config.Engine)
	assert.Equal(t, "okay_nabu", config.Model)
	assert.InDelta(t, 0.80, config.Threshold, 0.0001)
	assert.True(t, config.VAD.Enabled)
	assert.InDelta(t, 0.50, config.VAD.Threshold, 0.0001)
	assert.Equal(t, 250, config.PreRollMS)
	assert.Equal(t, 2000, config.MinIntervalMS)
	assert.True(t, config.AlwaysScoreWake)
	require.NoError(t, config.Validate())
}

func TestConfig_ValidateRejectsThresholdOutsideUnitRange(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Config){
		"wake below": func(c *Config) { c.Threshold = -0.01 },
		"wake above": func(c *Config) { c.Threshold = 1.01 },
		"VAD below":  func(c *Config) { c.VAD.Threshold = -0.01 },
		"VAD above":  func(c *Config) { c.VAD.Threshold = 1.01 },
		"wake NaN":   func(c *Config) { c.Threshold = math.NaN() },
		"VAD inf":    func(c *Config) { c.VAD.Threshold = math.Inf(1) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := Defaults()
			mutate(&config)
			assert.ErrorIs(t, config.Validate(), ErrInvalidConfig)
		})
	}
}

func TestConfig_ToProtocolReportsOnlyEnabledModelAndVAD(t *testing.T) {
	t.Parallel()
	config := Defaults()
	got := config.ToProtocol()
	assert.Equal(t, []string{"okay_nabu"}, got.Models)
	assert.InDelta(t, 0.50, got.VADThreshold, 0.0001)

	config.Enabled = false
	config.VAD.Enabled = false
	got = config.ToProtocol()
	assert.Empty(t, got.Models)
	assert.Zero(t, got.VADThreshold)
}
