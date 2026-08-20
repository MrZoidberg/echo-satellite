package buttons

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcher_IgnoresNonKeyEventTypes(t *testing.T) {
	at := time.Unix(1, 0)
	data := append(encodeTestEvent(at, 0, KeyAction, 1), encodeTestEvent(at, evTypeKey, KeyVolumeDown, 1)...)
	out := make(chan Press, 2)
	require.NoError(t, NewWatcher(io.NopCloser(bytes.NewReader(data))).Run(context.Background(), out))
	close(out)
	got := make([]Press, 0, 1)
	for press := range out {
		got = append(got, press)
	}
	require.Len(t, got, 1)
	assert.Equal(t, KeyVolumeDown, got[0].Key)
}

func TestWatcher_StopsOnContextCancel(t *testing.T) {
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewWatcher(reader).Run(ctx, make(chan Press)) }()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watcher did not interrupt its blocked read")
	}
	_ = writer.Close()
}

func TestWatcher_EmitsHoldStartAtThresholdBeforeRelease(t *testing.T) {
	reader, writer := io.Pipe()
	out := make(chan Press, 2)
	done := make(chan error, 1)
	go func() { done <- NewWatcher(reader).Run(context.Background(), out) }()
	_, err := writer.Write(encodeTestEvent(time.Unix(1, 0), evTypeKey, KeyAction, 1))
	require.NoError(t, err)
	select {
	case press := <-out:
		assert.Equal(t, ActionHoldStart, press.Action)
	case <-time.After(HoldThreshold + 300*time.Millisecond):
		t.Fatal("hold-start was not emitted at the threshold")
	}
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}
