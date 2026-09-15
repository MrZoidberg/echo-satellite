package audio

// Preprocessor is the seam for device-local DSP. Conditioning candidates first
// reduce the seven physical microphones to canonical mono, then implement this
// interface before fanout. Profile selection remains hardware-qualified work.
type Preprocessor interface {
	Process(in []int16) []int16
	Name() string
}

// MultichannelPreprocessor reduces the seven physical microphone channels
// before the canonical mono Preprocessor stage. Capturer recognizes it only
// when its configured channel map is exactly mic0 through mic6.
type MultichannelPreprocessor interface {
	Preprocessor
	ProcessChannels(mics [][]int16) []int16
}

// Bypass leaves canonical PCM unchanged.
type Bypass struct{}

func (Bypass) Process(in []int16) []int16 { return in }

func (Bypass) Name() string { return "bypass" }
