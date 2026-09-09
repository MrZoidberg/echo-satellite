package protocol

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllUpdatePhases_MatchesDesignStateMachine(t *testing.T) {
	// the list from docs/DESIGN.md 10.7, verbatim and in order
	want := []string{
		"idle", "available", "queued", "downloading", "verifying", "staged",
		"restarting", "confirmed", "failed", "cancelled", //nolint:misspell // wire value fixed by docs/DESIGN.md 10.7
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
	terminal := []UpdatePhase{PhaseConfirmed, PhaseFailed, PhaseCancelled}
	for _, p := range AllUpdatePhases() {
		assert.Equal(t, slices.Contains(terminal, p), p.Terminal(), "phase %s", p)
	}
}

func TestUpdatePayloads_ValidateAndRejectUnknownFields(t *testing.T) {
	offer := UpdateOffer{DeploymentID: "deployment-1", Version: "0.3.0", BuildID: "git-abc123", ArtifactURL: "https://gateway.local/echod", ManifestURL: "https://gateway.local/manifest", SignatureURL: "https://gateway.local/manifest.sig", Size: 1, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	require.NoError(t, offer.Validate())
	require.NoError(t, (UpdateDecision{DeploymentID: "deployment-1", Decision: UpdateDecisionAccepted}).Validate())
	require.NoError(t, (UpdateProgress{DeploymentID: "deployment-1", Phase: PhaseDownloading, Percent: 42}).Validate())
	require.NoError(t, (UpdateConfirmation{DeploymentID: "deployment-1", Version: "0.3.0", BuildID: "git-abc123"}).Validate())
	require.NoError(t, (UpdateCancellation{DeploymentID: "deployment-1"}).Validate())
	require.NoError(t, (UpdateFailure{DeploymentID: "deployment-1", Code: UpdateFailureDigestMismatch}).Validate())

	var decoded UpdateOffer
	err := json.Unmarshal([]byte(`{"deployment_id":"deployment-1","version":"0.3.0","build_id":"git-abc123","artifact_url":"https://gateway.local/echod","manifest_url":"https://gateway.local/manifest","signature_url":"https://gateway.local/manifest.sig","size":1,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","unexpected":true}`), &decoded)
	require.Error(t, err)
	assert.Error(t, (UpdateOffer{DeploymentID: "deployment-1", Version: "v", BuildID: "b", ArtifactURL: "http://gateway.local/a", ManifestURL: "https://gateway.local/m", SignatureURL: "https://gateway.local/s", Size: 1, SHA256: offer.SHA256}).Validate())
}

func TestAllUpdatePhases_ReturnsACopy(t *testing.T) {
	first := AllUpdatePhases()
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.Equal(t, PhaseIdle, AllUpdatePhases()[0])
}
