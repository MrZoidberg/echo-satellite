package audio

import (
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWAVSink_WritesInterleavedFrames(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "sink-*.wav")
	require.NoError(t, err)
	format := Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE}
	sink, err := NewWAVSink(file, format)
	require.NoError(t, err)
	buf := pcmBytes([]int16{1, 2, 3, 4})
	written, err := sink.WriteInterleaved(buf)
	require.NoError(t, err)
	assert.Equal(t, 2, written)
	require.NoError(t, sink.Close())
	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)
	actualFormat, samples, err := ReadWAV(file)
	require.NoError(t, err)
	assert.Equal(t, format, actualFormat)
	assert.Equal(t, []int16{1, 2, 3, 4}, samples)
}

func TestWAVSink_RejectsPartialFrame(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "sink-*.wav")
	require.NoError(t, err)
	sink, err := NewWAVSink(file, Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE})
	require.NoError(t, err)
	_, err = sink.WriteInterleaved([]byte{1, 2})
	require.ErrorIs(t, err, ErrShortBuffer)
}

func TestNullSink_CountsFrames(t *testing.T) {
	t.Parallel()
	sink, err := NewNullSink(Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE})
	require.NoError(t, err)
	written, err := sink.WriteInterleaved(make([]byte, 12))
	require.NoError(t, err)
	assert.Equal(t, 3, written)
	require.NoError(t, sink.Close())
}

func pcmBytes(samples []int16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample)) //nolint:gosec // G115: conversion preserves the PCM bit pattern.
	}
	return buf
}
