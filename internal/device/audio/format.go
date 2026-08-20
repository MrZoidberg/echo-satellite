// Package audio provides the portable audio primitives shared by device
// capture, local wake processing, diagnostics, and the simulator.
//
// Echo Dot microphone captures contain nine interleaved channels. Channels
// 0-6 are physical microphones; channels 7-8 are playback loopback references
// for AEC and must never enter the wake path.
package audio

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedLayout = errors.New("unsupported sample layout")
	ErrShortBuffer       = errors.New("short audio buffer")
)

type SampleLayout uint8

const (
	LayoutS16LE SampleLayout = iota + 1
	LayoutS24_3LE
)

type Format struct {
	SampleRate int
	Channels   int
	Layout     SampleLayout
}

func (f Format) BytesPerFrame() int {
	bytesPerSample := 0
	switch f.Layout {
	case LayoutS16LE:
		bytesPerSample = 2
	case LayoutS24_3LE:
		bytesPerSample = 3
	}
	return f.Channels * bytesPerSample
}

func (f Format) Validate() error {
	if f.SampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive: %d", f.SampleRate)
	}
	if f.Channels <= 0 {
		return fmt.Errorf("channel count must be positive: %d", f.Channels)
	}
	if f.Layout != LayoutS16LE && f.Layout != LayoutS24_3LE {
		return fmt.Errorf("%w: %d", ErrUnsupportedLayout, f.Layout)
	}
	return nil
}
