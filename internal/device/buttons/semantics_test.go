package buttons

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecognizer_ActionHeldBelow700msEmitsTapOnRelease(t *testing.T) {
	var r Recognizer
	start := time.Unix(1, 0)
	assert.Empty(t, r.Feed(KeyAction, 1, start))
	got := r.Feed(KeyAction, 0, start.Add(699*time.Millisecond))
	require.Len(t, got, 1)
	assert.Equal(t, ActionTap, got[0].Action)
}

func TestRecognizer_ActionHeldAbove700msEmitsHoldStartThenHoldEnd(t *testing.T) {
	var r Recognizer
	start := time.Unix(1, 0)
	r.Feed(KeyAction, 1, start)
	started := r.startHold(KeyAction)
	require.Len(t, started, 1)
	assert.Equal(t, ActionHoldStart, started[0].Action)
	assert.Equal(t, start.Add(HoldThreshold), started[0].At)
	ended := r.Feed(KeyAction, 0, start.Add(2*time.Second))
	require.Len(t, ended, 1)
	assert.Equal(t, ActionHoldEnd, ended[0].Action)
	assert.Equal(t, 2*time.Second, ended[0].Held)
}

func TestRecognizer_StartHoldIgnoresMissingVolumeAndDuplicate(t *testing.T) {
	var r Recognizer
	assert.Empty(t, r.startHold(KeyAction))
	r.Feed(KeyVolumeDown, 1, time.Unix(1, 0))
	assert.Empty(t, r.startHold(KeyVolumeDown))
	r.Feed(KeyMute, 1, time.Unix(1, 0))
	require.Len(t, r.startHold(KeyMute), 1)
	assert.Empty(t, r.startHold(KeyMute))
}

func TestRecognizer_MuteFollowsTapHoldSemantics(t *testing.T) {
	var r Recognizer
	start := time.Unix(1, 0)
	r.Feed(KeyMute, 1, start)
	got := r.Feed(KeyMute, 0, start.Add(time.Second))
	assert.Equal(t, []Action{ActionHoldStart, ActionHoldEnd}, []Action{got[0].Action, got[1].Action})
}

func TestRecognizer_VolumeKeyRepeatRamps(t *testing.T) {
	var r Recognizer
	start := time.Unix(1, 0)
	assert.Equal(t, ActionTap, r.Feed(KeyVolumeUp, 1, start)[0].Action)
	got := r.Feed(KeyVolumeUp, 2, start.Add(100*time.Millisecond))
	require.Len(t, got, 1)
	assert.Equal(t, ActionRepeat, got[0].Action)
	assert.Equal(t, 100*time.Millisecond, got[0].Held)
	assert.Empty(t, r.Feed(KeyVolumeUp, 0, start.Add(time.Second)))
}

func TestRecognizer_GeneratedVolumeRepeatStopsAfterRelease(t *testing.T) {
	var r Recognizer
	start := time.Unix(1, 0)
	r.Feed(KeyVolumeDown, 1, start)
	repeated := r.repeat(KeyVolumeDown, RepeatInterval)
	require.Len(t, repeated, 1)
	assert.Equal(t, ActionRepeat, repeated[0].Action)
	assert.Equal(t, RepeatInterval, repeated[0].Held)
	r.Feed(KeyVolumeDown, 0, start.Add(RepeatInterval+time.Millisecond))
	assert.Empty(t, r.repeat(KeyVolumeDown, 2*RepeatInterval))
}

func TestRecognizer_IgnoresUnknownKeysAndValues(t *testing.T) {
	var r Recognizer
	assert.Empty(t, r.Feed(Key(999), 1, time.Now()))
	assert.Empty(t, r.Feed(KeyAction, 9, time.Now()))
}
