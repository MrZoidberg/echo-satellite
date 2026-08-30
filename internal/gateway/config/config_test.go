package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestLoad_MergesPartialDeviceOverrides(t *testing.T) {
	profile, err := loadFixture(t, "valid.toml")
	require.NoError(t, err)

	assert.Equal(t, uint64(7), profile.Version())
	assert.Equal(t, []string{"dot-kitchen"}, profile.DeviceIDs())
	defaults := profile.Effective("dot-bedroom")
	kitchen := profile.Effective("dot-kitchen")
	assert.Equal(t, 1500, defaults.Endpointing.TrailingSilenceMS)
	assert.Equal(t, 1000, kitchen.Endpointing.TrailingSilenceMS)
	assert.Equal(t, protocol.LogLevelInfo, defaults.Logs.ForwardLevel)
	assert.Equal(t, protocol.LogLevelDebug, kitchen.Logs.ForwardLevel)
	assert.Equal(t, defaults.Wake, kitchen.Wake)
	require.NoError(t, kitchen.Validate())
}

func TestLoad_RejectsInvalidProfiles(t *testing.T) {
	valid, err := os.ReadFile(filepath.Join("testdata", "valid.toml"))
	require.NoError(t, err)
	cases := map[string]string{
		"unknown fields":             string(mustRead(t, "invalid-unknown.toml")),
		"secret-like fields":         string(valid) + "\ntoken = \"must-not-live-here\"\n",
		"version zero":               stringsReplace(string(valid), "version = 7", "version = 0"),
		"incomplete defaults":        stringsReplace(string(valid), "min_interval_ms = 2000\n", ""),
		"empty override":             string(valid) + "\n[devices.empty]\n",
		"invalid effective override": string(valid) + "\n[devices.bad.endpointing]\nmax_turn_ms = 0\n",
		"unknown override field":     string(valid) + "\n[devices.bad.logs]\nunknown = \"value\"\n",
		"empty device ID":            string(valid) + "\n[devices.\" \".logs]\nforward_level = \"info\"\n",
		"duplicate override table":   string(valid) + "\n[devices.\"dot-kitchen\".logs]\nforward_level = \"warn\"\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) { _, err := Load(bytes.NewBufferString(input)); assert.Error(t, err) })
	}
}

func TestStore_ReloadIsMonotonicAtomicAndReturnsConnectedConfigs(t *testing.T) {
	initial, err := loadFixture(t, "valid.toml")
	require.NoError(t, err)
	store, err := NewStore(initial)
	require.NoError(t, err)

	_, err = store.Reload(bytes.NewBufferString("version = 8\n[defaults]\n"), []string{"dot-kitchen"})
	require.Error(t, err)
	assert.Equal(t, uint64(7), store.Snapshot().Version(), "invalid reload must not replace active state")

	_, err = store.Reload(bytes.NewBufferString(string(mustRead(t, "valid.toml"))), nil)
	require.ErrorIs(t, err, ErrNonMonotonicVersion)

	next := stringsReplace(string(mustRead(t, "valid.toml")), "version = 7", "version = 8")
	affected, err := store.Reload(bytes.NewBufferString(next), []string{"dot-kitchen", "dot-bedroom", "dot-kitchen"})
	require.NoError(t, err)
	assert.Equal(t, uint64(8), store.Snapshot().Version())
	assert.Len(t, affected, 2)
	assert.Equal(t, uint64(8), affected["dot-kitchen"].Version)
}

func TestSnapshot_IsImmutable(t *testing.T) {
	profile, err := loadFixture(t, "valid.toml")
	require.NoError(t, err)
	store, err := NewStore(profile)
	require.NoError(t, err)

	snapshotCopy := store.Snapshot()
	snapshotCopy.overrides["injected"] = override{}
	assert.Equal(t, []string{"dot-kitchen"}, store.Snapshot().DeviceIDs())
}

func TestNewStore_RejectsUnvalidatedInitialProfile(t *testing.T) {
	_, err := NewStore(Snapshot{})
	require.Error(t, err)

	var store Store
	_, err = store.Reload(bytes.NewBufferString(string(mustRead(t, "valid.toml"))), nil)
	require.ErrorIs(t, err, ErrUninitializedStore)
}

func loadFixture(t *testing.T, name string) (Snapshot, error) {
	t.Helper()
	return Load(bytes.NewReader(mustRead(t, name)))
}
func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // G304: name is a test-controlled fixture basename.
	require.NoError(t, err)
	return data
}
func stringsReplace(value, old, replacement string) string {
	return strings.NewReplacer(old, replacement).Replace(value)
}
