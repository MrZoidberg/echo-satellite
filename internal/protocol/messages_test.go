package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllMessageTypes_CoversDesignFamilies(t *testing.T) {
	// the list from docs/DESIGN.md 8.6, verbatim
	want := []string{
		"hello", "welcome", "config", "config.result", "state", "health", "log",
		"turn.start", "turn.cancel",
		"wake.models", "wake.status",
		"audio.start", "audio.stop", "play.start", "play.stop",
		"update.offer", "update.accept", "update.reject", "update.progress",
		"update.staged", "update.restarting", "update.trial", "update.confirmed",
		"update.rolled_back", "update.failed",
		"button", "mute", "volume",
		"ping", "pong", "error",
	}

	got := make([]string, 0, len(AllMessageTypes()))
	for _, mt := range AllMessageTypes() {
		got = append(got, mt.String())
	}
	assert.ElementsMatch(t, want, got)

	for _, name := range want {
		assert.True(t, MessageType(name).Known(), "%s must be a known message type", name)
	}
}

func TestAllMessageTypes_ReturnsACopy(t *testing.T) {
	first := AllMessageTypes()
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.NotEqual(t, MessageType("mutated"), AllMessageTypes()[0])
}

func TestMessageType_Known(t *testing.T) {
	assert.True(t, TypeTurnStart.Known())
	assert.False(t, MessageType("").Known())
	assert.False(t, MessageType("wake.score").Known(), "the gateway never scores wake words")
}

func TestTurnTrigger_Valid(t *testing.T) {
	assert.True(t, TriggerWake.Valid())
	assert.True(t, TriggerButton.Valid())
	assert.False(t, TurnTrigger("gateway").Valid(), "a turn is never triggered by the gateway")
	assert.False(t, TurnTrigger("").Valid())
}

func TestAllDeviceStates_ReturnsDocumentedStatesAndACopy(t *testing.T) {
	want := []DeviceState{
		StateIdle, StateListening, StateThinking, StateSpeaking, StateMuted,
		StateOffline, StateError, StateUpdating, StateUpdateTrial,
	}
	first := AllDeviceStates()
	assert.Equal(t, want, first)
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.Equal(t, StateIdle, AllDeviceStates()[0])
}

func TestError_ErrorString(t *testing.T) {
	assert.Equal(t, "unauthorized: unknown device", Error{Code: "unauthorized", Message: "unknown device"}.Error())
	assert.Equal(t, "unknown device", Error{Message: "unknown device"}.Error())
}
