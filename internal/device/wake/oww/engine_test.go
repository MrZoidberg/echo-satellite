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

func TestEngine_RejectsWrongStepLengthWithErrStepLength(t *testing.T) {
	t.Parallel()

	engine := &Engine{}
	_, err := engine.Score(make([]int16, wake.StepSamples-1))
	require.Error(t, err)
	assert.ErrorIs(t, err, wake.ErrStepLength)
}

func TestEngine_ResetClearsStreamingState(t *testing.T) {
	engine := syntheticEngine(t)
	step := make([]int16, wake.StepSamples)

	first, err := engine.features.Step(step)
	require.NoError(t, err)
	assert.Len(t, first, 7*embedDims)
	second, err := engine.features.Step(step)
	require.NoError(t, err)
	assert.Len(t, second, embedStride*embedDims)
	engine.classify.ring[0] = 1
	engine.Reset()
	assert.Equal(t, make([]float32, len(engine.classify.ring)), engine.classify.ring)
	afterReset, err := engine.features.Step(step)
	require.NoError(t, err)
	assert.Equal(t, first, afterReset)
}

func TestOWW_DetectsKindFromClassifierInputShape(t *testing.T) {
	t.Parallel()

	kind, err := DetectKind(classifierModel(16))
	require.NoError(t, err)
	assert.Equal(t, wake.KindOpenWakeWord, kind)
}

func TestOWW_RejectsNonOWWInputShapeWithErrUnknownModelKind(t *testing.T) {
	t.Parallel()

	model := classifierModel(16)
	model.Subgraphs[0].Tensors[0].Shape = []int{1, 16, 95}
	_, err := DetectKind(model)
	require.Error(t, err)
	assert.ErrorIs(t, err, wake.ErrUnknownModelKind)
}

func TestEngine_SilenceScoresNearZero(t *testing.T) {
	engine := syntheticEngine(t)

	for _, score := range scoreSteps(t, engine, make([]int16, wake.StepSamples)) {
		assert.Less(t, score, 0.1)
	}
}

func TestEngine_RealModelsSilenceScoresNearZero(t *testing.T) {
	engine := requireEngine(t)

	for _, score := range scoreSteps(t, engine, make([]int16, wake.StepSamples)) {
		assert.Less(t, score, 0.1)
	}
}

func requireFeatures(t *testing.T) *Features {
	t.Helper()
	features, err := newFeatures(syntheticSharedModels(t))
	require.NoError(t, err)
	return features
}

func syntheticEngine(t *testing.T) *Engine {
	t.Helper()
	features, err := newFeatures(syntheticSharedModels(t))
	require.NoError(t, err)
	classifier, err := newClassifier(classifierModel(1))
	require.NoError(t, err)
	return &Engine{id: "synthetic", features: features, classify: classifier}
}

func requireEngine(t *testing.T) *Engine {
	t.Helper()
	dir := requireModelDir(t)
	engine, err := New(requireSharedModels(t), wake.Model{
		ID: "okay_nabu", Path: filepath.Join(dir, "okay_nabu.tflite"), Kind: wake.KindOpenWakeWord,
	})
	require.NoError(t, err)
	return engine
}

func requireSharedModels(t *testing.T) SharedModels {
	t.Helper()
	dir := requireModelDir(t)
	return SharedModels{
		Mel:       loadModel(t, filepath.Join(dir, "melspectrogram.tflite")),
		Embedding: loadModel(t, filepath.Join(dir, "embedding_model.tflite")),
	}
}

func requireModelDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("ECHO_WAKE_MODEL_DIR")
	if dir == "" {
		t.Skip("ECHO_WAKE_MODEL_DIR is not set")
	}
	return dir
}

func loadModel(t *testing.T, path string) *tflite.Model {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test receives only an explicit operator model directory.
	require.NoError(t, err)
	model, err := tflite.Parse(raw)
	require.NoError(t, err)
	return model
}

func scoreSteps(t *testing.T, engine *Engine, step []int16) []float64 {
	t.Helper()
	scores := make([]float64, 12)
	for i := range scores {
		var err error
		scores[i], err = engine.Score(step)
		require.NoError(t, err)
	}
	return scores
}
