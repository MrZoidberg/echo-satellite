// Package wake defines the portable contracts and pure logic for device-local
// wake detection.
package wake

import "errors"

const (
	SampleRate  = 16_000
	StepSamples = 1_280
)

var ErrVADUnavailable = errors.New("wake VAD unavailable")

// VAD scores one 80 ms step of canonical 16 kHz mono PCM for speech presence.
// A model-backed implementation such as Silero can replace a level detector at
// this seam without changing the wake gate or pipeline.
type VAD interface {
	Score(step []int16) (float64, error)
	Reset()
	Close() error
}

// AlwaysSpeech disables VAD gating without requiring nil handling in callers.
type AlwaysSpeech struct{}

func (AlwaysSpeech) Score([]int16) (float64, error) { return 1, nil }

func (AlwaysSpeech) Reset() {}

func (AlwaysSpeech) Close() error { return nil }
