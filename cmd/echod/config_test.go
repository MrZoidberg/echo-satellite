package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/device/system"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestParseArgs_Defaults(t *testing.T) {
	o, err := parseArgs(nil)
	require.NoError(t, err)
	assert.Equal(t, discovery.ModeMDNS, o.Discovery)
	assert.Empty(t, o.GatewayURL)
	assert.False(t, o.Dbg)
}

func TestParseArgs_WakeOnlyDefaultsMatchWakeConfigDefaults(t *testing.T) {
	o, err := parseArgs([]string{"--wake-only"})
	require.NoError(t, err)

	assert.True(t, o.WakeOnly)
	assert.Equal(t, wake.Defaults(), o.wakeConfig())
	assert.Equal(t, system.WakeModelDir, o.WakeModelDir)
	assert.Equal(t, led.DefaultRoot, o.LEDRoot)
	assert.Equal(t, defaultStatsInterval, o.StatsInterval)
	assert.EqualValues(t, defaultLogMaxBytes, o.LogMaxBytes)
}

func TestParseArgs_WakeThresholdOutsideUnitRangeIsRejected(t *testing.T) {
	for _, value := range []string{"-0.01", "1.01"} {
		t.Run(value, func(t *testing.T) {
			_, err := parseArgs([]string{"--wake-threshold=" + value})
			require.Error(t, err)
			assert.ErrorIs(t, err, wake.ErrInvalidConfig)
		})
	}
}

func TestParseArgs_MicChannelsAcceptsCommaList(t *testing.T) {
	o, err := parseArgs([]string{"--mic-channels=0,2, 6"})
	require.NoError(t, err)

	channels, err := o.micChannelList()
	require.NoError(t, err)
	assert.Equal(t, []int{0, 2, 6}, channels)
}

func TestParseArgs_WakeBooleansCanBeDisabled(t *testing.T) {
	o, err := parseArgs([]string{"--vad-enabled=false", "--always-score-wake=false"})
	require.NoError(t, err)
	assert.False(t, o.VADEnabled.Bool())
	assert.False(t, o.AlwaysScoreWake.Bool())
}

func TestParseArgs_FlagBeatsIniForWakeThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "echod.ini")
	require.NoError(t, os.WriteFile(path, []byte("wake-threshold = 0.25\nvad-lookback-ms = 320\nstats-interval = 5s\n"), 0o600))

	o, err := parseArgs([]string{"--config=" + path, "--wake-threshold=0.75"})
	require.NoError(t, err)
	assert.InDelta(t, 0.75, o.WakeThreshold, 0.0001)
	assert.Equal(t, 320, o.VADLookbackMS)
	assert.Equal(t, 5*time.Second, o.StatsInterval)
}

func TestParseArgs_EnvironmentBeatsIniForWakeOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "echod.ini")
	require.NoError(t, os.WriteFile(path, []byte("wake-threshold = 0.25\nmic-channels = 1\n"), 0o600))
	t.Setenv("ECHOD_WAKE_THRESHOLD", "0.65")
	t.Setenv("ECHOD_MIC_CHANNELS", "2,3")

	o, err := parseArgs([]string{"--config=" + path})
	require.NoError(t, err)
	assert.InDelta(t, 0.65, o.WakeThreshold, 0.0001)
	assert.Equal(t, "2,3", o.MicChannels)
}

func TestParseArgs_RejectsInvalidWakeDurationsAndChannels(t *testing.T) {
	for _, args := range [][]string{
		{"--vad-lookback-ms=-1"},
		{"--vad-lookback-ms=10001"},
		{"--stats-interval=0s"},
		{"--log-max-bytes=0"},
		{"--mic-channels=7"},
		{"--mic-channels=1,1"},
	} {
		_, err := parseArgs(args)
		require.Error(t, err, args)
	}
}

func TestParseArgs_Flags(t *testing.T) {
	o, err := parseArgs([]string{
		"--device-id=dot-kitchen",
		"--discovery=disabled",
		"--gateway-url=wss://192.168.10.20:8770/device",
		"--preferred-server-id=home-gateway",
		"--dbg",
	})
	require.NoError(t, err)

	assert.Equal(t, "dot-kitchen", o.DeviceID)
	assert.True(t, o.Dbg)
	assert.Equal(t, discovery.Config{
		Discovery:         discovery.ModeDisabled,
		URL:               "wss://192.168.10.20:8770/device",
		PreferredServerID: "home-gateway",
	}, o.discoveryConfig())
}

func TestParseArgs_RejectsUnknownDiscoveryMode(t *testing.T) {
	_, err := parseArgs([]string{"--discovery=gateway-wake"})
	require.Error(t, err)
	assert.False(t, isHelpRequest(err))
}

func TestParseArgs_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "echod.ini")
	require.NoError(t, os.WriteFile(path, []byte(
		"device-id = dot-office\ndiscovery = disabled\npreferred-server-id = home-gateway\n"), 0o600))

	t.Run("values come from the file", func(t *testing.T) {
		o, err := parseArgs([]string{"--config=" + path})
		require.NoError(t, err)
		assert.Equal(t, "dot-office", o.DeviceID)
		assert.Equal(t, discovery.ModeDisabled, o.Discovery)
	})

	t.Run("command line wins over the file", func(t *testing.T) {
		o, err := parseArgs([]string{"--config=" + path, "--device-id=dot-kitchen"})
		require.NoError(t, err)
		assert.Equal(t, "dot-kitchen", o.DeviceID)
		assert.Equal(t, discovery.ModeDisabled, o.Discovery, "unset flags still come from the file")
	})
}

func TestParseArgs_MissingConfigFile(t *testing.T) {
	_, err := parseArgs([]string{"--config=" + filepath.Join(t.TempDir(), "absent.ini")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read config")
}

func TestAnnouncedCapabilities_AlwaysLocalWake(t *testing.T) {
	caps := announcedCapabilities()
	assert.True(t, caps.Has(protocol.CapWakeLocal), "wake detection is always device-local")
	assert.True(t, caps.Has(protocol.CapUpdateAB))
}
