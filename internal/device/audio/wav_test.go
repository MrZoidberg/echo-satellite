package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryWriteSeeker struct {
	data   []byte
	offset int64
}

func (m *memoryWriteSeeker) Write(p []byte) (int, error) {
	end := int(m.offset) + len(p)
	if end > len(m.data) {
		m.data = append(m.data, make([]byte, end-len(m.data))...)
	}
	copy(m.data[m.offset:], p)
	m.offset = int64(end)
	return len(p), nil
}

func (m *memoryWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.offset = offset
	case io.SeekCurrent:
		m.offset += offset
	case io.SeekEnd:
		m.offset = int64(len(m.data)) + offset
	}
	if m.offset < 0 {
		return 0, errors.New("negative seek")
	}
	return m.offset, nil
}

func TestWAVRoundTrip(t *testing.T) {
	t.Parallel()

	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}
	w := &memoryWriteSeeker{}
	require.NoError(t, WriteWAV(w, format, []int16{-32_768, -1, 0, 1, 32_767}))
	gotFormat, gotSamples, err := ReadWAV(bytes.NewReader(w.data))
	require.NoError(t, err)
	assert.Equal(t, format, gotFormat)
	assert.Equal(t, []int16{-32_768, -1, 0, 1, 32_767}, gotSamples)
}

func TestWAVWriterStreamsAndPatchesSizes(t *testing.T) {
	t.Parallel()

	w := &memoryWriteSeeker{}
	wav, err := NewWAVWriter(w, Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE})
	require.NoError(t, err)
	n, err := wav.Write([]int16{1, 2})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	_, err = wav.Write([]int16{3})
	require.NoError(t, err)
	require.NoError(t, wav.Close())
	require.NoError(t, wav.Close())
	_, samples, err := ReadWAV(bytes.NewReader(w.data))
	require.NoError(t, err)
	assert.Equal(t, []int16{1, 2, 3}, samples)
	_, err = wav.Write([]int16{4})
	require.Error(t, err)
}

func TestReadWAV_RejectsNonRIFFWithErrNotRIFF(t *testing.T) {
	t.Parallel()

	_, _, err := ReadWAV(bytes.NewReader([]byte("not a wave!!")))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotRIFF)
}

func TestReadWAVRejectsUnsupportedAndTruncatedFiles(t *testing.T) {
	t.Parallel()

	_, _, err := ReadWAV(bytes.NewReader(nil))
	require.Error(t, err)

	w := &memoryWriteSeeker{}
	require.NoError(t, WriteWAV(w, Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, []int16{1}))
	w.data[20] = 3
	_, _, err = ReadWAV(bytes.NewReader(w.data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedWAV)
}

func TestReadWAVRejectsChunkOutsideRIFFBoundsWithoutAllocating(t *testing.T) {
	t.Parallel()

	contents := wavHeader(Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, 0)
	binary.LittleEndian.PutUint32(contents[4:8], 12)
	binary.LittleEndian.PutUint32(contents[40:44], maxWAVDataBytes+1)
	_, _, err := ReadWAV(bytes.NewReader(contents))
	require.Error(t, err)
}

func TestReadWAVRejectsInconsistentPCMHeader(t *testing.T) {
	t.Parallel()

	w := &memoryWriteSeeker{}
	require.NoError(t, WriteWAV(w, Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}, []int16{1, 2}))

	badBlockAlign := bytes.Clone(w.data)
	binary.LittleEndian.PutUint16(badBlockAlign[32:34], 2)
	_, _, err := ReadWAV(bytes.NewReader(badBlockAlign))
	require.ErrorIs(t, err, ErrUnsupportedWAV)

	badByteRate := bytes.Clone(w.data)
	binary.LittleEndian.PutUint32(badByteRate[28:32], 16_000)
	_, _, err = ReadWAV(bytes.NewReader(badByteRate))
	require.ErrorIs(t, err, ErrUnsupportedWAV)

	partialFrame := bytes.Clone(w.data[:46])
	binary.LittleEndian.PutUint32(partialFrame[4:8], 38)
	binary.LittleEndian.PutUint32(partialFrame[40:44], 2)
	_, _, err = ReadWAV(bytes.NewReader(partialFrame))
	require.ErrorIs(t, err, ErrUnsupportedWAV)
}

func TestNewWAVWriterRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	_, err := NewWAVWriter(&memoryWriteSeeker{}, Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS24_3LE})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedWAV)
}
