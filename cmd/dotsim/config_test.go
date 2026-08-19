package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestParseArgs_Defaults(t *testing.T) {
	o, err := parseArgs(nil)
	require.NoError(t, err)

	assert.Equal(t, "dotsim-1", o.DeviceID)
	assert.Equal(t, discovery.ModeMDNS, o.Discover)
	assert.Equal(t, "wake", o.Trigger)
	assert.Equal(t, "okay_nabu", o.WakeModel)
	require.NoError(t, o.validate())
}

func TestParseArgs_DesignExample(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "question.wav")
	require.NoError(t, os.WriteFile(wav, []byte("RIFF"), 0o600))

	o, err := parseArgs([]string{
		"--discover=mdns",
		"--trigger=wake",
		"--wake-model=okay_nabu",
		"--wake-score=0.87",
		"--vad-score=0.93",
		"--mic=" + wav,
		"--speaker-out=./response.wav",
	})
	require.NoError(t, err)
	require.NoError(t, o.validate())

	turn, err := o.turnStart()
	require.NoError(t, err)
	assert.Equal(t, protocol.TurnStart{
		Trigger: protocol.TriggerWake, Model: "okay_nabu", WakeScore: 0.87, VADScore: 0.93,
	}, turn)
}

func TestTurnStart_ButtonCarriesNoWakeScores(t *testing.T) {
	o, err := parseArgs([]string{"--trigger=button"})
	require.NoError(t, err)

	turn, err := o.turnStart()
	require.NoError(t, err)
	assert.Equal(t, protocol.TriggerButton, turn.Trigger)
	assert.Zero(t, turn.WakeScore, "a button turn reports no wake score")
	assert.Empty(t, turn.Model)
}

func TestValidate_Errors(t *testing.T) {
	tests := map[string][]string{
		"wake score too high": {"--wake-score=1.5"},
		"wake score negative": {"--wake-score=-0.1"},
		"vad score too high":  {"--vad-score=2"},
		"empty wake model":    {"--wake-model="},
		"missing mic file":    {"--mic=/nonexistent/question.wav"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			o, err := parseArgs(args)
			require.NoError(t, err)
			assert.Error(t, o.validate())
		})
	}
}

func TestParseArgs_RejectsUnknownTrigger(t *testing.T) {
	_, err := parseArgs([]string{"--trigger=gateway"})
	require.Error(t, err, "the gateway never triggers a turn")
	assert.False(t, isHelpRequest(err))
}

func TestDiscoveryConfig(t *testing.T) {
	o, err := parseArgs([]string{"--discover=disabled", "--gateway-url=wss://gw.local:8770/device"})
	require.NoError(t, err)

	assert.Equal(t, discovery.Config{
		Discovery: discovery.ModeDisabled,
		URL:       "wss://gw.local:8770/device",
	}, o.discoveryConfig())
}
