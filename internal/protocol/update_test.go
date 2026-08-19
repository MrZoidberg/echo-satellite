package protocol

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllUpdatePhases_MatchesDesignStateMachine(t *testing.T) {
	// the list from docs/DESIGN.md 10.7, verbatim and in order
	want := []string{
		"idle", "available", "queued", "downloading", "verifying", "staged",
		"restarting", "trial", "confirmed", "failed", "rolled_back", "cancelled", //nolint:misspell // wire value fixed by docs/DESIGN.md 10.7
	}

	got := make([]string, 0, len(AllUpdatePhases()))
	for _, p := range AllUpdatePhases() {
		got = append(got, p.String())
	}
	assert.Equal(t, want, got)
}

func TestParseUpdatePhase_AllStates(t *testing.T) {
	for _, p := range AllUpdatePhases() {
		parsed, err := ParseUpdatePhase(p.String())
		require.NoError(t, err, "phase %s", p)
		assert.Equal(t, p, parsed)
		assert.True(t, parsed.Valid())
	}
}

func TestParseUpdatePhase_Unknown(t *testing.T) {
	_, err := ParseUpdatePhase("committed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "committed")

	_, err = ParseUpdatePhase("")
	require.Error(t, err)
}

func TestUpdatePhase_Terminal(t *testing.T) {
	terminal := []UpdatePhase{PhaseConfirmed, PhaseFailed, PhaseRolledBack, PhaseCancelled}
	for _, p := range AllUpdatePhases() {
		assert.Equal(t, slices.Contains(terminal, p), p.Terminal(), "phase %s", p)
	}
}

func TestAllUpdatePhases_ReturnsACopy(t *testing.T) {
	first := AllUpdatePhases()
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.Equal(t, PhaseIdle, AllUpdatePhases()[0])
}
