package oww

import (
	"errors"
	"fmt"
	"os"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
)

// Engine implements the openWakeWord wake.Engine contract.
type Engine struct {
	id       string
	features *Features
	classify *Classifier
}

// New loads one wake-phrase classifier on top of the frozen shared backbone.
func New(shared SharedModels, model wake.Model) (*Engine, error) {
	if model.ID == "" {
		return nil, errors.New("openwakeword model ID is required")
	}
	if model.Path == "" {
		return nil, errors.New("openwakeword model path is required")
	}
	if model.Kind != wake.KindOpenWakeWord {
		return nil, fmt.Errorf("%w: %s", wake.ErrUnknownModelKind, model.Kind)
	}

	features, err := newFeatures(shared)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(model.Path)
	if err != nil {
		return nil, fmt.Errorf("read classifier %q: %w", model.Path, err)
	}
	classifierModel, err := tflite.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse classifier %q: %w", model.Path, err)
	}
	kind, detectErr := DetectKind(classifierModel)
	if detectErr != nil {
		return nil, detectErr
	}
	if kind != model.Kind {
		return nil, fmt.Errorf("%w: classifier is %s, model declares %s", wake.ErrUnknownModelKind, kind, model.Kind)
	}
	classifier, err := newClassifier(classifierModel)
	if err != nil {
		return nil, err
	}
	return &Engine{id: model.ID, features: features, classify: classifier}, nil
}

func (e *Engine) ID() string { return e.id }

func (*Engine) Kind() wake.Kind { return wake.KindOpenWakeWord }

func (e *Engine) Score(step []int16) (float64, error) {
	if len(step) != wake.StepSamples {
		return 0, fmt.Errorf("%w: got %d, want %d", wake.ErrStepLength, len(step), wake.StepSamples)
	}
	embeddings, err := e.features.Step(step)
	if err != nil {
		return 0, err
	}
	var score float64
	for start := 0; start < len(embeddings); start += embedDims {
		score, err = e.classify.Score(embeddings[start : start+embedDims])
		if err != nil {
			return 0, err
		}
	}
	return score, nil
}

func (e *Engine) Reset() {
	e.features.Reset()
	e.classify.Reset()
}

func (*Engine) Close() error { return nil }

// DetectKind determines the engine solely from a classifier's input tensor shape.
func DetectKind(model *tflite.Model) (wake.Kind, error) {
	interpreter, err := tflite.New(model)
	if err != nil {
		return wake.KindUnknown, fmt.Errorf("prepare classifier for kind detection: %w", err)
	}
	shape := interpreter.InputShape(0)
	if len(shape) == 3 && shape[0] == 1 && shape[1] > 0 && shape[2] == embedDims {
		return wake.KindOpenWakeWord, nil
	}
	return wake.KindUnknown, fmt.Errorf("%w: classifier input shape %v", wake.ErrUnknownModelKind, shape)
}
