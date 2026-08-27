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

func TestWatcher_GeneratesVolumeRepeatsUntilRelease(t *testing.T) {
	reader, writer := io.Pipe()
	out := make(chan Press, 4)
	done := make(chan error, 1)
	go func() { done <- NewWatcher(reader).Run(context.Background(), out) }()
	_, err := writer.Write(encodeTestEvent(time.Now(), evTypeKey, KeyVolumeUp, 1))
	require.NoError(t, err)
	assert.Equal(t, ActionTap, (<-out).Action)
	select {
	case press := <-out:
		assert.Equal(t, ActionRepeat, press.Action)
	case <-time.After(RepeatInterval + 300*time.Millisecond):
		t.Fatal("volume repeat was not emitted")
	}
	_, err = writer.Write(encodeTestEvent(time.Now(), evTypeKey, KeyVolumeUp, 0))
	require.NoError(t, err)
	select {
	case press := <-out:
		t.Fatalf("unexpected press after release: %+v", press)
	case <-time.After(RepeatInterval + 100*time.Millisecond):
	}
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}

func TestWatcher_FiltersKeysByDeviceMapping(t *testing.T) {
	at := time.Unix(1, 0)
	data := append(encodeTestEvent(at, evTypeKey, KeyVolumeDown, 1), encodeTestEvent(at, evTypeKey, KeyAction, 1)...)
	out := make(chan Press, 2)
	require.NoError(t, NewWatcher(io.NopCloser(bytes.NewReader(data)), KeyAction, KeyMute).Run(context.Background(), out))
	close(out)
	got := make([]Press, 0, 1)
	for press := range out {
		got = append(got, press)
	}
	assert.Empty(t, got, "Action emits only on release; filtered Volume Down must emit nothing")
}

func TestWatcher_StaleCallbacksCannotAttachToRepress(t *testing.T) {
	tests := []struct {
		name string
		key  Key
	}{
		{name: "volume repeat", key: KeyVolumeUp},
		{name: "action hold", key: KeyAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watcher := NewWatcher(io.NopCloser(bytes.NewReader(nil)))
			ctx := context.Background()
			out := make(chan Press, 4)
			holdDue, repeatDue := make(chan timerDue, 2), make(chan timerDue, 2)
			timers := make(map[Key]*time.Timer)
			repeatHeld := make(map[Key]time.Duration)
			generations := make(map[Key]uint64)
			start := time.Unix(1, 0)

			require.True(t, watcher.handleEvent(ctx, out, holdDue, repeatDue, timers, repeatHeld, generations, rawEvent{type_: evTypeKey, code: uint16(tt.key), value: 1, at: start}))
			stale := timerDue{key: tt.key, generation: generations[tt.key]}
			require.True(t, watcher.handleEvent(ctx, out, holdDue, repeatDue, timers, repeatHeld, generations, rawEvent{type_: evTypeKey, code: uint16(tt.key), value: 0, at: start.Add(time.Millisecond)}))
			require.True(t, watcher.handleEvent(ctx, out, holdDue, repeatDue, timers, repeatHeld, generations, rawEvent{type_: evTypeKey, code: uint16(tt.key), value: 1, at: start.Add(2 * time.Millisecond)}))

			if tt.key == KeyVolumeUp {
				require.True(t, watcher.handleRepeat(ctx, out, repeatDue, timers, repeatHeld, generations, stale))
			} else {
				require.True(t, watcher.handleHold(ctx, out, timers, generations, stale))
			}
			stopTimers(timers)
			close(out)
			for press := range out {
				assert.NotEqual(t, ActionRepeat, press.Action, "stale callback emitted an early repeat")
				assert.NotEqual(t, ActionHoldStart, press.Action, "stale callback emitted an early hold")
			}
		})
	}
}

func TestWatcher_KernelRepeatDisablesGeneratedRepeats(t *testing.T) {
	reader, writer := io.Pipe()
	out := make(chan Press, 4)
	done := make(chan error, 1)
	go func() { done <- NewWatcher(reader).Run(context.Background(), out) }()
	start := time.Now()
	_, err := writer.Write(encodeTestEvent(start, evTypeKey, KeyVolumeUp, 1))
	require.NoError(t, err)
	assert.Equal(t, ActionTap, (<-out).Action)
	_, err = writer.Write(encodeTestEvent(start.Add(50*time.Millisecond), evTypeKey, KeyVolumeUp, 2))
	require.NoError(t, err)
	assert.Equal(t, ActionRepeat, (<-out).Action)
	select {
	case press := <-out:
		t.Fatalf("unexpected generated repeat after kernel repeat: %+v", press)
	case <-time.After(RepeatInterval + 100*time.Millisecond):
	}
	_, err = writer.Write(encodeTestEvent(start.Add(time.Second), evTypeKey, KeyVolumeUp, 0))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
}
