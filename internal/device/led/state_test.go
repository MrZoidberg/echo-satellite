package led

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestRender_CoversEveryDeviceState(t *testing.T) {
	for _, state := range protocol.AllDeviceStates() {
		_, known := renderKnown(state, 0)
		assert.True(t, known, "%s must have an explicit render case", state)
		assert.NotEqual(t, Frame{}, Render(state, 0), "%s must render a visible initial pattern", state)
	}
	_, known := renderKnown(protocol.DeviceState("unknown"), 0)
	assert.False(t, known)
	assert.Equal(t, Render(protocol.StateError, 0), Render(protocol.DeviceState("unknown"), 0), "unknown states must visibly fail safe")
}

func TestRender_MutedIsVisuallyDistinctFromError(t *testing.T) {
	muted := Render(protocol.StateMuted, 0)
	assert.NotEqual(t, muted, Render(protocol.StateError, 0))
	assert.Equal(t, muted, Render(protocol.StateMuted, 1), "muted must remain steady")
	assert.NotEqual(t, Render(protocol.StateError, 0), Render(protocol.StateError, 1), "error must blink")
}

func TestRender_AnimatedStatesAdvanceAndNegativeTicksAreSafe(t *testing.T) {
	assert.NotEqual(t, Render(protocol.StateThinking, 0), Render(protocol.StateThinking, 1))
	assert.NotEqual(t, Render(protocol.StateUpdating, 0), Render(protocol.StateUpdating, 1))
	assert.NotEqual(t, Render(protocol.StateUpdateTrial, 0), Render(protocol.StateUpdateTrial, 1))
	assert.NotPanics(t, func() { Render(protocol.StateListening, -1) })
}
