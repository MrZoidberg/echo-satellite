package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestStartLiveWakeIndicator_PlaysCueAndHoldsRedUntilCleared(t *testing.T) {
	root := wakeIndicatorRoot(t)
	originalSleep, originalPlay := wakeIndicatorSleep, playWakeStartAudio
	var played string
	var sequence []string
	wakeIndicatorSleep = func(_ time.Duration) {
		contents, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // path is beneath a test-owned temporary directory.
		require.NoError(t, err)
		sequence = append(sequence, string(contents))
	}
	playWakeStartAudio = func(path string) error {
		played = path
		contents, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // path is beneath a test-owned temporary directory.
		require.NoError(t, err)
		sequence = append(sequence, "audio:"+string(contents))
		return nil
	}
	t.Cleanup(func() {
		wakeIndicatorSleep = originalSleep
		playWakeStartAudio = originalPlay
	})

	clearIndicator, err := startLiveWakeIndicator(wakeInputOptions{LEDRoot: root, StartAudio: "starting.wav"})
	require.NoError(t, err)
	assert.Equal(t, "starting.wav", played)
	red := led.Render(protocol.StateMuted, 0).EncodeHex() + "\n"
	off := led.Frame{}.EncodeHex() + "\n"
	assert.Equal(t, []string{red, off, red, off, "audio:" + red}, sequence)
	assertFileContents(t, filepath.Join(root, "frame"), red)

	require.NoError(t, clearIndicator())
	assertFileContents(t, filepath.Join(root, "frame"), led.Frame{}.EncodeHex()+"\n")
}

func TestStartLiveWakeIndicator_ClearsLEDWhenAudioFails(t *testing.T) {
	root := wakeIndicatorRoot(t)
	wantErr := errors.New("speaker failed")
	originalSleep, originalPlay := wakeIndicatorSleep, playWakeStartAudio
	wakeIndicatorSleep = func(_ time.Duration) {}
	playWakeStartAudio = func(string) error { return wantErr }
	t.Cleanup(func() {
		wakeIndicatorSleep = originalSleep
		playWakeStartAudio = originalPlay
	})

	_, err := startLiveWakeIndicator(wakeInputOptions{LEDRoot: root, StartAudio: "starting.wav"})
	require.ErrorIs(t, err, wantErr)
	assertFileContents(t, filepath.Join(root, "frame"), led.Frame{}.EncodeHex()+"\n")
}

func TestStartLiveWakeIndicator_FileReplaySkipsHardware(t *testing.T) {
	clearIndicator, err := startLiveWakeIndicator(wakeInputOptions{FromFile: "fixture.wav"})
	require.NoError(t, err)
	require.NoError(t, clearIndicator())
}

func TestWakeFeedback_FlashesGreenThenRestoresRed(t *testing.T) {
	root := wakeIndicatorRoot(t)
	red := led.Render(protocol.StateMuted, 0).EncodeHex() + "\n"
	green := led.Render(protocol.StateListening, 3).EncodeHex() + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "frame"), []byte(red), 0o600))

	originalDuration := wakeFeedbackDuration
	wakeFeedbackDuration = time.Millisecond
	t.Cleanup(func() { wakeFeedbackDuration = originalDuration })

	flash, closeFeedback, err := startWakeFeedback(wakeInputOptions{LEDRoot: root}, true)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closeFeedback()) })
	require.NoError(t, flash())
	assertFileContents(t, filepath.Join(root, "frame"), green)
	assert.Eventually(t, func() bool {
		contents, readErr := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // path is beneath a test-owned temporary directory.
		return readErr == nil && string(contents) == red
	}, time.Second, time.Millisecond)
}

func TestStartWakeFeedback_FileReplayIsRejected(t *testing.T) {
	_, _, err := startWakeFeedback(wakeInputOptions{FromFile: "fixture.wav"}, true)
	require.Error(t, err)
}

func TestWakeFeedback_RestoreFailureIsReportedOnClose(t *testing.T) {
	root := wakeIndicatorRoot(t)
	originalDuration := wakeFeedbackDuration
	wakeFeedbackDuration = time.Millisecond
	t.Cleanup(func() { wakeFeedbackDuration = originalDuration })

	flash, closeFeedback, err := startWakeFeedback(wakeInputOptions{LEDRoot: root}, true)
	require.NoError(t, err)
	require.NoError(t, flash())
	require.NoError(t, os.Remove(filepath.Join(root, "frame")))
	require.NoError(t, os.Mkdir(filepath.Join(root, "frame"), 0o700))
	time.Sleep(10 * time.Millisecond)
	require.Error(t, closeFeedback())
}

func TestPlayWakeStartAudio_UsesQuarterVolume(t *testing.T) {
	original := runWakeStartSpeakerTest
	var got speakerTestCommand
	runWakeStartSpeakerTest = func(_ io.Writer, command speakerTestCommand) error {
		got = command
		return nil
	}
	t.Cleanup(func() { runWakeStartSpeakerTest = original })

	require.NoError(t, playWakeStartAudio("starting.wav"))
	assert.Equal(t, "starting.wav", got.In)
	assert.InDelta(t, 0.25, got.Volume, 0)
}

func wakeIndicatorRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"frame", "led_current", "boot_animation"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), nil, 0o600))
	}
	return root
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path) //nolint:gosec // path is beneath a test-owned temporary directory.
	require.NoError(t, err)
	assert.Equal(t, want, string(contents))
}
