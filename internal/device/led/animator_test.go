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
