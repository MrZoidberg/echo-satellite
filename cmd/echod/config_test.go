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
	assert.Equal(t, discovery.ModeMDNS, o.Discovery)
	assert.Empty(t, o.GatewayURL)
	assert.False(t, o.Dbg)
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
