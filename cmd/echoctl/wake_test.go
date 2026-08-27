package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenWakeSource_LiveUsesDotMicrophone(t *testing.T) {
	wantErr := errors.New("stop after config capture")
	var got alsa.Config
	original := openALSACapture
	openALSACapture = func(config alsa.Config) (*alsa.PCM, error) {
		got = config
		return nil, wantErr
	}
	t.Cleanup(func() { openALSACapture = original })

	_, _, err := openWakeSource("")
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, alsa.MicCard, got.Card)
	assert.Equal(t, alsa.MicDevice, got.Device)
	assert.True(t, got.Capture)
}

func TestDiagnosticContext_StartsCaptureTimeoutWhenCreated(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	t.Cleanup(cancelParent)
	started := time.Now()
	ctx, cancel := diagnosticContext(parent, "", 0.1)
	t.Cleanup(cancel)
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.GreaterOrEqual(t, deadline.Sub(started), 90*time.Millisecond)

	cancelParent()
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

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

func TestWakeInstall_HeyPrimeAsset(t *testing.T) {
	dir := os.Getenv("ECHO_WAKE_MODEL_DIR")
	if dir == "" {
		t.Skip("ECHO_WAKE_MODEL_DIR is not set")
	}
	source := filepath.Join(dir, "Hey_Prime_20260824_084713.tflite")
	raw, err := os.ReadFile(source) //nolint:gosec // explicit operator test asset directory.
	require.NoError(t, err)
	digestBytes := sha256.Sum256(raw)
	digest := hex.EncodeToString(digestBytes[:])
	require.Equal(t, "ad1fedb27dac6b9f3401da64f696351e1516a703038eed2c2414ae1740af34f0", digest)

	metadata := filepath.Join(t.TempDir(), "hey_prime.json")
	sidecar := fmt.Sprintf("{\"schema\":1,\"id\":\"hey_prime\",\"kind\":\"openwakeword\",\"phrase\":\"Hey Prime\",\"languages\":[\"en\"],\"sample_rate\":16000,\"sha256\":%q,\"source\":\"https://openwakeword.com\"}\n", digest)
	require.NoError(t, os.WriteFile(metadata, []byte(sidecar), 0o600)) //nolint:gosec // metadata is beneath t.TempDir.

	var output bytes.Buffer
	err = wakeInstall(&output, wakeInstallCommand{
		Args: struct {
			ID string `positional-arg-name:"id" required:"true"`
		}{ID: "hey_prime"},
		From: source, Metadata: metadata, SHA256: digest, ModelDir: filepath.Join(t.TempDir(), "models"),
	})
	require.NoError(t, err)
	assert.Contains(t, output.String(), "installed: hey_prime")
}

func TestWakeList_EmptyStore(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	require.NoError(t, wakeList(&output, wakeListCommand{ModelDir: filepath.Join(t.TempDir(), "absent")}))
	assert.Contains(t, output.String(), "models: none")
}

func TestWakeVADTest_SilenceReportsStepsAndSummary(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := wakeVADTest(&output, wakeVADTestCommand{wakeInputOptions: wakeInputOptions{
		FromFile:  filepath.Join("..", "..", "testdata", "audio", "silence_16k_mono.wav"),
		Threshold: 0.5, PrintSteps: true,
	}})
	require.NoError(t, err)
	assert.Contains(t, output.String(), "step=1 time=0.000s vad=")
	assert.Contains(t, output.String(), "steps: ")
	assert.Contains(t, output.String(), "fraction_above_threshold: 0.0000")
}

func TestWakeDiagnostics_RejectInvalidThresholds(t *testing.T) {
	t.Parallel()
	require.Error(t, wakeVADTest(io.Discard, wakeVADTestCommand{wakeInputOptions: wakeInputOptions{Threshold: 2}}))
	require.Error(t, wakeTest(io.Discard, wakeTestCommand{wakeInputOptions: wakeInputOptions{Threshold: -1}}))
	require.Error(t, wakeVADTest(io.Discard, wakeVADTestCommand{wakeInputOptions: wakeInputOptions{Threshold: 0.5, Seconds: -1}}))
	require.Error(t, wakeTest(io.Discard, wakeTestCommand{wakeInputOptions: wakeInputOptions{Threshold: 0.5, Seconds: -1}}))
	require.Error(t, wakeTest(io.Discard, wakeTestCommand{wakeInputOptions: wakeInputOptions{Threshold: 0.5}, VADLookbackMS: -1}))
	require.Error(t, wakeTest(io.Discard, wakeTestCommand{wakeInputOptions: wakeInputOptions{FromFile: "fixture.wav", Threshold: 0.5}, GreenOnWake: true}))
}
