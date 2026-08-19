package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateFixtures = flag.Bool("update-fixtures", false, "rewrite the fixtures under testdata/updates")

// fixtureArtifact stands in for an echod binary. Its contents are irrelevant to
// the trust primitives, which only ever see bytes.
var fixtureArtifact = []byte("echo-satellite fixture agent binary\n")

type fixtureFiles map[string][]byte

// buildFixtures produces the exact bytes committed under testdata/updates.
func buildFixtures(t *testing.T) map[string]fixtureFiles {
	t.Helper()
	priv, pub := fixtureKeys(t)
	pubEncoded := []byte(base64.StdEncoding.EncodeToString(pub) + "\n")

	valid := manifestFor(fixtureArtifact, "0.3.0")
	validSig, err := Sign(priv, valid)
	require.NoError(t, err)

	tamperedArtifact := bytes.Clone(fixtureArtifact)
	tamperedArtifact[0] ^= 0x01

	otherPriv := ed25519.NewKeyFromSeed([]byte("echo-satellite-fixture-seed-9999"))
	badSig, err := Sign(otherPriv, valid)
	require.NoError(t, err)

	supervisorTooNew := manifestFor(fixtureArtifact, "0.4.0")
	supervisorTooNew.SupervisorMin = 99

	unknownField := map[string]any{}
	require.NoError(t, json.Unmarshal(marshalManifest(t, valid), &unknownField))
	unknownField["rollout_channel"] = "beta"
	unknownFieldJSON, err := json.MarshalIndent(unknownField, "", "  ")
	require.NoError(t, err)

	return map[string]fixtureFiles{
		"valid": {
			"echod":         fixtureArtifact,
			"manifest.json": marshalManifest(t, valid),
			"manifest.sig":  []byte(EncodeSignature(validSig) + "\n"),
			"manifest.pub":  pubEncoded,
		},
		"tampered": {
			// the manifest and signature are genuine; the artifact bytes are not
			"echod":         tamperedArtifact,
			"manifest.json": marshalManifest(t, valid),
			"manifest.sig":  []byte(EncodeSignature(validSig) + "\n"),
			"manifest.pub":  pubEncoded,
		},
		"badsig": {
			// signed by a key that is not the release key
			"echod":         fixtureArtifact,
			"manifest.json": marshalManifest(t, valid),
			"manifest.sig":  []byte(EncodeSignature(badSig) + "\n"),
			"manifest.pub":  pubEncoded,
		},
		"unsigned": {
			"echod":         fixtureArtifact,
			"manifest.json": marshalManifest(t, valid),
			"manifest.pub":  pubEncoded,
		},
		"supervisor-too-new": {
			"echod":         fixtureArtifact,
			"manifest.json": marshalManifest(t, supervisorTooNew),
		},
		"unknown-field": {
			"manifest.json": append(unknownFieldJSON, '\n'),
		},
	}
}

// TestFixtures_MatchGenerator fails when the committed fixtures drift from what
// the generator produces. Regenerate with:
//
//	go test ./internal/release -run TestFixtures_Regenerate -update-fixtures
func TestFixtures_MatchGenerator(t *testing.T) {
	for dir, files := range buildFixtures(t) {
		t.Run(dir, func(t *testing.T) {
			for name, want := range files {
				got, err := os.ReadFile(filepath.Join(fixtureDir(t, dir), name)) //nolint:gosec // G304: test-controlled fixture path
				require.NoError(t, err, "missing fixture %s/%s", dir, name)
				assert.Equal(t, string(want), string(got), "fixture %s/%s is stale", dir, name)
			}
		})
	}
}

func TestFixtures_Regenerate(t *testing.T) {
	if !*updateFixtures {
		t.Skip("run with -update-fixtures to rewrite testdata/updates")
	}
	for dir, files := range buildFixtures(t) {
		target := fixtureDir(t, dir)
		require.NoError(t, os.MkdirAll(target, 0o750))
		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(target, name), content, 0o600))
		}
	}
}
