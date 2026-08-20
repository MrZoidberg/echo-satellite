package alsa

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHWParams_EncodesS24_3LE9ChannelMicConfig(t *testing.T) {
	t.Parallel()

	params, err := encodeHWParams(micConfig())
	require.NoError(t, err)
	assertHWParams(t, params, 32, 24, 216, 9, 16_000, 320, 8)
}

func TestHWParams_EncodesS16LEStereoSpeakerConfig(t *testing.T) {
	t.Parallel()

	params, err := encodeHWParams(speakerConfig())
	require.NoError(t, err)
	assertHWParams(t, params, 2, 16, 32, 2, 48_000, 1024, 4)
}

func TestHWParams_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := encodeHWParams(Config{})
	require.Error(t, err)
}

func assertHWParams(t *testing.T, params []byte, format, sampleBits, frameBits, channels, rate, periodFrames, periods uint32) {
	t.Helper()
	require.Len(t, params, 608)
	assert.Equal(t, uint32(1<<3), word(params, maskOffset+paramAccess*maskSize))
	assert.Equal(t, uint32(1<<(format%32)), word(params, maskOffset+paramFormat*maskSize+int(format/32)*4))
	assert.Equal(t, uint32(1), word(params, maskOffset+paramSubformat*maskSize))
	for param, want := range map[int]uint32{
		paramSampleBits: sampleBits, paramFrameBits: frameBits, paramChannels: channels,
		paramRate: rate, paramPeriodFrames: periodFrames, paramPeriods: periods,
	} {
		offset := intervalOffset + (param-firstInterval)*intervalSize
		assert.Equal(t, want, word(params, offset))
		assert.Equal(t, want, word(params, offset+4))
		assert.Equal(t, uint32(1<<2), word(params, offset+8))
	}
	assert.Equal(t, ^uint32(0), word(params, rmaskOffset))
	assert.Equal(t, ^uint32(0), word(params, infoOffset))
}

func word(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset:])
}
