package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRing_TailReturnsMostRecentPreRollOnly(t *testing.T) {
	t.Parallel()

	ring, err := NewRing(Format{SampleRate: 10, Channels: 1, Layout: LayoutS16LE}, time.Second)
	require.NoError(t, err)
	ring.Write([]int16{0, 1, 2, 3, 4, 5})
	ring.Write([]int16{6, 7, 8, 9, 10, 11})
	assert.Equal(t, []int16{7, 8, 9, 10, 11}, ring.Tail(500*time.Millisecond))
}

func TestRing_TailIsBoundedByWrittenAudio(t *testing.T) {
	t.Parallel()

	ring, err := NewRing(Format{SampleRate: 10, Channels: 1, Layout: LayoutS16LE}, time.Second)
	require.NoError(t, err)
	ring.Write([]int16{1, 2, 3})
	assert.Equal(t, []int16{1, 2, 3}, ring.Tail(time.Second))
	assert.Empty(t, ring.Tail(0))
	ring.Reset()
	assert.Empty(t, ring.Tail(time.Second))
}

func TestRingWriteLargerThanCapacity(t *testing.T) {
	t.Parallel()

	ring, err := NewRing(Format{SampleRate: 4, Channels: 1, Layout: LayoutS16LE}, time.Second)
	require.NoError(t, err)
	ring.Write([]int16{0, 1, 2, 3, 4, 5})
	assert.Equal(t, []int16{2, 3, 4, 5}, ring.Tail(time.Second))
}

func TestNewRingRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	_, err := NewRing(Format{}, time.Second)
	require.Error(t, err)
	_, err = NewRing(Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}, time.Second)
	require.Error(t, err)
	_, err = NewRing(Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, -time.Second)
	require.Error(t, err)

	ring, err := NewRing(Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, 0)
	require.NoError(t, err)
	ring.Write([]int16{1})
	assert.Empty(t, ring.Tail(time.Second))
}
