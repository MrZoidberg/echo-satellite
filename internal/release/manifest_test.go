package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifest_Valid(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	assert.Equal(t, SchemaVersion, m.Schema)
	assert.Equal(t, "0.3.0", m.Version)
	assert.Equal(t, "linux-arm64", m.Architecture)
	assert.Equal(t, int64(len(fixtureArtifact)), m.Size)
	assert.Equal(t, 1, m.ProtocolMin)
	assert.Equal(t, 1, m.SupervisorMin)
	assert.True(t, m.ReleasedAt.Equal(fixtureReleasedAt))
}

func TestParseManifest_RejectsUnknownFields(t *testing.T) {
	_, err := ParseManifest(readFixture(t, "unknown-field", "manifest.json"))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidManifest)
	assert.Contains(t, err.Error(), "rollout_channel")
}

func TestManifest_Validate(t *testing.T) {
	valid := manifestFor(fixtureArtifact, "0.3.0")
	require.NoError(t, valid.Validate())

	tests := []struct {
		name   string
		mutate func(*Manifest)
		target error
	}{
		{"wrong schema", func(m *Manifest) { m.Schema = 2 }, ErrUnsupportedSchema},
		{"no version", func(m *Manifest) { m.Version = "" }, ErrInvalidManifest},
		{"no architecture", func(m *Manifest) { m.Architecture = "" }, ErrInvalidManifest},
		{"zero size", func(m *Manifest) { m.Size = 0 }, ErrInvalidManifest},
		{"digest not hex", func(m *Manifest) { m.SHA256 = "zz" }, ErrInvalidManifest},
		{"digest too short", func(m *Manifest) { m.SHA256 = "abcd" }, ErrInvalidManifest},
		{"inverted protocol range", func(m *Manifest) { m.ProtocolMin, m.ProtocolMax = 3, 2 }, ErrInvalidManifest},
		{"zero protocol min", func(m *Manifest) { m.ProtocolMin = 0 }, ErrInvalidManifest},
		{"negative supervisor min", func(m *Manifest) { m.SupervisorMin = -1 }, ErrInvalidManifest},
		{"no release time", func(m *Manifest) { m.ReleasedAt = zeroTime() }, ErrInvalidManifest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid
			tt.mutate(&m)
			assert.ErrorIs(t, m.Validate(), tt.target)
		})
	}
}

func TestManifest_CanonicalBytes(t *testing.T) {
	m := manifestFor(fixtureArtifact, "0.3.0")

	first, err := m.CanonicalBytes()
	require.NoError(t, err)
	second, err := m.CanonicalBytes()
	require.NoError(t, err)
	assert.Equal(t, first, second, "canonical bytes must be stable")
	assert.Greater(t, len(first), len(signingDomain))
	assert.Equal(t, signingDomain, string(first[:len(signingDomain)]), "signatures are domain-separated")

	other := m
	other.BuildID = "git-def456"
	changed, err := other.CanonicalBytes()
	require.NoError(t, err)
	assert.NotEqual(t, first, changed, "any field change must change the signed bytes")
}
