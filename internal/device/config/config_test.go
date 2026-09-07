package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromProtocolAndRoundTrip(t *testing.T) {
	settings, err := FromProtocol(testProtocolConfig(7))
	require.NoError(t, err)
	assert.Equal(t, testProtocolConfig(7), settings.ToProtocol())

	invalid := testProtocolConfig(7)
	invalid.Wake.VADLookbackMS = 20_000
	_, err = FromProtocol(invalid)
	assert.Error(t, err)
}

func TestStoreSaveLoadAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config.json")
	store := Store{Path: path}
	bootstrap := Bootstrap()
	got, err := store.Load(bootstrap)
	require.NoError(t, err)
	assert.Equal(t, bootstrap, got)

	want, err := FromProtocol(testProtocolConfig(3))
	require.NoError(t, err)
	require.NoError(t, store.Save(want))
	got, err = store.Load(bootstrap)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.NoFileExists(t, path+".part")

	// A crashed staged write is never promoted over the last known good state.
	require.NoError(t, os.WriteFile(path+".part", []byte(`{"version":999`), 0o600))
	got, err = store.Load(bootstrap)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStorePreservesCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
	bootstrap := Bootstrap()
	got, err := (Store{Path: path}).Load(bootstrap)
	require.ErrorIs(t, err, ErrCorruptState)
	assert.Equal(t, bootstrap, got)
	data, readErr := os.ReadFile(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, readErr)
	assert.Equal(t, []byte("not json"), data)
}

func TestCompare(t *testing.T) {
	current, err := FromProtocol(testProtocolConfig(2))
	require.NoError(t, err)
	assert.NoError(t, Compare(current, current))
	stale, err := FromProtocol(testProtocolConfig(1))
	require.NoError(t, err)
	require.ErrorIs(t, Compare(current, stale), ErrStaleVersion)
	conflicting := current
	conflicting.Logs.ForwardLevel = protocol.LogLevelDebug
	assert.ErrorIs(t, Compare(current, conflicting), ErrConflictingVersion)
}

func testProtocolConfig(version uint64) protocol.DeviceConfig {
	return protocol.DeviceConfig{Version: version, Wake: protocol.WakeSettings{
		Engine: "openwakeword", Model: "okay_nabu", Threshold: 0.5, VADEnabled: true,
		VADThreshold: 0.5, VADLookbackMS: 1200, PreRollMS: 600, MinIntervalMS: 2000,
		AlwaysScoreWake: true,
	}, Endpointing: protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160,
		TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000,
	}, Logs: protocol.LogSettings{ForwardLevel: protocol.LogLevelInfo}}
}

func TestFromProtocolRejectsNonFinite(t *testing.T) {
	value := testProtocolConfig(1)
	value.Endpointing.SpeechThreshold = math.NaN()
	_, err := FromProtocol(value)
	assert.Error(t, err)
}
