package wake

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStats_SnapshotReportsEverySection16Field(t *testing.T) {
	stats := NewStats(StatsConfig{
		ActiveModelID: "okay_nabu", ModelKind: KindOpenWakeWord, Languages: []string{"en"},
		Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}, VADEnabled: true, VADLookbackMS: 160,
	})
	stats.Observe(Observation{
		InstantVADScore: 0.01, EffectiveVADScore: 0.6, VADElapsed: time.Millisecond,
		Candidates: []CandidateObservation{{WakeScore: 0.99, Decision: DecisionRejectedLowVAD, WakeElapsed: 3 * time.Millisecond, Measured: true}},
	})
	stats.Observe(Observation{
		InstantVADScore: 0.8, EffectiveVADScore: 0.9, VADElapsed: 2 * time.Millisecond,
		Candidates: []CandidateObservation{{WakeScore: 0.9, Decision: DecisionAccepted, WakeElapsed: 5 * time.Millisecond, Measured: true}},
	})
	stats.SetFramesDropped(7)

	snapshot := stats.Snapshot()
	assert.Equal(t, "okay_nabu", snapshot.ActiveModelID)
	assert.Equal(t, KindOpenWakeWord, snapshot.ModelKind)
	assert.Equal(t, []string{"en"}, snapshot.Languages)
	assert.InDelta(t, 0.8, snapshot.WakeThreshold, 0)
	assert.True(t, snapshot.VADEnabled)
	assert.InDelta(t, 0.5, snapshot.VADThreshold, 0)
	assert.Equal(t, 160, snapshot.VADLookbackMS)
	assert.InDelta(t, 0.9, snapshot.LastWakeScore, 0)
	assert.InDelta(t, 0.8, snapshot.LastInstantVADScore, 0)
	assert.InDelta(t, 0.9, snapshot.LastEffectiveVADScore, 0)
	assert.InDelta(t, 0.99, snapshot.MaxWakeScore, 0)
	assert.Equal(t, uint64(1), snapshot.WakeCount)
	assert.Equal(t, uint64(1), snapshot.RejectedLowVAD)
	assert.Equal(t, uint64(2), snapshot.StepsProcessed)
	assert.Equal(t, uint64(7), snapshot.FramesDropped)
	assert.InDelta(t, 3.0, snapshot.WakeInference.P50MS, 0)
	assert.InDelta(t, 5.0, snapshot.WakeInference.MaxMS, 0)
	assert.InDelta(t, 1.0, snapshot.VADInference.P50MS, 0)
}

func TestStats_TimingPercentilesAreOrdered(t *testing.T) {
	stats := NewStats(StatsConfig{})
	for i := 1; i <= 100; i++ {
		stats.Observe(Observation{
			VADElapsed: time.Duration(i*2) * time.Millisecond,
			Candidates: []CandidateObservation{{WakeElapsed: time.Duration(i) * time.Millisecond, Measured: true}},
		})
	}
	snapshot := stats.Snapshot()
	assert.LessOrEqual(t, snapshot.WakeInference.P50MS, snapshot.WakeInference.P95MS)
	assert.LessOrEqual(t, snapshot.WakeInference.P95MS, snapshot.WakeInference.MaxMS)
	assert.InDelta(t, 50.0, snapshot.WakeInference.P50MS, 0)
	assert.InDelta(t, 95.0, snapshot.WakeInference.P95MS, 0)
	assert.InDelta(t, 100.0, snapshot.WakeInference.MaxMS, 0)
	assert.LessOrEqual(t, snapshot.VADInference.P50MS, snapshot.VADInference.P95MS)
	assert.LessOrEqual(t, snapshot.VADInference.P95MS, snapshot.VADInference.MaxMS)
}

func TestStats_TimingRetentionIsBounded(t *testing.T) {
	stats := NewStats(StatsConfig{})
	for i := range timingWindowSize + 100 {
		stats.Observe(Observation{
			VADElapsed: time.Duration(i),
			Candidates: []CandidateObservation{{WakeElapsed: time.Duration(i), Measured: true}},
		})
	}
	assert.Equal(t, timingWindowSize, stats.wakeTimings.count)
	assert.Equal(t, timingWindowSize, stats.vadTimings.count)
}

func TestStats_SnapshotIsIndependent(t *testing.T) {
	stats := NewStats(StatsConfig{Languages: []string{"en"}})
	first := stats.Snapshot()
	first.Languages[0] = "changed"
	assert.Equal(t, []string{"en"}, stats.Snapshot().Languages)
	assert.Equal(t, TimingSnapshot{}, NewStats(StatsConfig{}).Snapshot().WakeInference)
}
