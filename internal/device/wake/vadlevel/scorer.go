package vadlevel

import (
	"fmt"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
)

// Scorer adapts the room-relative level detector to the wake VAD contract.
type Scorer struct {
	detector *Detector
}

func NewScorer() *Scorer { return &Scorer{detector: NewDetector()} }

func (s *Scorer) Score(step []int16) (float64, error) {
	if len(step) != wake.StepSamples {
		return 0, fmt.Errorf("score wake VAD step: expected %d samples, got %d", wake.StepSamples, len(step))
	}
	s.detector.Observe(step)
	return s.detector.SpeechScore(), nil
}

func (s *Scorer) Reset() { s.detector.Reset() }

func (*Scorer) Close() error { return nil }

var _ wake.VAD = (*Scorer)(nil)
