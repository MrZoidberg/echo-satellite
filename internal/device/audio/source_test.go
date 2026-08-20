package audio

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileSource_ReadsRawDeviceShapedPCM(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.raw")
	require.NoError(t, os.WriteFile(path, []byte{1, 2, 3, 4, 5, 6}, 0o600))
	format := Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS24_3LE}
	source, err := NewFileSource(path, format, false)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, source.Close()) })

	buf := make([]byte, 6)
	frames, err := source.ReadInterleaved(buf)
	require.NoError(t, err)
	assert.Equal(t, 2, frames)
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6}, buf)
	assert.Equal(t, format, source.Format())
	_, err = source.ReadInterleaved(buf)
	assert.ErrorIs(t, err, io.EOF)
}

func TestFileSource_ReadsWAVAndReportsHeaderFormat(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.wav")
	file, err := os.Create(path) //nolint:gosec // Test path is under t.TempDir.
	require.NoError(t, err)
	format := Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}
	require.NoError(t, WriteWAV(file, format, []int16{-1, 2, -3, 4}))
	require.NoError(t, file.Close())

	source, err := NewFileSource(path, Format{}, false)
	require.NoError(t, err)
	buf := make([]byte, 8)
	frames, err := source.ReadInterleaved(buf)
	require.NoError(t, err)
	assert.Equal(t, 2, frames)
	assert.Equal(t, format, source.Format())
	assert.Equal(t, uint16(0xffff), binary.LittleEndian.Uint16(buf[0:2]))
	assert.Equal(t, uint16(2), binary.LittleEndian.Uint16(buf[2:4]))
	assert.NoError(t, source.Close())
	assert.NoError(t, source.Close(), "Close must be idempotent")
}

func TestFileSource_RejectsTruncatedRawFrame(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.raw")
	require.NoError(t, os.WriteFile(path, []byte{1, 2, 3}, 0o600))
	source, err := NewFileSource(path, Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}, false)
	require.NoError(t, err)
	_, err = source.ReadInterleaved(make([]byte, 8))
	assert.ErrorIs(t, err, ErrShortBuffer)
}
