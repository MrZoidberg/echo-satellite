package buttons

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKey_String(t *testing.T) {
	assert.Equal(t, "mute", KeyMute.String())
	assert.Equal(t, "volume-down", KeyVolumeDown.String())
	assert.Equal(t, "volume-up", KeyVolumeUp.String())
	assert.Equal(t, "action", KeyAction.String())
	assert.Equal(t, "key-999", Key(999).String())
}
