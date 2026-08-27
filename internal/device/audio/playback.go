package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const defaultPlaybackPeriodFrames = 1024

type Player struct {
	sink         PCMSink
	resampler    Resampler
	periodFrames int
}

func NewPlayer(sink PCMSink, resampler Resampler, periodFrames int) (*Player, error) {
	if sink == nil || resampler == nil {
		return nil, errors.New("create player: sink and resampler are required")
	}
	format := sink.Format()
	if err := format.Validate(); err != nil {
		return nil, fmt.Errorf("create player: sink format: %w", err)
	}
	if format.Layout != LayoutS16LE {
		return nil, fmt.Errorf("create player: %w: %d", ErrUnsupportedLayout, format.Layout)
	}
	expectedRatio := float64(format.SampleRate) / CanonicalSampleRate
	if ratio := resampler.Ratio(); math.IsNaN(ratio) || math.IsInf(ratio, 0) || math.Abs(ratio-expectedRatio) > 1e-12 {
		return nil, fmt.Errorf("create player: resampler ratio %g does not map %d Hz to %d Hz", ratio, CanonicalSampleRate, format.SampleRate)
	}
	if periodFrames == 0 {
		periodFrames = defaultPlaybackPeriodFrames
	}
	if periodFrames < 0 {
		return nil, fmt.Errorf("create player: period frames must be positive: %d", periodFrames)
	}
	return &Player{sink: sink, resampler: resampler, periodFrames: periodFrames}, nil
}

// Play converts canonical 16 kHz mono PCM immediately before writing it to the
// hardware-facing sink. The source slice is not retained.
func (p *Player) Play(ctx context.Context, source []int16) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("play PCM: %w", err)
	}
	format := p.sink.Format()
	outputFrames := int(math.Ceil(float64(len(source)) * p.resampler.Ratio()))
	mono := make([]int16, outputFrames)
	outputFrames = p.resampler.Resample(mono, source)
	// Resample is an intentionally small, atomic interface without context. Check
	// again immediately afterward so cancellation never proceeds to sink I/O.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("play PCM: %w", err)
	}
	bytesPerFrame := format.BytesPerFrame()
	period := make([]byte, p.periodFrames*bytesPerFrame)
	for offset := 0; offset < outputFrames; {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("play PCM: %w", err)
		}
		frames := min(p.periodFrames, outputFrames-offset)
		buf := period[:frames*bytesPerFrame]
		encodeDuplicatedS16LE(buf, mono[offset:offset+frames], format.Channels)
		written, err := p.sink.WriteInterleaved(buf)
		if err != nil {
			return fmt.Errorf("play PCM at frame %d: %w", offset, err)
		}
		if written <= 0 || written > frames {
			return fmt.Errorf("play PCM at frame %d: %w", offset, io.ErrShortWrite)
		}
		offset += written
	}
	return nil
}

func encodeDuplicatedS16LE(dst []byte, mono []int16, channels int) {
	for frame, sample := range mono {
		for channel := range channels {
			index := (frame*channels + channel) * 2
			binary.LittleEndian.PutUint16(dst[index:], uint16(sample)) //nolint:gosec // G115: conversion preserves the PCM bit pattern.
		}
	}
}
