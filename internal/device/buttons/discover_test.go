package buttons

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addTestDevice(t *testing.T, inputDir, sysDir, node, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, node), nil, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(sysDir, node, "device"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sysDir, node, "device", "name"), []byte(name+"\n"), 0o600))
}

func TestFindDevices_MatchesByDeviceName(t *testing.T) {
	root := t.TempDir()
	inputDir, sysDir := filepath.Join(root, "input"), filepath.Join(root, "sys")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	addTestDevice(t, inputDir, sysDir, "event2", "Echo Buttons")
	addTestDevice(t, inputDir, sysDir, "event1", "Other")

	got, err := FindDevices(inputDir, sysDir, []string{"echo buttons"})
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(inputDir, "event2")}, got)
}

func TestFindDevices_ReturnsErrNoInputDeviceWhenNoNameMatches(t *testing.T) {
	root := t.TempDir()
	inputDir, sysDir := filepath.Join(root, "input"), filepath.Join(root, "sys")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	addTestDevice(t, inputDir, sysDir, "event0", "Other")
	_, err := FindDevices(inputDir, sysDir, []string{"buttons"})
	assert.ErrorIs(t, err, ErrNoInputDevice)
}

func TestFindControlDevicesAssignsNonOverlappingHardwareKeys(t *testing.T) {
	root := t.TempDir()
	inputDir, sysDir := filepath.Join(root, "input"), filepath.Join(root, "sys")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	addTestDevice(t, inputDir, sysDir, "event1", DeviceNameKeypad)
	addTestDevice(t, inputDir, sysDir, "event2", DeviceNameGPIOKeys)

	got, err := FindControlDevices(inputDir, sysDir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []Key{KeyAction, KeyMute}, got[0].Keys)
	assert.Equal(t, []Key{KeyVolumeUp, KeyVolumeDown}, got[1].Keys)
}
