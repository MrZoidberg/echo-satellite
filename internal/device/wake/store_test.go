package wake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_InstallAndListStableMetadata(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	model, err := store.Install(request)
	require.NoError(t, err)
	assert.Equal(t, "okay_nabu", model.ID)
	assert.FileExists(t, model.Path)
	assert.NoFileExists(t, model.Path+".part")

	models, err := store.List()
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, model, models[0])
	got, err := store.Get("okay_nabu")
	require.NoError(t, err)
	assert.Equal(t, model, got)
	_, err = store.Get("absent")
	assert.ErrorIs(t, err, ErrModelNotFound)
}

func TestStore_InstallRejectsURLSourceBecauseModelsAreNeverFetched(t *testing.T) {
	t.Parallel()
	_, err := (Store{Root: t.TempDir()}).Install(InstallRequest{ID: "model", SourcePath: "https://example.com/model.tflite"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL sources are forbidden")
}

func TestStore_InstallRejectsSidecarKindMismatch(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "microwakeword")
	_, err := store.Install(request)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSidecarMismatch)
}

func TestStore_InstallRejectsModelWithUnsupportedOpcodes(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_unsupported.tflite", "openwakeword")
	_, err := store.Install(request)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedModelOps)
	assert.Contains(t, err.Error(), "OP_999")
}

func TestStore_InstallLeavesNoPartFileOnFailure(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	request.ExpectedSHA256 = testDigest
	_, err := store.Install(request)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrDigestMismatch)
	entries, readErr := os.ReadDir(store.Root)
	if readErr != nil {
		assert.ErrorIs(t, readErr, os.ErrNotExist)
		return
	}
	for _, entry := range entries {
		assert.NotEqual(t, ".part", filepath.Ext(entry.Name()))
		assert.NotEqual(t, ".tflite", filepath.Ext(entry.Name()))
	}
}

func TestStore_InstallRejectsDigestMismatchBeforeWriting(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	rewriteSidecar(t, request.SidecarPath, testDigest, "openwakeword")
	request.ExpectedSHA256 = testDigest
	_, err := store.Install(request)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrDigestMismatch)
	_, statErr := os.Stat(store.Root)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestStore_OverwriteCommitsByIndexAndPreservesPreviousArtifact(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	first, err := store.Install(request)
	require.NoError(t, err)
	firstBytes, err := os.ReadFile(first.Path)
	require.NoError(t, err)

	secondSource := filepath.Join(t.TempDir(), "replacement.tflite")
	secondBytes := append(append([]byte(nil), firstBytes...), 0)
	require.NoError(t, os.WriteFile(secondSource, secondBytes, 0o600)) //nolint:gosec // path is beneath t.TempDir.
	digestBytes := sha256.Sum256(secondBytes)
	digest := hex.EncodeToString(digestBytes[:])
	secondSidecar := filepath.Join(t.TempDir(), "replacement.json")
	rewriteSidecar(t, secondSidecar, digest, "openwakeword")
	second, err := store.Install(InstallRequest{
		ID: "okay_nabu", SourcePath: secondSource, SidecarPath: secondSidecar,
		ExpectedSHA256: digest, Overwrite: true,
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.Path, second.Path)
	assert.FileExists(t, first.Path, "the rollback artifact must remain after index commit")
	gotFirstBytes, err := os.ReadFile(first.Path)
	require.NoError(t, err)
	assert.Equal(t, firstBytes, gotFirstBytes)
	current, err := store.Get("okay_nabu")
	require.NoError(t, err)
	assert.Equal(t, second.Path, current.Path)
}

func TestStore_CorruptIndexPreventsOverwriteBeforePublishing(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	first, err := store.Install(request)
	require.NoError(t, err)
	firstBytes, err := os.ReadFile(first.Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(store.Root, "index.json"), []byte("not json"), 0o600))

	request.Overwrite = true
	_, err = store.Install(request)
	require.Error(t, err)
	got, readErr := os.ReadFile(first.Path)
	require.NoError(t, readErr)
	assert.Equal(t, firstBytes, got)
}

func TestStore_ConcurrentInstallWithoutOverwriteHasSingleWinner(t *testing.T) {
	t.Parallel()
	store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, err := store.Install(request)
			errorsSeen <- err
		}()
	}
	ready.Wait()
	close(start)
	var successes int
	for range 2 {
		if err := <-errorsSeen; err == nil {
			successes++
		} else {
			assert.Contains(t, err.Error(), "already exists")
		}
	}
	assert.Equal(t, 1, successes)
}

func TestStore_InstallRejectsMismatchedPreexistingImmutableFiles(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"artifact", "metadata"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
			require.NoError(t, os.MkdirAll(store.Root, 0o750))
			name := "okay_nabu." + request.ExpectedSHA256 + ".tflite"
			if target == "metadata" {
				name = "okay_nabu." + request.ExpectedSHA256 + ".json"
			}
			planted := filepath.Join(store.Root, name)
			require.NoError(t, os.WriteFile(planted, []byte("different"), 0o600))
			_, err := store.Install(request)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "different contents")
			contents, readErr := os.ReadFile(planted) //nolint:gosec // planted is a test-selected filename beneath t.TempDir.
			require.NoError(t, readErr)
			assert.Equal(t, []byte("different"), contents)
			assert.NoFileExists(t, filepath.Join(store.Root, "index.json"))
		})
	}
}

func TestStore_ListRejectsDotArtifactAndMetadataPaths(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"artifact", "metadata"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			store, request := installFixtureRequest(t, "oww_classifier.tflite", "openwakeword")
			_, err := store.Install(request)
			require.NoError(t, err)
			raw, err := os.ReadFile(filepath.Join(store.Root, "index.json"))
			require.NoError(t, err)
			var index storeIndex
			require.NoError(t, json.Unmarshal(raw, &index))
			if field == "artifact" {
				index.Models[0].Artifact = ".."
			} else {
				index.Models[0].Metadata = "."
			}
			raw, err = json.Marshal(index)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(store.Root, "index.json"), raw, 0o600))
			_, err = store.List()
			require.ErrorIs(t, err, ErrInvalidModel)
		})
	}
}

func installFixtureRequest(t *testing.T, fixture, kind string) (Store, InstallRequest) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join("..", "..", "..", "testdata", "wake", "synthetic", fixture)
	raw, err := os.ReadFile(source) //nolint:gosec // source is a repository-owned fixed fixture name.
	require.NoError(t, err)
	digestBytes := sha256.Sum256(raw)
	digest := hex.EncodeToString(digestBytes[:])
	sidecar := filepath.Join(dir, "metadata.json")
	rewriteSidecar(t, sidecar, digest, kind)
	return Store{Root: filepath.Join(dir, "store")}, InstallRequest{
		ID: "okay_nabu", SourcePath: source, SidecarPath: sidecar, ExpectedSHA256: digest,
	}
}

func rewriteSidecar(t *testing.T, path, digest, kind string) {
	t.Helper()
	raw := fmt.Sprintf("{\"schema\":1,\"id\":\"okay_nabu\",\"kind\":%q,\"phrase\":\"okay nabu\",\"languages\":[\"en\"],\"sample_rate\":16000,\"sha256\":%q,\"source\":\"fixture\",\"license\":\"MIT\"}\n", kind, digest)
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600)) //nolint:gosec // path is created beneath t.TempDir by the test.
}
