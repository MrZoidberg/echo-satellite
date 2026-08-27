package vadlevel

import (
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScorer_RejectsWrongStepLength(t *testing.T) {
	t.Parallel()

	_, err := NewScorer().Score(make([]int16, wake.StepSamples-1))
	require.Error(t, err)
}

func TestScorer_ResetClearsFloorState(t *testing.T) {
	t.Parallel()

	scorer := NewScorer()
	frames := append(steps(fixtureSamples(t, "silence_16k_mono.wav")), steps(fixtureSamples(t, "tone_1k_16k_mono.wav"))...)
	first := scoreFrames(t, scorer, frames)
	scorer.Reset()
	second := scoreFrames(t, scorer, frames)
	assert.Equal(t, first, second)
	assert.NoError(t, scorer.Close())
}

func scoreFrames(t *testing.T, scorer *Scorer, frames [][]int16) []float64 {
	t.Helper()

	scores := make([]float64, len(frames))
	for i, frame := range frames {
		var err error
		scores[i], err = scorer.Score(frame)
		require.NoError(t, err)
	}
	return scores
}
