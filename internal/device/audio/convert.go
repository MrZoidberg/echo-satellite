package audio

import (
	"encoding/binary"
	"fmt"
)

func DecodeS24_3LE(dst []int16, src []byte) (int, error) {
	if len(src)%3 != 0 {
		return 0, fmt.Errorf("decode s24_3le: %w: %d bytes", ErrShortBuffer, len(src))
	}
	samples := len(src) / 3
	if len(dst) < samples {
		return 0, fmt.Errorf("decode s24_3le destination: %w: need %d samples, have %d", ErrShortBuffer, samples, len(dst))
	}
	for i := range samples {
		word := int32(src[i*3]) | int32(src[i*3+1])<<8 | int32(src[i*3+2])<<16
		if word&0x800000 != 0 {
			word |= ^int32(0xffffff)
		}
		dst[i] = int16(word >> 8) //nolint:gosec // G115: sign extension guarantees the shifted value is within int16 range.
	}
	return samples, nil
}

func DecodeS16LE(dst []int16, src []byte) (int, error) {
	if len(src)%2 != 0 {
		return 0, fmt.Errorf("decode s16le: %w: %d bytes", ErrShortBuffer, len(src))
	}
	samples := len(src) / 2
	if len(dst) < samples {
		return 0, fmt.Errorf("decode s16le destination: %w: need %d samples, have %d", ErrShortBuffer, samples, len(dst))
	}
	for i := range samples {
		dst[i] = int16(binary.LittleEndian.Uint16(src[i*2:])) //nolint:gosec // G115: conversion preserves the signed PCM bit pattern.
	}
	return samples, nil
}

func SelectChannels(dst, src []int16, channels int, selected []int) (int, error) {
	if channels <= 0 {
		return 0, fmt.Errorf("select channels: channel count must be positive: %d", channels)
	}
	if len(src)%channels != 0 {
		return 0, fmt.Errorf("select channels source: %w: %d samples for %d channels", ErrShortBuffer, len(src), channels)
	}
	for _, channel := range selected {
		if channel < 0 || channel >= channels {
			return 0, fmt.Errorf("select channels: channel index %d outside [0,%d)", channel, channels)
		}
	}
	needed := len(src) / channels * len(selected)
	if len(dst) < needed {
		return 0, fmt.Errorf("select channels destination: %w: need %d samples, have %d", ErrShortBuffer, needed, len(dst))
	}
	out := 0
	for frame := 0; frame < len(src); frame += channels {
		for _, channel := range selected {
			dst[out] = src[frame+channel]
			out++
		}
	}
	return out, nil
}

func MonoDownmix(dst, src []int16, channels int) (int, error) {
	if channels <= 0 {
		return 0, fmt.Errorf("mono downmix: channel count must be positive: %d", channels)
	}
	if len(src)%channels != 0 {
		return 0, fmt.Errorf("mono downmix source: %w: %d samples for %d channels", ErrShortBuffer, len(src), channels)
	}
	frames := len(src) / channels
	if len(dst) < frames {
		return 0, fmt.Errorf("mono downmix destination: %w: need %d samples, have %d", ErrShortBuffer, frames, len(dst))
	}
	for frame := range frames {
		var sum int64
		for channel := range channels {
			sum += int64(src[frame*channels+channel])
		}
		dst[frame] = int16(sum / int64(channels)) //nolint:gosec // G115: an average of int16 samples remains within int16 range.
	}
	return frames, nil
}
