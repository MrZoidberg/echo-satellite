// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package oww

import (
	"fmt"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
)

const (
	// melBins is the frozen openWakeWord mel extractor output width.
	melBins = 32
	// melLookback preserves 30 ms of context before each 80 ms step.
	melLookback = 3 * 160
	// embedStride is the number of new mel frames per PCM step.
	embedStride = 8
	// embedFrames and embedDims are the frozen shared embedding model's input and output sizes.
	embedFrames = 76
	embedDims   = 96
	melScale    = 0.1
	melOffset   = 2
)

// SharedModels are the frozen models used by every openWakeWord classifier. A new wake phrase
// changes only its classifier; it never changes this shared mel and embedding backbone.
type SharedModels struct {
	Mel       *tflite.Model
	Embedding *tflite.Model
}

// Features advances the shared mel and embedding pipeline one PCM step at a time.
type Features struct {
	mel       *tflite.Interpreter
	embedding *tflite.Stream
	window    []int16
}

func newFeatures(shared SharedModels) (*Features, error) {
	if shared.Mel == nil || shared.Embedding == nil {
		return nil, ErrMissingSharedModels
	}
	mel, err := tflite.New(shared.Mel)
	if err != nil {
		return nil, fmt.Errorf("prepare mel model: %w", err)
	}
	if input := mel.Input(0); input.Type != tflite.Float32 || len(input.Shape) != 2 || input.Shape[0] != 1 {
		return nil, fmt.Errorf("%w: mel input has type %s and shape %v, want float32 [1 samples]", ErrInvalidModelShape, input.Type, input.Shape)
	}
	mel.ResizeInput(0, []int{1, wake.StepSamples + melLookback})

	shape, err := sharedEmbeddingShape(shared.Embedding)
	if err != nil {
		return nil, err
	}
	if len(shape) != 4 || shape[0] != 1 || shape[1] != embedFrames || shape[2] != melBins || shape[3] != 1 {
		return nil, fmt.Errorf("%w: embedding input %v, want [1 %d %d 1]", ErrInvalidModelShape, shape, embedFrames, melBins)
	}
	embedding, err := tflite.NewStream(shared.Embedding, shape)
	if err != nil {
		return nil, fmt.Errorf("prepare embedding stream: %w", err)
	}
	return &Features{mel: mel, embedding: embedding, window: make([]int16, wake.StepSamples+melLookback)}, nil
}

func sharedEmbeddingShape(model *tflite.Model) ([]int, error) {
	interpreter, err := tflite.New(model)
	if err != nil {
		return nil, fmt.Errorf("prepare embedding model: %w", err)
	}
	input := interpreter.Input(0)
	if input.Type != tflite.Float32 {
		return nil, fmt.Errorf("%w: embedding input has type %s, want float32", ErrInvalidModelShape, input.Type)
	}
	return interpreter.InputShape(0), nil
}

// Step produces the newest embeddings, if the embedding stream has accumulated enough context.
func (f *Features) Step(step []int16) ([]float32, error) {
	copy(f.window[:melLookback], f.window[wake.StepSamples:])
	copy(f.window[melLookback:], step)

	input := f.mel.Input(0).F32
	for i, sample := range f.window {
		input[i] = float32(sample)
	}
	if err := f.mel.Invoke(); err != nil {
		return nil, fmt.Errorf("invoke mel model: %w", err)
	}

	mels := f.mel.Output(0).F32
	if len(mels) != embedStride*melBins {
		return nil, fmt.Errorf("%w: mel output has %d values, want %d", ErrInvalidModelShape, len(mels), embedStride*melBins)
	}
	for i := range mels {
		mels[i] = mels[i]*melScale + melOffset
	}
	embeddings, err := f.embedding.Write(mels)
	if err != nil {
		return nil, fmt.Errorf("advance embedding stream: %w", err)
	}
	if len(embeddings) > 0 && len(embeddings)%embedDims != 0 {
		return nil, fmt.Errorf("%w: embedding output has %d values, not a multiple of %d", ErrInvalidModelShape, len(embeddings), embedDims)
	}
	return embeddings, nil
}

func (f *Features) Reset() {
	clear(f.window)
	f.embedding.Reset()
}
