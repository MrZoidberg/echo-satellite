package release

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignAndVerifyManifest(t *testing.T) {
	priv, pub := fixtureKeys(t)
	m := manifestFor(fixtureArtifact, "0.3.0")

	sig, err := Sign(priv, m)
	require.NoError(t, err)
	require.NoError(t, VerifyManifest(pub, m, sig))
}

func TestVerifyManifest_CommittedFixture(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)
	pub, err := ParsePublicKey(string(readFixture(t, "valid", "manifest.pub")))
	require.NoError(t, err)
	sig, err := DecodeSignature(string(readFixture(t, "valid", "manifest.sig")))
	require.NoError(t, err)

	require.NoError(t, VerifyManifest(pub, m, sig))
}

func TestVerifyManifest_WrongKey(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "badsig", "manifest.json"))
	require.NoError(t, err)
	pub, err := ParsePublicKey(string(readFixture(t, "badsig", "manifest.pub")))
	require.NoError(t, err)
	sig, err := DecodeSignature(string(readFixture(t, "badsig", "manifest.sig")))
	require.NoError(t, err)

	assert.ErrorIs(t, VerifyManifest(pub, m, sig), ErrSignatureMismatch)
}

func TestVerifyManifest_ModifiedManifest(t *testing.T) {
	priv, pub := fixtureKeys(t)
	m := manifestFor(fixtureArtifact, "0.3.0")
	sig, err := Sign(priv, m)
	require.NoError(t, err)

	m.SupervisorMin = 0 // an attacker relaxing an installation-safety constraint
	assert.ErrorIs(t, VerifyManifest(pub, m, sig), ErrSignatureMismatch)
}

func TestVerifyManifest_BadInputs(t *testing.T) {
	_, pub := fixtureKeys(t)
	m := manifestFor(fixtureArtifact, "0.3.0")

	t.Run("no key", func(t *testing.T) {
		assert.ErrorIs(t, VerifyManifest(nil, m, make([]byte, ed25519.SignatureSize)), ErrNoPublicKey)
	})

	t.Run("short signature", func(t *testing.T) {
		assert.ErrorIs(t, VerifyManifest(pub, m, []byte{1, 2, 3}), ErrInvalidSignature)
	})
}

func TestSign_BadPrivateKey(t *testing.T) {
	_, err := Sign(ed25519.PrivateKey{1, 2, 3}, manifestFor(fixtureArtifact, "0.3.0"))
	assert.ErrorIs(t, err, ErrInvalidPublicKey)
}

func TestParsePublicKey(t *testing.T) {
	_, pub := fixtureKeys(t)
	encoded := EncodeSignature(pub) // base64 of 32 bytes

	got, err := ParsePublicKey("  " + encoded + "\n")
	require.NoError(t, err)
	assert.Equal(t, pub, got)

	t.Run("not base64", func(t *testing.T) {
		_, err := ParsePublicKey("not!base64")
		assert.ErrorIs(t, err, ErrInvalidPublicKey)
	})

	t.Run("wrong length", func(t *testing.T) {
		_, err := ParsePublicKey(EncodeSignature([]byte{1, 2, 3}))
		assert.ErrorIs(t, err, ErrInvalidPublicKey)
	})
}

func TestDecodeSignature(t *testing.T) {
	priv, _ := fixtureKeys(t)
	sig, err := Sign(priv, manifestFor(fixtureArtifact, "0.3.0"))
	require.NoError(t, err)

	got, err := DecodeSignature(EncodeSignature(sig) + "\n")
	require.NoError(t, err)
	assert.Equal(t, sig, got)

	t.Run("not base64", func(t *testing.T) {
		_, err := DecodeSignature("!!!")
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})

	t.Run("wrong length", func(t *testing.T) {
		_, err := DecodeSignature(EncodeSignature([]byte{1, 2, 3}))
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})
}

func TestEmbeddedPublicKey_AbsentInDevBuilds(t *testing.T) {
	_, err := EmbeddedPublicKey()
	assert.ErrorIs(t, err, ErrNoPublicKey,
		"a build with no release key must refuse signed releases rather than trust them")
}
