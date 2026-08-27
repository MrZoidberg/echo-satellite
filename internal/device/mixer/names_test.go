package mixer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNames_SpeakerAmpControlMatchesHardwareName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Ext_Speaker_Amp_Switch", ControlSpeakerAmp)
	assert.Equal(t, "On", ValueOn)
	assert.Equal(t, "Off", ValueOff)
}
