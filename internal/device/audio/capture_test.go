package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturer_ConvertsDeviceFramesToCanonicalMono16k(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 3, Layout: LayoutS24_3LE}
	source := &sourceStub{format: format, reads: []sourceRead{{data: encodeS24([]int32{
		256, 2_560, 25_600,
		512, 5_120, 30_720,
	})}, {err: io.EOF}}}
	capturer := newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0, 1}, Preprocessor: Bypass{}, StepSamples: 2})

	var frames []Frame
	require.NoError(t, capturer.Run(context.Background(), func(frame Frame) error {
		frames = append(frames, frame)
		return nil
	}))
	require.Len(t, frames, 1)
	assert.Equal(t, int64(0), frames[0].Offset)
	assert.Equal(t, []int16{5, 11}, frames[0].Samples)
}

func TestCapturer_OffsetsAreContiguousAcrossPeriods(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	source := &sourceStub{format: format, reads: []sourceRead{
		{data: encodeS16([]int16{1, 2})},
		{data: encodeS16([]int16{3, 4})},
		{err: io.EOF},
	}}
	capturer := newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 2})

	var frames []Frame
	require.NoError(t, capturer.Run(context.Background(), func(frame Frame) error {
		frames = append(frames, frame)
		return nil
	}))
	require.Len(t, frames, 2)
	assert.Equal(t, int64(0), frames[0].Offset)
	assert.Equal(t, int64(2), frames[1].Offset)
	assert.Equal(t, []int16{1, 2}, frames[0].Samples, "emitted frames must own their samples")
}

func TestCapturer_ContinuesAfterXRun(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	source := &sourceStub{format: format, reads: []sourceRead{
		{err: alsa.ErrXRun},
		{data: encodeS16([]int16{7})},
		{err: io.EOF},
	}}
	capturer := newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1})

	var got []int16
	require.NoError(t, capturer.Run(context.Background(), func(frame Frame) error {
		got = append(got, frame.Samples...)
		return nil
	}))
	assert.Equal(t, []int16{7}, got)
	assert.Equal(t, uint64(1), capturer.XRuns())
}

func TestCapturer_PropagatesOutputError(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	source := &sourceStub{format: format, reads: []sourceRead{{data: encodeS16([]int16{1})}}}
	capturer := newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1})
	want := errors.New("stop")
	err := capturer.Run(context.Background(), func(Frame) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestCapturer_RejectsInvalidSourceFrameCount(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	for name, frames := range map[string]int{"negative": -1, "too_many": 2} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := &frameCountSourceStub{format: format, frames: frames}
			capturer := newTestCapturer(t, source, CaptureConfig{Device: format, Channels: []int{0}, Preprocessor: Bypass{}, StepSamples: 1})
			err := capturer.Run(context.Background(), func(Frame) error { return nil })
			assert.ErrorContains(t, err, "source returned")
		})
	}
}

func newTestCapturer(t *testing.T, source PCMSource, config CaptureConfig) *Capturer {
	t.Helper()
	capturer, err := NewCapturer(source, config, nil)
	require.NoError(t, err)
	return capturer
}

type sourceRead struct {
	data []byte
	err  error
}

type sourceStub struct {
	format Format
	reads  []sourceRead
	index  int
}

func (s *sourceStub) ReadInterleaved(buf []byte) (int, error) {
	read := s.reads[s.index]
	s.index++
	copy(buf, read.data)
	return len(read.data) / s.format.BytesPerFrame(), read.err
}

func (s *sourceStub) Format() Format { return s.format }

func (*sourceStub) Close() error { return nil }

type frameCountSourceStub struct {
	format Format
	frames int
}

func (s *frameCountSourceStub) ReadInterleaved([]byte) (int, error) { return s.frames, nil }

func (s *frameCountSourceStub) Format() Format { return s.format }

func (*frameCountSourceStub) Close() error { return nil }

func encodeS16(samples []int16) []byte {
	encoded := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(encoded[i*2:], uint16(sample)) //nolint:gosec // Test helper preserves the signed PCM bit pattern.
	}
	return encoded
}

func encodeS24(samples []int32) []byte {
	encoded := make([]byte, len(samples)*3)
	var word [4]byte
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(word[:], uint32(sample)) //nolint:gosec // Test helper preserves the signed PCM bit pattern.
		copy(encoded[i*3:], word[:3])
	}
	return encoded
}
