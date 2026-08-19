package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestParseArgs_Defaults(t *testing.T) {
	o, err := parseArgs(nil)
	require.NoError(t, err)

	assert.Equal(t, ":8770", o.Listen)
	assert.Equal(t, "/device", o.Path)
	assert.False(t, o.AllowUnsignedDevBuilds, "the dev escape hatch is off unless asked for")
}

func TestAdvertisement(t *testing.T) {
	o, err := parseArgs([]string{"--server-id=home-gateway", "--hostname=echo-gateway.local.", "--tls"})
	require.NoError(t, err)

	inst, err := o.advertisement()
	require.NoError(t, err)

	assert.Equal(t, "home-gateway", inst.ServerID)
	assert.Equal(t, discovery.DefaultPort, inst.Port)
	assert.Equal(t, protocol.ProtocolVersion, inst.TXT.Protocol)
	assert.Equal(t, []string{
		"protocol=1",
		"server_id=home-gateway",
		"tls=1",
		"path=/device",
	}, inst.TXT.Encode())

	endpoint, err := inst.EndpointURL()
	require.NoError(t, err)
	assert.Equal(t, "wss://echo-gateway.local.:8770/device", endpoint)
}

func TestAdvertisement_CarriesNoSecrets(t *testing.T) {
	o, err := parseArgs(nil)
	require.NoError(t, err)
	inst, err := o.advertisement()
	require.NoError(t, err)

	// whatever the gateway advertises must survive the parser's secret check
	_, err = discovery.ParseTXT(inst.TXT.Encode())
	require.NoError(t, err)
}

func TestAdvertisement_BadListenAddress(t *testing.T) {
	for _, listen := range []string{"8770", ":", ":0", ":not-a-port", ":70000"} {
		t.Run(listen, func(t *testing.T) {
			o, err := parseArgs([]string{"--listen=" + listen})
			require.NoError(t, err)
			_, err = o.advertisement()
			assert.Error(t, err)
		})
	}
}

func TestTrustPolicy_EscapeHatchIsSurfaced(t *testing.T) {
	secure, err := parseArgs(nil)
	require.NoError(t, err)
	dev, err := parseArgs([]string{"--allow-unsigned-dev-builds"})
	require.NoError(t, err)

	assert.True(t, dev.trustPolicy().AllowUnsignedDevBuilds)
	notes := dev.trustPolicy().StatusNotes()
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[0], "unsigned")

	assert.False(t, secure.trustPolicy().AllowUnsignedDevBuilds)
}
