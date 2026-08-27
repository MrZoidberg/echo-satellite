package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestStartWakeOnlyTestIndicator_BlinksPlaysAndHoldsRed(t *testing.T) {
	root := wakeOnlyIndicatorRoot(t)
	originalSleep, originalPlay := wakeOnlyIndicatorSleep, playWakeOnlyStartAudio
	var sequence []string
	wakeOnlyIndicatorSleep = func(time.Duration) { sequence = append(sequence, wakeOnlyFrame(t, root)) }
	playWakeOnlyStartAudio = func(_ context.Context, path string) error {
		sequence = append(sequence, "audio:"+path+":"+wakeOnlyFrame(t, root))
		return nil
	}
	t.Cleanup(func() {
		wakeOnlyIndicatorSleep = originalSleep
		playWakeOnlyStartAudio = originalPlay
	})

	clearIndicator, err := startWakeOnlyTestIndicator(context.Background(), opts{LEDRoot: root, TestStartAudio: "starting.wav"})
	require.NoError(t, err)
	red := led.Render(protocol.StateMuted, 0).EncodeHex() + "\n"
	off := led.Frame{}.EncodeHex() + "\n"
	assert.Equal(t, []string{red, off, red, off, "audio:starting.wav:" + red}, sequence)
	require.NoError(t, clearIndicator())
	assert.Equal(t, off, wakeOnlyFrame(t, root))
}

func TestStartWakeOnlyTestIndicator_FileReplaySkipsHardware(t *testing.T) {
	clearIndicator, err := startWakeOnlyTestIndicator(context.Background(), opts{MicFromFile: "fixture.wav"})
	require.NoError(t, err)
	require.NoError(t, clearIndicator())
}

func TestScaleWakeOnlyCue_QuartersSignedPCM(t *testing.T) {
	samples := []int16{-32768, -3, -2, -1, 0, 1, 2, 3, 32767}
	scaleWakeOnlyCue(samples)
	assert.Equal(t, []int16{-8192, 0, 0, 0, 0, 0, 0, 0, 8191}, samples)
}

func wakeOnlyIndicatorRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"frame", "led_current", "boot_animation"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), nil, 0o600))
	}
	return root
}

func wakeOnlyFrame(t *testing.T, root string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // path is beneath a test-owned temporary directory.
	require.NoError(t, err)
	return string(contents)
}
