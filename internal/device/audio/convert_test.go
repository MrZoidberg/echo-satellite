package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeS24_3LE_SignExtendsNegativeSamples(t *testing.T) {
	t.Parallel()

	dst := make([]int16, 4)
	n, err := DecodeS24_3LE(dst, []byte{0x00, 0xff, 0x7f, 0x00, 0x00, 0x80, 0x00, 0x34, 0x12, 0xff, 0xff, 0xff})
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []int16{32_767, -32_768, 0x1234, -1}, dst)
}

func TestDecodeS24_3LE_RejectsTruncatedFinalSample(t *testing.T) {
	t.Parallel()

	_, err := DecodeS24_3LE(make([]int16, 1), []byte{0, 1})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrShortBuffer)
}

func TestDecodeS16LE(t *testing.T) {
	t.Parallel()

	dst := make([]int16, 2)
	n, err := DecodeS16LE(dst, []byte{0x34, 0x12, 0xff, 0xff})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []int16{0x1234, -1}, dst)

	_, err = DecodeS16LE(dst, []byte{0})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrShortBuffer)
}

func TestSelectChannels_ExcludesLoopbackChannels(t *testing.T) {
	t.Parallel()

	src := []int16{0, 1, 2, 3, 4, 5, 6, 70, 80, 10, 11, 12, 13, 14, 15, 16, 170, 180}
	dst := make([]int16, 14)
	n, err := SelectChannels(dst, src, 9, []int{0, 1, 2, 3, 4, 5, 6})
	require.NoError(t, err)
	assert.Equal(t, 14, n)
	assert.Equal(t, []int16{0, 1, 2, 3, 4, 5, 6, 10, 11, 12, 13, 14, 15, 16}, dst)
}

func TestSelectChannelsRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	_, err := SelectChannels(nil, []int16{1}, 0, nil)
	require.Error(t, err)
	_, err = SelectChannels(nil, []int16{1}, 2, nil)
	require.Error(t, err)
	_, err = SelectChannels(make([]int16, 1), []int16{1, 2}, 2, []int{2})
	require.Error(t, err)
	_, err = SelectChannels(nil, []int16{1, 2}, 2, []int{0})
	require.Error(t, err)
}

func TestMonoDownmix(t *testing.T) {
	t.Parallel()

	dst := make([]int16, 2)
	n, err := MonoDownmix(dst, []int16{10, 20, 30, -30}, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []int16{15, 0}, dst)
}

func TestMonoDownmixRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	_, err := MonoDownmix(nil, nil, 0)
	require.Error(t, err)
	_, err = MonoDownmix(nil, []int16{1}, 2)
	require.Error(t, err)
	_, err = MonoDownmix(nil, []int16{1, 2}, 2)
	require.Error(t, err)
}
