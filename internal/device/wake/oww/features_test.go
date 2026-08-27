package oww

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatures_ProducesEightMelFramesPerStep(t *testing.T) {
	features := requireFeatures(t)

	_, err := features.Step(make([]int16, wake.StepSamples))
	require.NoError(t, err)
	embeddings, err := features.Step(make([]int16, wake.StepSamples))
	require.NoError(t, err)
	assert.Len(t, features.mel.Output(0).F32, embedStride*melBins)
	assert.Len(t, embeddings, embedStride*embedDims)
}

func TestFeatures_AppliesMelScaleAndOffset(t *testing.T) {
	features := requireFeatures(t)
	step := make([]int16, wake.StepSamples)
	for i := range step {
		step[i] = int16(i % 1024)
	}

	window := append([]int16(nil), features.window...)
	copy(window[:melLookback], window[wake.StepSamples:])
	copy(window[melLookback:], step)
	input := features.mel.Input(0).F32
	for i, sample := range window {
		input[i] = float32(sample)
	}
	require.NoError(t, features.mel.Invoke())
	raw := features.mel.Output(0).F32[0]

	embeddings, err := features.Step(step)
	require.NoError(t, err)
	assert.InDelta(t, float64(raw*melScale+melOffset), float64(features.mel.Output(0).F32[0]), 1e-6)
	for start := 0; start < len(embeddings); start += embedDims {
		assert.InDelta(t, melOffset, embeddings[start], 1e-6)
	}
}

func TestFeatures_UsesMelLookbackContextOnFirstStep(t *testing.T) {
	features := requireFeatures(t)
	step := make([]int16, wake.StepSamples)
	for i := range step {
		step[i] = 1024
	}

	_, err := features.Step(step)
	require.NoError(t, err)
	assert.Equal(t, make([]int16, melLookback), features.window[:melLookback])
	assert.InDelta(t, 0, features.mel.Input(0).F32[0], 0)
	assert.InDelta(t, float64(step[0]), features.mel.Input(0).F32[melLookback], 0)
}

func TestFeatures_RejectsNonFloatMelInput(t *testing.T) {
	t.Parallel()

	shared := SharedModels{Mel: classifierModel(1), Embedding: classifierModel(embedFrames)}
	shared.Mel.Subgraphs[0].Tensors[0].Type = tflite.Int32
	_, err := newFeatures(shared)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidModelShape)
}

func syntheticSharedModels(t *testing.T) SharedModels {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "wake", "synthetic", "oww_embedding.tflite"))
	require.NoError(t, err)
	embedding, err := tflite.Parse(raw)
	require.NoError(t, err)
	return SharedModels{Mel: syntheticMelModel(), Embedding: embedding}
}

func syntheticMelModel() *tflite.Model {
	return &tflite.Model{Subgraphs: []*tflite.Subgraph{{
		Tensors: []*tflite.TensorDesc{
			{Type: tflite.Float32, Shape: []int{1, wake.StepSamples + melLookback}},
			{Type: tflite.Float32, Shape: []int{embedStride * melBins, wake.StepSamples + melLookback}},
			{Type: tflite.Float32, Shape: []int{embedStride * melBins}},
			{Type: tflite.Float32, Shape: []int{1, embedStride * melBins}},
		},
		Inputs:  []int{0},
		Outputs: []int{3},
		Ops: []*tflite.OpDesc{{
			Op: tflite.OpFullyConnected, Inputs: []int{0, 1, 2}, Outputs: []int{3},
		}},
	}}}
}
