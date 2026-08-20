package led

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevice_WriteFrameSkipsIdenticalFrame(t *testing.T) {
	root := t.TempDir()
	device := New(root)
	frame := Uniform(RGB{R: 1})
	require.NoError(t, device.WriteFrame(frame))
	require.NoError(t, os.Chmod(filepath.Join(root, "frame"), 0o400))
	require.NoError(t, device.WriteFrame(frame), "a duplicate must not touch the read-only file")
	assert.Error(t, device.WriteFrame(Uniform(RGB{G: 1})))
}

func TestDevice_ControlsAndClear(t *testing.T) {
	root := t.TempDir()
	device := New(root)
	require.NoError(t, device.SetCurrent(42))
	require.NoError(t, device.SetBootAnimation(true))
	require.NoError(t, device.Clear())
	current, err := os.ReadFile(filepath.Join(root, "led_current")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, "42", string(current))
	boot, err := os.ReadFile(filepath.Join(root, "boot_animation")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, "1", string(boot))
	encoded, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, Frame{}.EncodeHex(), string(encoded))
}
