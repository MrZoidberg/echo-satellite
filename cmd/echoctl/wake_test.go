package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWakeInstallAndList_ReportMetadataAndSharedAssets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	modelDir := filepath.Join(dir, "models")
	source := filepath.Join("..", "..", "testdata", "wake", "synthetic", "oww_classifier.tflite")
	raw, err := os.ReadFile(source) //nolint:gosec // source is a repository-owned fixed fixture name.
	require.NoError(t, err)
	digestBytes := sha256.Sum256(raw)
	digest := hex.EncodeToString(digestBytes[:])
	metadata := filepath.Join(dir, "model.json")
	sidecar := fmt.Sprintf("{\"schema\":1,\"id\":\"okay_nabu\",\"kind\":\"openwakeword\",\"phrase\":\"okay nabu\",\"languages\":[\"en\"],\"sample_rate\":16000,\"sha256\":%q}\n", digest)
	require.NoError(t, os.WriteFile(metadata, []byte(sidecar), 0o600)) //nolint:gosec // metadata is beneath t.TempDir.

	var output bytes.Buffer
	err = wakeInstall(&output, wakeInstallCommand{
		Args: struct {
			ID string `positional-arg-name:"id" required:"true"`
		}{ID: "okay_nabu"},
		From: source, Metadata: metadata, SHA256: digest, ModelDir: modelDir,
	})
	require.NoError(t, err)
	assert.Contains(t, output.String(), "installed: okay_nabu")

	require.NoError(t, os.WriteFile(filepath.Join(modelDir, "melspectrogram.tflite"), []byte("fixture"), 0o600))
	output.Reset()
	require.NoError(t, wakeList(&output, wakeListCommand{ModelDir: modelDir}))
	assert.Contains(t, output.String(), "mel=true embedding=false")
	assert.Contains(t, output.String(), "model: okay_nabu kind=openwakeword")
	assert.Contains(t, output.String(), "phrase=\"okay nabu\"")
	assert.Contains(t, output.String(), "languages=en")
	assert.Contains(t, output.String(), digest[:12])
}

func TestWakeList_EmptyStore(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	require.NoError(t, wakeList(&output, wakeListCommand{ModelDir: filepath.Join(t.TempDir(), "absent")}))
	assert.Contains(t, output.String(), "models: none")
}
