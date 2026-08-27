package wake

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type frameSourceStub struct{ frames <-chan audio.Frame }

func (s frameSourceStub) Frames() <-chan audio.Frame { return s.frames }

type engineStub struct {
	id     string
	scores []float64
	err    error
	calls  int
}

func (s *engineStub) ID() string { return s.id }
func (s *engineStub) Kind() Kind { return KindOpenWakeWord }
func (s *engineStub) Score([]int16) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	score := s.scores[min(s.calls, len(s.scores)-1)]
	s.calls++
	return score, nil
}
func (s *engineStub) Reset()       {}
func (s *engineStub) Close() error { return nil }

type vadStub struct {
	scores []float64
	err    error
	calls  int
}

func (s *vadStub) Score([]int16) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	score := s.scores[min(s.calls, len(s.scores)-1)]
	s.calls++
	return score, nil
}
func (s *vadStub) Reset()       {}
func (s *vadStub) Close() error { return nil }

func TestPipeline_EmitsEventWithConfiguredPreRollLength(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.9}, []float64{0.9, 0.9}, 100)
	frames <- audio.Frame{Samples: make([]int16, StepSamples*2)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	event := <-events
	assert.Equal(t, "model", event.ModelID)
	assert.Len(t, event.PreRoll, 1600)
	assert.InDelta(t, 0.9, event.WakeScore, 0)
	assert.InDelta(t, 0.9, event.InstantVADScore, 0)
	assert.InDelta(t, 0.9, event.EffectiveVADScore, 0)
}

func TestPipeline_AcceptsDelayedWakeInsideVADLookback(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.1, 0.9}, []float64{0.9, 0.1, 0.1}, 0)
	pipeline.Config.VAD.LookbackMS = 160
	frames <- audio.Frame{Samples: make([]int16, StepSamples*3)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	event := <-events
	assert.InDelta(t, 0.1, event.InstantVADScore, 0)
	assert.InDelta(t, 0.9, event.EffectiveVADScore, 0)
	assert.Equal(t, 160, event.VADLookbackMS)
}

func TestPipeline_RejectsDelayedWakeOutsideVADLookback(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.1, 0.9}, []float64{0.9, 0.1, 0.1}, 0)
	pipeline.Config.VAD.LookbackMS = 80
	frames <- audio.Frame{Samples: make([]int16, StepSamples*3)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Empty(t, events)
	assert.Equal(t, uint64(1), pipeline.Stats.Snapshot().RejectedLowVAD)
}

func TestPipeline_ZeroLookbackPreservesSameStepVAD(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.9}, []float64{0.9, 0.1}, 0)
	frames <- audio.Frame{Samples: make([]int16, StepSamples*2)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Empty(t, events)
}

func TestPipeline_CountsRejectedHighWakeLowVADCandidates(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.99}, []float64{0.01}, 0)
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Empty(t, events)
	assert.Equal(t, uint64(1), pipeline.Stats.Snapshot().RejectedLowVAD)
}

func TestPipeline_SilenceFixtureProducesNoWakeEvent(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.01}, []float64{0.01}, 0)
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Empty(t, events)
}

func TestPipeline_RecordsInferenceTimings(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.01}, []float64{0.01}, 0)
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	snapshot := pipeline.Stats.Snapshot()
	assert.GreaterOrEqual(t, snapshot.WakeInference.MaxMS, 0.0)
	assert.GreaterOrEqual(t, snapshot.VADInference.MaxMS, 0.0)
	assert.Equal(t, uint64(1), snapshot.StepsProcessed)
}

func TestPipeline_StopsOnContextCancel(t *testing.T) {
	pipeline, _, events := newTestPipeline(t, []float64{0.1}, []float64{0.1}, 0)
	frames := make(chan audio.Frame)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, pipeline.Run(ctx, frameSourceStub{frames}, events))
}

func TestPipeline_ShortCircuitsWakeWhenConfigured(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.99}, []float64{0.01}, 0)
	pipeline.Config.AlwaysScoreWake = false
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Zero(t, pipeline.Engines[0].(*engineStub).calls)
	assert.Equal(t, TimingSnapshot{}, pipeline.Stats.Snapshot().WakeInference)
}

func TestPipeline_LowCPUScoringUsesEffectiveVADInsideLookback(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.1, 0.9}, []float64{0.9, 0.1, 0.1}, 0)
	pipeline.Config.AlwaysScoreWake = false
	pipeline.Config.VAD.LookbackMS = 160
	frames <- audio.Frame{Samples: make([]int16, StepSamples*3)}
	close(frames)

	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Equal(t, 3, pipeline.Engines[0].(*engineStub).calls)
	require.Len(t, events, 1)
	assert.InDelta(t, 0.9, (<-events).EffectiveVADScore, 0)
}

func TestPipeline_LowCPUScoringStopsOutsideEffectiveVADLookback(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1, 0.1}, []float64{0.9, 0.1, 0.1}, 0)
	pipeline.Config.AlwaysScoreWake = false
	pipeline.Config.VAD.LookbackMS = 80
	frames <- audio.Frame{Samples: make([]int16, StepSamples*3)}
	close(frames)

	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Equal(t, 2, pipeline.Engines[0].(*engineStub).calls)
	assert.Empty(t, events)
}

func TestPipeline_MultipleEnginesCountOneStepAndOneVADTiming(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.1}, []float64{0.9}, 0)
	pipeline.Engines = append(pipeline.Engines, &engineStub{id: "second", scores: []float64{0.1}})
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.NoError(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events))
	assert.Equal(t, uint64(1), pipeline.Stats.Snapshot().StepsProcessed)
	assert.Equal(t, 1, pipeline.Stats.vadTimings.count)
	assert.Equal(t, 2, pipeline.Stats.wakeTimings.count)
}

func TestPipeline_PropagatesScorerErrors(t *testing.T) {
	t.Run("VAD", func(t *testing.T) {
		pipeline, frames, events := newTestPipeline(t, []float64{0.1}, []float64{0.1}, 0)
		pipeline.VAD = &vadStub{err: errors.New("vad failed")}
		frames <- audio.Frame{Samples: make([]int16, StepSamples)}
		close(frames)
		require.ErrorContains(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events), "score wake VAD")
	})
	t.Run("engine", func(t *testing.T) {
		pipeline, frames, events := newTestPipeline(t, []float64{0.1}, []float64{0.9}, 0)
		pipeline.Engines[0] = &engineStub{id: "model", err: errors.New("model failed")}
		frames <- audio.Frame{Samples: make([]int16, StepSamples)}
		close(frames)
		require.ErrorContains(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events), `score wake model "model"`)
	})
}

func TestPipeline_LaterEngineErrorDoesNotCommitEarlierAcceptance(t *testing.T) {
	pipeline, frames, events := newTestPipeline(t, []float64{0.9}, []float64{0.9}, 0)
	pipeline.Engines = append(pipeline.Engines, &engineStub{id: "broken", err: errors.New("failed")})
	frames <- audio.Frame{Samples: make([]int16, StepSamples)}
	close(frames)
	require.ErrorContains(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events), `score wake model "broken"`)
	assert.Empty(t, events)
	assert.True(t, pipeline.Gate.lastAccepted.IsZero())
	assert.Zero(t, pipeline.Stats.Snapshot().StepsProcessed)
}

func TestPipeline_RejectsInvalidScores(t *testing.T) {
	t.Run("VAD", func(t *testing.T) {
		pipeline, frames, events := newTestPipeline(t, []float64{0.1}, []float64{math.NaN()}, 0)
		frames <- audio.Frame{Samples: make([]int16, StepSamples)}
		close(frames)
		require.ErrorIs(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events), ErrInvalidScore)
	})
	t.Run("engine", func(t *testing.T) {
		pipeline, frames, events := newTestPipeline(t, []float64{math.Inf(1)}, []float64{0.9}, 0)
		frames <- audio.Frame{Samples: make([]int16, StepSamples)}
		close(frames)
		require.ErrorIs(t, pipeline.Run(context.Background(), frameSourceStub{frames}, events), ErrInvalidScore)
	})
}

func TestPipeline_RejectsInvalidWiring(t *testing.T) {
	assert.ErrorIs(t, (&Pipeline{}).Run(context.Background(), nil, nil), ErrInvalidPipeline)
}

func newTestPipeline(t *testing.T, wakeScores, vadScores []float64, preRollMS int) (*Pipeline, chan audio.Frame, chan Event) {
	t.Helper()
	ring, err := audio.NewRing(audio.Format{SampleRate: SampleRate, Channels: 1, Layout: audio.LayoutS16LE}, time.Second)
	require.NoError(t, err)
	frames := make(chan audio.Frame, 2)
	events := make(chan Event, 2)
	now := time.Unix(100, 0)
	return &Pipeline{
		Engines: []Engine{&engineStub{id: "model", scores: wakeScores}},
		VAD:     &vadStub{scores: vadScores},
		Gate:    Gate{Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}},
		Ring:    ring,
		Stats:   NewStats(StatsConfig{}),
		Config:  Config{VAD: VADConfig{Enabled: true}, PreRollMS: preRollMS, AlwaysScoreWake: true},
		Now:     func() time.Time { return now },
	}, frames, events
}
