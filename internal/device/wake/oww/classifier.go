package oww

import (
	"fmt"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
)

// Classifier applies a per-phrase model to the newest shared embeddings.
type Classifier struct {
	interpreter *tflite.Interpreter
	frames      int
	ring        []float32
}

func newClassifier(model *tflite.Model) (*Classifier, error) {
	interpreter, err := tflite.New(model)
	if err != nil {
		return nil, fmt.Errorf("prepare classifier: %w", err)
	}
	shape := interpreter.InputShape(0)
	if input := interpreter.Input(0); input.Type != tflite.Float32 || len(shape) != 3 || shape[0] != 1 || shape[1] < 1 || shape[2] != embedDims {
		return nil, fmt.Errorf("%w: classifier input has type %s and shape %v, want float32 [1 frames %d]", ErrInvalidModelShape, input.Type, shape, embedDims)
	}
	interpreter.ResizeInput(0, shape)
	output := interpreter.Output(0)
	if output.Type != tflite.Float32 || output.Count() != 1 {
		return nil, fmt.Errorf("%w: classifier output has type %s and shape %v, want one float32", ErrInvalidModelShape, output.Type, output.Shape)
	}
	return &Classifier{interpreter: interpreter, frames: shape[1], ring: make([]float32, shape[1]*embedDims)}, nil
}

func (c *Classifier) Score(embedding []float32) (float64, error) {
	if len(embedding) != embedDims {
		return 0, fmt.Errorf("%w: embedding has %d values, want %d", ErrInvalidModelShape, len(embedding), embedDims)
	}
	copy(c.ring, c.ring[embedDims:])
	copy(c.ring[len(c.ring)-embedDims:], embedding)
	copy(c.interpreter.Input(0).F32, c.ring)
	if err := c.interpreter.Invoke(); err != nil {
		return 0, fmt.Errorf("invoke classifier: %w", err)
	}
	out := c.interpreter.Output(0).F32
	if len(out) == 0 {
		return 0, fmt.Errorf("%w: classifier has no output", ErrInvalidModelShape)
	}
	return float64(out[0]), nil
}

func (c *Classifier) Reset() { clear(c.ring) }
