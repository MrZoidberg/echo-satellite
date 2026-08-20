package wake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlwaysSpeech(t *testing.T) {
	t.Parallel()

	var vad VAD = AlwaysSpeech{}
	score, err := vad.Score(nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, score, 0)
	assert.NotPanics(t, vad.Reset)
	assert.NoError(t, vad.Close())
}
