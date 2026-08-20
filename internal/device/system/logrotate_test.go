package system

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRotatingWriter_NeverExceedsConfiguredTotalBytes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "echod.log")
	w, err := NewRotatingWriter(path, 30, 3)
	require.NoError(t, err)
	for range 20 {
		n, writeErr := w.Write([]byte("1234567"))
		require.NoError(t, writeErr)
		assert.Equal(t, 7, n)
		assert.LessOrEqual(t, totalLogBytes(t, path, 3), int64(30))
	}
	require.NoError(t, w.Close())
}

func TestRotatingWriter_RetainsNewestBytesAcrossFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "echod.log")
	w, err := NewRotatingWriter(path, 12, 3)
	require.NoError(t, err)
	_, err = w.Write([]byte("abcdefghijklmnop"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	contents := readLogFilesOldestFirst(t, path, 3)
	assert.Equal(t, []byte("efghijklmnop"), contents)
}

func TestNewRotatingWriter_RejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	_, err := NewRotatingWriter(filepath.Join(t.TempDir(), "x"), 0, 1)
	require.Error(t, err)
	_, err = NewRotatingWriter(filepath.Join(t.TempDir(), "x"), 1, 2)
	require.Error(t, err)
}

func TestRotatingWriter_WriteAfterCloseFails(t *testing.T) {
	t.Parallel()

	w, err := NewRotatingWriter(filepath.Join(t.TempDir(), "echod.log"), 10, 1)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = w.Write([]byte("x"))
	require.Error(t, err)
}

func TestNewRotatingWriter_BoundsExistingFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "echod.log")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), 20), 0o600))
	require.NoError(t, os.WriteFile(path+".1", bytes.Repeat([]byte("y"), 20), 0o600))
	w, err := NewRotatingWriter(path, 12, 3)
	require.NoError(t, err)
	assert.LessOrEqual(t, totalLogBytes(t, path, 3), int64(12))
	require.NoError(t, w.Close())
}

func TestNewRotatingWriter_RemovesStaleGenerationsAfterCountReduction(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "echod.log")
	require.NoError(t, os.WriteFile(path+".4", bytes.Repeat([]byte("x"), 20), 0o600))
	require.NoError(t, os.WriteFile(path+".notes", []byte("keep"), 0o600))
	w, err := NewRotatingWriter(path, 12, 3)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = os.Stat(path + ".4")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(path + ".notes")
	require.NoError(t, err)
}

func totalLogBytes(t *testing.T, path string, count int) int64 {
	t.Helper()

	var total int64
	for index := range count {
		info, err := os.Stat(rotatedPath(path, index))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		total += info.Size()
	}
	return total
}

func readLogFilesOldestFirst(t *testing.T, path string, count int) []byte {
	t.Helper()

	var result bytes.Buffer
	for index := count - 1; index >= 0; index-- {
		contents, err := os.ReadFile(rotatedPath(path, index))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		result.Write(contents)
	}
	return result.Bytes()
}
