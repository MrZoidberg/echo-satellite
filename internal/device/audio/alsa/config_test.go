package alsa

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_RejectsZeroPeriodFrames(t *testing.T) {
	t.Parallel()

	config := micConfig()
	config.PeriodFrames = 0
	require.Error(t, config.Validate())
}

func TestConfig_ValidatesMeasuredDeviceConfigurations(t *testing.T) {
	t.Parallel()

	require.NoError(t, micConfig().Validate())
	require.NoError(t, speakerConfig().Validate())
}

func TestConfig_RejectsInvalidFields(t *testing.T) {
	t.Parallel()

	base := micConfig()
	mutations := map[string]func(*Config){
		"card":     func(c *Config) { c.Card = -1 },
		"device":   func(c *Config) { c.Device = -1 },
		"rate":     func(c *Config) { c.Rate = 0 },
		"channels": func(c *Config) { c.Channels = 0 },
		"format":   func(c *Config) { c.Format = 99 },
		"periods":  func(c *Config) { c.Periods = 0 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := base
			mutate(&config)
			require.Error(t, config.Validate())
		})
	}
}

func TestDevicePath_UsesCaptureSuffixForCapture(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/dev/snd/pcmC0D24c", DevicePath(micConfig()))
	assert.Equal(t, "/dev/snd/pcmC0D23p", DevicePath(speakerConfig()))
}

func TestConfig_StartsCaptureButLetsPlaybackStartOnWrite(t *testing.T) {
	t.Parallel()

	assert.True(t, micConfig().startsOnOpen())
	assert.False(t, speakerConfig().startsOnOpen())
}

func TestConfig_XRunRecoveryRestartsCaptureButNotPlayback(t *testing.T) {
	t.Parallel()

	var captureCalls []string
	require.NoError(t, micConfig().recoverXRun(
		func() error { captureCalls = append(captureCalls, "prepare"); return nil },
		func() error { captureCalls = append(captureCalls, "start"); return nil },
	))
	assert.Equal(t, []string{"prepare", "start"}, captureCalls)

	var playbackCalls []string
	require.NoError(t, speakerConfig().recoverXRun(
		func() error { playbackCalls = append(playbackCalls, "prepare"); return nil },
		func() error { playbackCalls = append(playbackCalls, "start"); return nil },
	))
	assert.Equal(t, []string{"prepare"}, playbackCalls)
}

func TestConfig_XRunRecoveryReturnsSequenceErrors(t *testing.T) {
	t.Parallel()

	prepareErr := errors.New("prepare")
	startErr := errors.New("start")
	require.ErrorIs(t, micConfig().recoverXRun(func() error { return prepareErr }, func() error {
		t.Fatal("start called after prepare failure")
		return nil
	}), prepareErr)
	require.ErrorIs(t, micConfig().recoverXRun(func() error { return nil }, func() error { return startErr }), startErr)
}

func micConfig() Config {
	return Config{Card: MicCard, Device: MicDevice, Rate: MicRate, Channels: MicChannels, Format: MicFormat, PeriodFrames: MicPeriodFrames, Periods: MicPeriods, Capture: true}
}

func speakerConfig() Config {
	return Config{Card: MicCard, Device: SpeakerDevice, Rate: SpeakerRate, Channels: SpeakerChannels, Format: SpeakerFormat, PeriodFrames: SpeakerPeriodFrames, Periods: SpeakerPeriods}
}
