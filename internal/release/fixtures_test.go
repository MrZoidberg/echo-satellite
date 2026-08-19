package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fixtureSeed derives the Ed25519 pair used by the committed fixtures under
// testdata/updates. It is a test value, not a release identity: the real
// release private key lives only in the build and release process.
const fixtureSeed = "echo-satellite-fixture-seed-0001"

// fixtureReleasedAt keeps the fixtures byte-stable across regeneration.
var fixtureReleasedAt = time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)

func fixtureKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed([]byte(fixtureSeed))
	pub, ok := priv.Public().(ed25519.PublicKey)
	require.True(t, ok)
	return priv, pub
}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "updates", name)
}

func readFixture(t *testing.T, name, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(t, name), file)) //nolint:gosec // G304: test-controlled fixture path
	require.NoError(t, err)
	return data
}

func manifestFor(artifact []byte, version string) Manifest {
	sum := sha256.Sum256(artifact)
	return Manifest{
		Schema:        SchemaVersion,
		Version:       version,
		BuildID:       "git-abc123",
		Architecture:  "linux-arm64",
		Size:          int64(len(artifact)),
		SHA256:        hex.EncodeToString(sum[:]),
		ProtocolMin:   1,
		ProtocolMax:   1,
		SupervisorMin: 1,
		ReleasedAt:    fixtureReleasedAt,
	}
}

func marshalManifest(t *testing.T, m Manifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	return append(data, '\n')
}
