package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustPolicy_ZeroValueIsSecure(t *testing.T) {
	var policy TrustPolicy
	m, err := ParseManifest(readFixture(t, "unsigned", "manifest.json"))
	require.NoError(t, err)

	require.ErrorIs(t, policy.Check(m, nil), ErrUnsignedRelease)
	assert.NotEmpty(t, policy.StatusNotes(), "a build with no release key must say so in status")
}

func TestTrustPolicy_SignedRelease(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)
	pub, err := ParsePublicKey(string(readFixture(t, "valid", "manifest.pub")))
	require.NoError(t, err)
	sig, err := DecodeSignature(string(readFixture(t, "valid", "manifest.sig")))
	require.NoError(t, err)

	policy := TrustPolicy{PublicKey: pub}
	require.NoError(t, policy.Check(m, sig))
	assert.Empty(t, policy.StatusNotes(), "a fully configured secure policy has nothing to warn about")
}

func TestTrustPolicy_RejectsWrongSignature(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "badsig", "manifest.json"))
	require.NoError(t, err)
	pub, err := ParsePublicKey(string(readFixture(t, "badsig", "manifest.pub")))
	require.NoError(t, err)
	sig, err := DecodeSignature(string(readFixture(t, "badsig", "manifest.sig")))
	require.NoError(t, err)

	assert.ErrorIs(t, TrustPolicy{PublicKey: pub}.Check(m, sig), ErrSignatureMismatch)
}

func TestTrustPolicy_UnsignedOnlyWithEscapeHatch(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "unsigned", "manifest.json"))
	require.NoError(t, err)
	pub, err := ParsePublicKey(string(readFixture(t, "unsigned", "manifest.pub")))
	require.NoError(t, err)

	secure := TrustPolicy{PublicKey: pub}
	require.ErrorIs(t, secure.Check(m, nil), ErrUnsignedRelease)

	dev := TrustPolicy{PublicKey: pub, AllowUnsignedDevBuilds: true}
	require.NoError(t, dev.Check(m, nil))
	assert.NotEmpty(t, dev.StatusNotes(), "the escape hatch must always be visible in status")
	assert.Contains(t, dev.StatusNotes()[0], "unsigned")
}

func TestTrustPolicy_RejectsInvalidManifestBeforeSignature(t *testing.T) {
	broken := manifestFor(fixtureArtifact, "0.3.0")
	broken.Schema = 7

	assert.ErrorIs(t, TrustPolicy{AllowUnsignedDevBuilds: true}.Check(broken, nil), ErrUnsupportedSchema)
}

func TestTrustPolicy_StatusNotes(t *testing.T) {
	_, pub := fixtureKeys(t)

	assert.Empty(t, TrustPolicy{PublicKey: pub}.StatusNotes())
	assert.Len(t, TrustPolicy{}.StatusNotes(), 1, "missing release key")
	assert.Len(t, TrustPolicy{AllowUnsignedDevBuilds: true}.StatusNotes(), 1, "escape hatch subsumes the key warning")
	assert.Len(t, TrustPolicy{PublicKey: pub, AllowUnsignedDevBuilds: true}.StatusNotes(), 1)
}
