package led

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestAnimator_AdvancesPatternOnEachTick(t *testing.T) {
	root := t.TempDir()
	ticks := make(chan time.Time)
	animator := NewAnimator(New(root), ticks)
	animator.Set(protocol.StateThinking)
	done := make(chan error, 1)
	go func() { done <- animator.Run(context.Background()) }()
	first := waitForFrame(t, filepath.Join(root, "frame"), "")
	ticks <- time.Now()
	second := waitForFrame(t, filepath.Join(root, "frame"), first)
	close(ticks)
	require.NoError(t, <-done)
	assert.NotEqual(t, first, second)
}

func TestAnimator_OffClearsAndSetResumesRendering(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"frame", "led_current", "boot_animation"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), nil, 0o600))
	}
	ticks := make(chan time.Time, 2)
	animator := NewAnimator(New(root), ticks)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- animator.Run(ctx) }()
	ticks <- time.Now()
	animator.Off()
	ticks <- time.Now()
	require.Eventually(t, func() bool {
		contents, err := os.ReadFile(filepath.Join(root, "frame")) //nolint:gosec // Test reads a fixed path under its private temporary directory.
		return err == nil && string(contents) == Frame{}.EncodeHex()+"\n"
	}, time.Second, time.Millisecond)
	animator.Set(protocol.StateOffline)
	cancel()
	require.NoError(t, <-done)
}

func waitForFrame(t *testing.T, path, previous string) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		encoded, err := os.ReadFile(path) //nolint:gosec // Test reads the path to its private temporary LED frame.
		if err == nil && string(encoded) != previous {
			return string(encoded)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for LED frame")
	return ""
}
