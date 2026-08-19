package release

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zeroTime is a helper so table tests can express "no released_at".
func zeroTime() time.Time { return time.Time{} }

func TestVerifyArtifact_Valid(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, VerifyArtifact(bytes.NewReader(readFixture(t, "valid", "echod")), m))
}

func TestVerifyArtifact_TamperedByte(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "tampered", "manifest.json"))
	require.NoError(t, err)

	err = VerifyArtifact(bytes.NewReader(readFixture(t, "tampered", "echod")), m)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrDigestMismatch)
	assert.NotErrorIs(t, err, ErrSizeMismatch, "a flipped byte is a digest failure, not a truncated download")
}

func TestVerifyArtifact_ShortRead(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	truncated := readFixture(t, "valid", "echod")[:10]
	err = VerifyArtifact(bytes.NewReader(truncated), m)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSizeMismatch)
}

func TestVerifyArtifact_ReadError(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	sentinel := errors.New("disk went away")
	err = VerifyArtifact(failingReader{err: sentinel}, m)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestDigest(t *testing.T) {
	digest, size, err := Digest(bytes.NewReader(fixtureArtifact))
	require.NoError(t, err)
	assert.Equal(t, int64(len(fixtureArtifact)), size)
	assert.Equal(t, manifestFor(fixtureArtifact, "0.3.0").SHA256, digest)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
