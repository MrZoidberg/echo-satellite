package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestLEDTest_AllStatesWritesFrameAndCurrent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent", "led")
	var report bytes.Buffer
	require.NoError(t, ledTest(&report, ledTestCommand{Root: root, AllStates: true, Seconds: 0.001, Current: 23}))
	frame, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Len(t, frame, led.Channels*2)
	current, err := os.ReadFile(filepath.Join(root, "led_current")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, "23", string(current))
	for _, state := range protocol.AllDeviceStates() {
		assert.Contains(t, report.String(), "state: "+string(state))
	}
}

func TestLEDTest_ClearLeavesRingOff(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, ledTest(&bytes.Buffer{}, ledTestCommand{Root: root, State: "muted", Seconds: 0.001, Clear: true}))
	frame, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // Test reads a fixed name under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, led.Frame{}.EncodeHex(), string(frame))
}

func TestLEDTest_RejectsUnknownStateAndDuration(t *testing.T) {
	require.Error(t, ledTest(&bytes.Buffer{}, ledTestCommand{Root: t.TempDir(), State: "bogus", Seconds: 1}))
	require.Error(t, ledTest(&bytes.Buffer{}, ledTestCommand{Root: t.TempDir(), State: "idle", Seconds: 0}))
}
