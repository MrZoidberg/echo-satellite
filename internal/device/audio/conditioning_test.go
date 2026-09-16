package audio

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditioningCandidates_UseOnlySevenPhysicalMicrophones(t *testing.T) {
	t.Parallel()
	mics := make([][]int16, PhysicalMicrophones)
	for mic := range mics {
		mics[mic] = []int16{int16(100 * (mic + 1)), int16(-100 * (mic + 1))}
	}

	channel0, err := NewConditioning(CandidateChannel0)
	require.NoError(t, err)
	assert.Equal(t, []int16{100, -100}, channel0.ProcessChannels(mics))

	mix, err := NewConditioning(CandidateUnsteeredMix)
	require.NoError(t, err)
	assert.Equal(t, []int16{400, -400}, mix.ProcessChannels(mics))

	for _, candidate := range []ConditioningCandidate{CandidateChannel0, CandidateUnsteeredMix, CandidateDelaySum} {
		processor, createErr := NewConditioning(candidate)
		require.NoError(t, createErr)
		assert.Nil(t, processor.ProcessChannels(mics[:PhysicalMicrophones-1]), candidate)
	}
}

func TestLeveler_BoundsGainAndPreservesHeadroomOnSuddenLoudAudio(t *testing.T) {
	t.Parallel()
	leveler := NewLeveler()
	quiet := make([]int16, 320)
	for i := range quiet {
		quiet[i] = 100
	}
	for range 100 {
		leveler.Process(quiet)
	}
	before := leveler.Metrics().AppliedGainDB
	assert.LessOrEqual(t, before, MaxConditioningGainDB+0.0001)
	assert.Greater(t, before, 0.0)

	loud := make([]int16, 320)
	for i := range loud {
		loud[i] = math.MaxInt16
	}
	out := leveler.Process(loud)
	peak, _ := peakAndRMS(out)
	assert.LessOrEqual(t, amplitudeDBFS(peak), -conditioningHeadroom+0.001)
	metrics := leveler.Metrics()
	assert.LessOrEqual(t, metrics.AppliedGainDB, 0.0)
	assert.Zero(t, metrics.ClippingCount)
	assert.LessOrEqual(t, metrics.ClippingFraction, 0.001)
}

func TestLeveler_UsesControlledGainTransitions(t *testing.T) {
	t.Parallel()
	leveler := NewLeveler()
	quiet := make([]int16, 320)
	for i := range quiet {
		quiet[i] = 100
	}
	leveler.Process(quiet)
	first := leveler.Metrics().AppliedGainDB
	leveler.Process(quiet)
	second := leveler.Metrics().AppliedGainDB
	assert.Greater(t, first, 0.0)
	assert.Greater(t, second, first)
	assert.Less(t, second, MaxConditioningGainDB)
}

func TestConditioningMetrics_ReportsRequiredValues(t *testing.T) {
	t.Parallel()
	conditioner, err := NewConditioning(CandidateChannel0)
	require.NoError(t, err)
	mics := make([][]int16, PhysicalMicrophones)
	for mic := range mics {
		mics[mic] = []int16{1_000, -1_000, 1_000, -1_000}
	}
	conditioner.Process(conditioner.ProcessChannels(mics))
	metrics := conditioner.Metrics([]int16{100, -100, 100, -100})
	assert.Equal(t, string(CandidateChannel0), metrics.Profile)
	assert.Greater(t, metrics.PeakDBFS, -120.0)
	assert.Greater(t, metrics.RMSDBFS, -120.0)
	assert.Greater(t, metrics.NoiseLevelDBFS, -120.0)
	assert.Greater(t, metrics.SpeechNoiseDB, 0.0)
	assert.Positive(t, metrics.ProcessingDuration)
}

func TestConditioningMetrics_SeparatesBeforeLeveling(t *testing.T) {
	t.Parallel()
	conditioner, err := NewConditioning(CandidateChannel0)
	require.NoError(t, err)
	mics := make([][]int16, PhysicalMicrophones)
	for mic := range mics {
		mics[mic] = make([]int16, ConditioningCadenceSamples)
		for sample := range mics[mic] {
			mics[mic][sample] = 1_000
		}
	}
	conditioner.ProcessBlock(mics)
	metrics := conditioner.Metrics([]int16{100, -100})
	assert.InDelta(t, 20, metrics.SpeechNoiseDB, 0.1)
	assert.Less(t, metrics.MaxBlockDuration, 80*time.Millisecond)
}
