package wake

import (
	"math"
	"slices"
	"sync"
	"time"
)

type StatsConfig struct {
	ActiveModelID string
	ModelKind     Kind
	Languages     []string
	Thresholds    Thresholds
	VADEnabled    bool
	VADLookbackMS int
}

type Observation struct {
	InstantVADScore   float64
	EffectiveVADScore float64
	VADElapsed        time.Duration
	Candidates        []CandidateObservation
}

type CandidateObservation struct {
	WakeScore   float64
	Decision    Decision
	WakeElapsed time.Duration
	Measured    bool
}

const timingWindowSize = 1024

type durationWindow struct {
	values [timingWindowSize]time.Duration
	next   int
	count  int
}

type Stats struct {
	mu          sync.Mutex
	snapshot    Snapshot
	wakeTimings durationWindow
	vadTimings  durationWindow
}

func NewStats(config StatsConfig) *Stats {
	return &Stats{snapshot: Snapshot{
		ActiveModelID: config.ActiveModelID,
		ModelKind:     config.ModelKind,
		Languages:     append([]string(nil), config.Languages...),
		WakeThreshold: config.Thresholds.Wake,
		VADEnabled:    config.VADEnabled,
		VADThreshold:  config.Thresholds.VAD,
		VADLookbackMS: config.VADLookbackMS,
	}}
}

func (s *Stats) Observe(observation Observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.StepsProcessed++
	s.snapshot.LastInstantVADScore = observation.InstantVADScore
	s.snapshot.LastEffectiveVADScore = observation.EffectiveVADScore
	s.vadTimings.add(observation.VADElapsed)
	for _, candidate := range observation.Candidates {
		if candidate.Measured {
			s.snapshot.LastWakeScore = candidate.WakeScore
			s.snapshot.MaxWakeScore = max(s.snapshot.MaxWakeScore, candidate.WakeScore)
			s.wakeTimings.add(candidate.WakeElapsed)
		}
		if candidate.Decision == DecisionAccepted {
			s.snapshot.WakeCount++
		}
		if candidate.Decision == DecisionRejectedLowVAD {
			s.snapshot.RejectedLowVAD++
		}
	}
}

func (s *Stats) SetFramesDropped(count uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.FramesDropped = count
}

func (s *Stats) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.snapshot
	result.Languages = append([]string(nil), s.snapshot.Languages...)
	result.WakeInference = timingSnapshot(s.wakeTimings.snapshot())
	result.VADInference = timingSnapshot(s.vadTimings.snapshot())
	return result
}

func (w *durationWindow) add(value time.Duration) {
	w.values[w.next] = value
	w.next = (w.next + 1) % len(w.values)
	w.count = min(w.count+1, len(w.values))
}

func (w *durationWindow) snapshot() []time.Duration {
	result := make([]time.Duration, w.count)
	copy(result, w.values[:w.count])
	return result
}

func timingSnapshot(values []time.Duration) TimingSnapshot {
	if len(values) == 0 {
		return TimingSnapshot{}
	}
	sorted := append([]time.Duration(nil), values...)
	slices.Sort(sorted)
	return TimingSnapshot{
		P50MS: durationMilliseconds(nearestRankDuration(sorted, 0.50)),
		P95MS: durationMilliseconds(nearestRankDuration(sorted, 0.95)),
		MaxMS: durationMilliseconds(sorted[len(sorted)-1]),
	}
}

func nearestRankDuration(sorted []time.Duration, percentile float64) time.Duration {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	return sorted[max(index, 0)]
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
