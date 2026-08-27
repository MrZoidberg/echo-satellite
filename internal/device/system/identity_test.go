package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_ExplicitOverrideBeatsSerial(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/sys/devices/soc0/serial_number", "hardware-serial")
	identity, err := Resolve(SerialReader{Root: root}, "Kitchen Dot", filepath.Join(root, "device-id"))
	require.NoError(t, err)
	assert.Equal(t, Identity{DeviceID: "kitchen-dot"}, identity)
}

func TestResolve_PersistsGeneratedIDForStabilityAcrossRestarts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "config", "device-id")
	first, err := Resolve(SerialReader{Root: root}, "", path)
	require.NoError(t, err)
	require.NotEmpty(t, first.DeviceID)
	second, err := Resolve(SerialReader{Root: root}, "", path)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestResolve_SanitizesSerialIntoDeviceID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/sys/devices/soc0/serial_number", " G090LF/09__64060EHP ")
	identity, err := Resolve(SerialReader{Root: root}, "", filepath.Join(root, "device-id"))
	require.NoError(t, err)
	assert.Equal(t, Identity{Serial: "G090LF/09__64060EHP", DeviceID: "g090lf-09-64060ehp"}, identity)
}

func TestResolve_UsesPersistedIDWhenSerialUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "device-id")
	require.NoError(t, os.WriteFile(path, []byte("persisted-id\n"), 0o600))
	identity, err := Resolve(SerialReader{Root: root}, "", path)
	require.NoError(t, err)
	assert.Equal(t, Identity{DeviceID: "persisted-id"}, identity)
}

func TestResolve_RejectsOverrideWithoutUsableCharacters(t *testing.T) {
	t.Parallel()

	_, err := Resolve(SerialReader{Root: t.TempDir()}, "___", filepath.Join(t.TempDir(), "device-id"))
	require.Error(t, err)
}

func TestResolve_UsesASCIIOnlyDeviceID(t *testing.T) {
	t.Parallel()

	identity, err := Resolve(SerialReader{Root: t.TempDir()}, "Кухня Dot", filepath.Join(t.TempDir(), "device-id"))
	require.NoError(t, err)
	assert.Equal(t, "-dot", identity.DeviceID)
}

func TestResolve_PreservesValidHyphens(t *testing.T) {
	t.Parallel()

	identity, err := Resolve(SerialReader{Root: t.TempDir()}, "A--B/C", filepath.Join(t.TempDir(), "device-id"))
	require.NoError(t, err)
	assert.Equal(t, "a--b-c", identity.DeviceID)
}

func TestResolve_ReplacesBlankPersistedID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "device-id")
	require.NoError(t, os.WriteFile(path, []byte("\n"), 0o600))
	identity, err := Resolve(SerialReader{Root: root}, "", path)
	require.NoError(t, err)
	assert.NotEmpty(t, identity.DeviceID)
	contents, err := os.ReadFile(path) //nolint:gosec // G304: path is confined to this test's temporary directory.
	require.NoError(t, err)
	assert.Equal(t, identity.DeviceID+"\n", string(contents))
}
