package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerial_PrefersSoc0OverCmdline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/sys/devices/soc0/serial_number", " SOC-123\n")
	writeRootFile(t, root, "/proc/cmdline", "androidboot.serialno=CMD-456")
	writeRootFile(t, root, "/proc/device-tree/serial-number", "TREE-789\x00")

	serial, err := (SerialReader{Root: root}).Read()
	require.NoError(t, err)
	assert.Equal(t, "SOC-123", serial)
}

func TestSerial_ParsesAndroidbootSerialnoFromCmdline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/proc/cmdline", "console=tty0 androidboot.serialno=CMD-456 root=/dev/dm-0")

	serial, err := (SerialReader{Root: root}).Read()
	require.NoError(t, err)
	assert.Equal(t, "CMD-456", serial)
}

func TestSerial_TrimsNULFromDeviceTreeValue(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/proc/device-tree/serial-number", "\x00TREE-789\x00\n")

	serial, err := (SerialReader{Root: root}).Read()
	require.NoError(t, err)
	assert.Equal(t, "TREE-789", serial)
}

func TestSerial_ReturnsErrNoSerialWhenNoSourceExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRootFile(t, root, "/sys/devices/soc0/serial_number", " \n")
	writeRootFile(t, root, "/proc/cmdline", "console=tty0")

	_, err := (SerialReader{Root: root}).Read()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoSerial)
}

func TestSerial_ReportsUnreadableSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sys", "devices", "soc0", "serial_number"), 0o700))

	_, err := (SerialReader{Root: root}).Read()
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoSerial)
}

func TestSerial_EmptyRootAddressesHostRoot(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.FromSlash("/proc/cmdline"), (SerialReader{}).path("/proc/cmdline"))
}

func writeRootFile(t *testing.T, root, name, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name[1:]))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}
