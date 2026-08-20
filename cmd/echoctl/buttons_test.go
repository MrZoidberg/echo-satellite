package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestButtonsTest_FromFilePrintsRecognizedPress(t *testing.T) {
	var out bytes.Buffer
	err := buttonsTest(&out, buttonsTestCommand{
		FromFile: filepath.Join("..", "..", "testdata", "buttons", "action_tap.bin"),
		Seconds:  1,
	})
	require.NoError(t, err)
	assert.Equal(t, "action tap held=250ms\n", out.String())
}

func TestButtonsTest_RejectsInvalidDurationAndMissingFixture(t *testing.T) {
	require.Error(t, buttonsTest(io.Discard, buttonsTestCommand{Seconds: 0}))
	err := buttonsTest(io.Discard, buttonsTestCommand{FromFile: filepath.Join(t.TempDir(), "missing"), Seconds: 1})
	assert.Error(t, err)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestButtonsTest_ReportsOutputFailure(t *testing.T) {
	err := buttonsTest(failingWriter{}, buttonsTestCommand{
		FromFile: filepath.Join("..", "..", "testdata", "buttons", "action_tap.bin"),
		Seconds:  1,
	})
	assert.Error(t, err)
}

type errorReadCloser struct{ err error }

func (r errorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (errorReadCloser) Close() error               { return nil }

func TestWatchButtons_PreservesFirstDeviceErrorAndCancelsBlockedPeer(t *testing.T) {
	want := errors.New("device failed")
	blocked, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	err := watchButtons(ctx, io.Discard, []io.ReadCloser{blocked, errorReadCloser{err: want}})
	require.ErrorIs(t, err, want)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
}
