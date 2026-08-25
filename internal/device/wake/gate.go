package wake

import (
	"math"
	"time"
)

type Thresholds struct {
	Wake float64
	VAD  float64
}

type Candidate struct {
	ModelID           string
	WakeScore         float64
	InstantVADScore   float64
	EffectiveVADScore float64
	VADEnabled        bool
	At                time.Time
}

type Decision uint8

const (
	DecisionBelowWake Decision = iota
	DecisionRejectedLowVAD
	DecisionRefractory
	DecisionAccepted
)

// Gate applies the local wake acceptance rule. Candidate.At makes the decision deterministic
// and testable without consulting a wall clock.
type Gate struct {
	Thresholds   Thresholds
	MinInterval  time.Duration
	lastAccepted time.Time
}

func (g *Gate) Decide(candidate Candidate) Decision {
	if !finite(candidate.WakeScore) || candidate.WakeScore < g.Thresholds.Wake {
		return DecisionBelowWake
	}
	if candidate.VADEnabled && (!finite(candidate.EffectiveVADScore) || candidate.EffectiveVADScore < g.Thresholds.VAD) {
		return DecisionRejectedLowVAD
	}
	if !g.lastAccepted.IsZero() && candidate.At.Sub(g.lastAccepted) < g.MinInterval {
		return DecisionRefractory
	}
	g.lastAccepted = candidate.At
	return DecisionAccepted
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
