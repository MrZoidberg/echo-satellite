package wake

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGate_RejectsHighWakeWithLowVAD(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}}
	decision := gate.Decide(Candidate{WakeScore: 0.99, InstantVADScore: 0.01, EffectiveVADScore: 0.01, VADEnabled: true, At: time.Unix(1, 0)})
	assert.Equal(t, DecisionRejectedLowVAD, decision)
}

func TestGate_AcceptsWhenWakeAndVADBothExceedThresholds(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}}
	decision := gate.Decide(Candidate{WakeScore: 0.8, InstantVADScore: 0.1, EffectiveVADScore: 0.5, VADEnabled: true, At: time.Unix(1, 0)})
	assert.Equal(t, DecisionAccepted, decision)
}

func TestGate_IgnoresVADWhenDisabled(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}}
	decision := gate.Decide(Candidate{WakeScore: 0.9, At: time.Unix(1, 0)})
	assert.Equal(t, DecisionAccepted, decision)
}

func TestGate_RefractoryWindowSuppressesDuplicateWake(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8}, MinInterval: 2 * time.Second}
	first := time.Unix(10, 0)
	assert.Equal(t, DecisionAccepted, gate.Decide(Candidate{WakeScore: 0.9, At: first}))
	assert.Equal(t, DecisionRefractory, gate.Decide(Candidate{WakeScore: 0.9, At: first.Add(time.Second)}))
	assert.Equal(t, DecisionAccepted, gate.Decide(Candidate{WakeScore: 0.9, At: first.Add(2 * time.Second)}))
}

func TestGate_BelowWake(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8}}
	assert.Equal(t, DecisionBelowWake, gate.Decide(Candidate{WakeScore: 0.79}))
}

func TestGate_RejectsNonFiniteScores(t *testing.T) {
	gate := Gate{Thresholds: Thresholds{Wake: 0.8, VAD: 0.5}}
	assert.Equal(t, DecisionBelowWake, gate.Decide(Candidate{WakeScore: math.NaN()}))
	assert.Equal(t, DecisionBelowWake, gate.Decide(Candidate{WakeScore: math.Inf(1)}))
	assert.Equal(t, DecisionRejectedLowVAD, gate.Decide(Candidate{
		WakeScore: 0.9, EffectiveVADScore: math.NaN(), VADEnabled: true,
	}))
}
