package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewayconfig "github.com/MrZoidberg/echo-satellite/internal/gateway/config"
)

func TestLogValue_RemovesRecordSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "gateway.example:8443", logValue("gateway.example\r\n:8443"))
}

func TestReloadProfile_RetainsActiveSnapshotAfterInvalidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.toml")
	valid := []byte("version = 1\n\n[defaults.wake]\nengine = 'wake'\nmodel = 'model'\nthreshold = 0.5\nvad_enabled = true\nvad_threshold = 0.5\nvad_lookback_ms = 1\npre_roll_ms = 1\nmin_interval_ms = 1\nalways_score_wake = true\n\n[defaults.endpointing]\nspeech_threshold = 0.5\nspeech_onset_ms = 1\ntrailing_silence_ms = 1\nno_speech_timeout_ms = 1\nmax_turn_ms = 1\n\n[defaults.logs]\nforward_level = 'info'\n")
	require.NoError(t, os.WriteFile(path, valid, 0o600))
	profile, err := loadProfile(path)
	require.NoError(t, err)
	store, err := gatewayconfig.NewStore(profile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("version = 2\nunknown = true\n"), 0o600))
	_, err = reloadProfile(store, path, []string{"dot-1"})
	require.Error(t, err)
	assert.EqualValues(t, 1, store.Snapshot().Version())
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(valid), "version = 1", "version = 2", 1)), 0o600))
	configs, err := reloadProfile(store, path, []string{"dot-1"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, configs["dot-1"].Version)
}

func TestLogValues_RemovesRecordSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"id=home", "proto=1"}, logValues([]string{"id=home\n", "proto=1\r"}))
}
