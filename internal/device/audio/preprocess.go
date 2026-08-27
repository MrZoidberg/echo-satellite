package audio

// Preprocessor is the seam for device-local DSP. Deferred stages include
// beamforming, noise suppression, and acoustic echo cancellation. Milestone 1
// deliberately ships Bypass; the hardware finding is recorded by Task 25.
type Preprocessor interface {
	Process(in []int16) []int16
	Name() string
}

// Bypass leaves canonical PCM unchanged.
type Bypass struct{}

func (Bypass) Process(in []int16) []int16 { return in }

func (Bypass) Name() string { return "bypass" }
