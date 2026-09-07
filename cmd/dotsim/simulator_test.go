package main

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	deviceconfig "github.com/MrZoidberg/echo-satellite/internal/device/config"
	"github.com/MrZoidberg/echo-satellite/internal/gateway/devices"
	"github.com/MrZoidberg/echo-satellite/internal/gateway/turns"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestSimConfigPersistsAndRejectsBadVersion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state", "config.json")
	config := newSimConfig(deviceconfig.Store{Path: statePath}, deviceconfig.Bootstrap())
	value := simDeviceConfig(1)
	assert.Equal(t, protocol.ConfigResultApplied, config.Apply(value).Status)
	assert.EqualValues(t, 1, config.current().Version)
	loaded := newSimConfig(deviceconfig.Store{Path: statePath}, deviceconfig.Bootstrap())
	require.NoError(t, loaded.load())
	assert.EqualValues(t, 1, loaded.current().Version)
	stale := simDeviceConfig(0)
	result := config.Apply(stale)
	assert.Equal(t, protocol.ConfigResultRejected, result.Status)
	assert.Equal(t, "invalid_config", result.Code)
	conflict := value
	conflict.Logs.ForwardLevel = protocol.LogLevelDebug
	result = config.Apply(conflict)
	assert.Equal(t, protocol.ConfigResultRejected, result.Status)
	assert.Equal(t, "conflicting_version", result.Code)
}

func TestWAVTurnsUsesPushedWakeSettingsAndOnlyReplaysOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.wav")
	file, err := os.Create(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, err)
	require.NoError(t, audio.WriteWAV(file, audio.Format{SampleRate: 16_000, Channels: 1, Layout: audio.LayoutS16LE}, make([]int16, 320)))
	require.NoError(t, file.Close())
	config := newSimConfig(deviceconfig.Store{Path: filepath.Join(t.TempDir(), "config.json")}, deviceconfig.Bootstrap())
	value := simDeviceConfig(1)
	value.Wake.Model = "pushed-model"
	value.Wake.PreRollMS = 321
	require.Equal(t, protocol.ConfigResultApplied, config.Apply(value).Status)
	source, err := newWAVTurns(opts{Mic: path, Trigger: "wake", WakeModel: "cli-model", WakeScore: .87, VADScore: .93}, config)
	require.NoError(t, err)
	turn, err := source.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "pushed-model", turn.Start.Model)
	assert.Equal(t, 321, turn.Start.PreRollMS)
	assert.InEpsilon(t, .87, turn.Start.WakeScore, .0001)
	assert.InEpsilon(t, .93, turn.Start.VADScore, .0001)
	assert.Equal(t, protocol.AudioStopEOF, turn.Reason)
	_, err = source.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestWAVTurnsRejectsWrongFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stereo.wav")
	file, err := os.Create(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, err)
	require.NoError(t, audio.WriteWAV(file, audio.Format{SampleRate: 16_000, Channels: 2, Layout: audio.LayoutS16LE}, []int16{1, 2}))
	require.NoError(t, file.Close())
	_, err = newWAVTurns(opts{Mic: path}, newSimConfig(deviceconfig.Store{}, deviceconfig.Bootstrap()))
	assert.ErrorContains(t, err, "mono 16 kHz")
}

func TestRun_ExplicitWSSSendsOneFixtureTurn(t *testing.T) {
	server, err := devices.New(devices.Options{Token: []byte("01234567890123456789012345678901"), ServerID: "test-gateway", Config: func(string) protocol.DeviceConfig { return simDeviceConfig(1) }, Turns: turns.Receiver{}})
	require.NoError(t, err)
	t.Cleanup(server.Close)
	httpServer := httptest.NewTLSServer(server)
	t.Cleanup(httpServer.Close)
	mic := writeSimWAV(t, []int16{0, 0, 0, 0})
	token := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(token, []byte("01234567890123456789012345678901"), 0o600))
	require.NoError(t, run(opts{DeviceID: "dotsim-test", Discover: "disabled", GatewayURL: "wss" + strings.TrimPrefix(httpServer.URL, "https") + "/device", Trigger: "button", Mic: mic, GatewayTokenFile: token, StateDir: t.TempDir(), DiscoveryTimeout: 100, TLSSkipVerify: true, Once: true}))
}

func writeSimWAV(t *testing.T, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.wav")
	file, err := os.Create(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, err)
	require.NoError(t, audio.WriteWAV(file, audio.Format{SampleRate: 16_000, Channels: 1, Layout: audio.LayoutS16LE}, samples))
	require.NoError(t, file.Close())
	return path
}

func simDeviceConfig(version uint64) protocol.DeviceConfig {
	return protocol.DeviceConfig{Version: version, Wake: protocol.WakeSettings{Engine: "openwakeword", Model: "okay_nabu", Threshold: .5, VADEnabled: true, VADThreshold: .5, VADLookbackMS: 1200, PreRollMS: 600, MinIntervalMS: 2000, AlwaysScoreWake: true}, Endpointing: protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, Logs: protocol.LogSettings{ForwardLevel: protocol.LogLevelInfo}}
}
