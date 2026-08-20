package audio

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSink struct {
	format Format
	writes [][]byte
	err    error
}

func (s *recordingSink) WriteInterleaved(buf []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.writes = append(s.writes, append([]byte(nil), buf...))
	return len(buf) / s.format.BytesPerFrame(), nil
}

func (s *recordingSink) Format() Format { return s.format }
func (*recordingSink) Close() error     { return nil }

func TestPlayer_MonoIsDuplicatedToBothChannels(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{format: Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}}
	player, err := NewPlayer(sink, NewHoldResampler(16_000, 16_000), 8)
	require.NoError(t, err)
	require.NoError(t, player.Play(context.Background(), []int16{-3, 7}))
	require.Len(t, sink.writes, 1)
	decoded := make([]int16, 4)
	_, err = DecodeS16LE(decoded, sink.writes[0])
	require.NoError(t, err)
	assert.Equal(t, []int16{-3, -3, 7, 7}, decoded)
}

func TestPlayer_WritesPeriodSizedChunks(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{format: Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE}}
	player, err := NewPlayer(sink, NewHoldResampler(16_000, 48_000), 4)
	require.NoError(t, err)
	require.NoError(t, player.Play(context.Background(), []int16{1, 2, 3}))
	require.Len(t, sink.writes, 3)
	assert.Len(t, sink.writes[0], 16)
	assert.Len(t, sink.writes[1], 16)
	assert.Len(t, sink.writes[2], 4)
}

func TestPlayer_PropagatesContextAndSinkErrors(t *testing.T) {
	t.Parallel()
	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	player, err := NewPlayer(&recordingSink{format: format}, NewHoldResampler(16_000, 16_000), 1)
	require.NoError(t, err)
	require.ErrorIs(t, player.Play(ctx, []int16{1}), context.Canceled)

	want := errors.New("sink failed")
	player, err = NewPlayer(&recordingSink{format: format, err: want}, NewHoldResampler(16_000, 16_000), 1)
	require.NoError(t, err)
	require.ErrorIs(t, player.Play(context.Background(), []int16{1}), want)
}

func TestPlayer_RejectsResamplerForWrongSinkRate(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{format: Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE}}
	_, err := NewPlayer(sink, NewHoldResampler(16_000, 16_000), 1)
	require.Error(t, err)
}

func TestPlayer_SincToneThroughWAVSinkIsClean48kStereo(t *testing.T) {
	t.Parallel()
	format := Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE}
	wav := &memoryWriteSeeker{}
	sink, err := NewWAVSink(wav, format)
	require.NoError(t, err)
	player, err := NewPlayer(sink, NewSincResampler(CanonicalSampleRate, format.SampleRate), 257)
	require.NoError(t, err)
	source := generatedTone(CanonicalSampleRate, CanonicalSampleRate)
	require.NoError(t, player.Play(context.Background(), source))
	require.NoError(t, sink.Close())

	actualFormat, interleaved, err := ReadWAV(bytes.NewReader(wav.data))
	require.NoError(t, err)
	assert.Equal(t, format, actualFormat)
	require.Len(t, interleaved, len(source)*3*2)
	left := make([]int16, len(interleaved)/2)
	for i := range left {
		left[i] = interleaved[i*2]
		assert.Equal(t, interleaved[i*2], interleaved[i*2+1])
	}
	reference := generatedTone(format.SampleRate, len(left))
	snr := signalToNoise(reference[96:len(reference)-96], left[96:len(left)-96])
	t.Logf("Player -> WAVSink sinc tone SNR: %.2f dB", snr)
	assert.Greater(t, snr, 40.0)
}

type invalidRatioResampler struct{ ratio float64 }

func (r invalidRatioResampler) Resample([]int16, []int16) int { return 0 }
func (r invalidRatioResampler) Ratio() float64                { return r.ratio }

func TestPlayer_RejectsNonFiniteResamplerRatio(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{format: Format{SampleRate: 48_000, Channels: 2, Layout: LayoutS16LE}}
	for _, ratio := range []float64{math.NaN(), math.Inf(1)} {
		_, err := NewPlayer(sink, invalidRatioResampler{ratio: ratio}, 1)
		require.Error(t, err)
	}
}
