package audio

import "time"

const CanonicalSampleRate = 16_000

type Frame struct {
	Offset int64
	// Samples is immutable after delivery. Fanout subscribers share this
	// backing array so capture can remain allocation-bounded per frame.
	Samples []int16
}

func (f Frame) Duration() time.Duration {
	return time.Duration(len(f.Samples)) * time.Second / CanonicalSampleRate
}
