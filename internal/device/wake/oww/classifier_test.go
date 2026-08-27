package oww

import (
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifier_ReadsFrameCountFromModelInputShape(t *testing.T) {
	t.Parallel()

	classifier, err := newClassifier(classifierModel(7))
	require.NoError(t, err)
	assert.Equal(t, 7, classifier.frames)
}

func TestClassifier_RejectsNonScalarOutput(t *testing.T) {
	t.Parallel()

	model := classifierModel(7)
	model.Subgraphs[0].Tensors = append(model.Subgraphs[0].Tensors, &tflite.TensorDesc{Type: tflite.Float32, Shape: []int{1, 2}})
	model.Subgraphs[0].Outputs = []int{2}
	_, err := newClassifier(model)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidModelShape)
}

func TestClassifier_RejectsNonFloatInput(t *testing.T) {
	t.Parallel()

	model := classifierModel(7)
	model.Subgraphs[0].Tensors[0].Type = tflite.Int32
	_, err := newClassifier(model)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidModelShape)
}

func classifierModel(frames int) *tflite.Model {
	return &tflite.Model{Subgraphs: []*tflite.Subgraph{{
		Tensors: []*tflite.TensorDesc{
			{Type: tflite.Float32, Shape: []int{1, frames, embedDims}},
			{Type: tflite.Float32, Shape: []int{1}},
		},
		Inputs:  []int{0},
		Outputs: []int{1},
	}}}
}
