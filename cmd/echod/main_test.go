package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/MrZoidberg/echo-satellite/internal/device/led"
)

func TestOpenWakeOnlySource_LiveUsesDotMicrophone(t *testing.T) {
	wantErr := errors.New("stop after config capture")
	var got alsa.Config
	original := openALSACapture
	openALSACapture = func(config alsa.Config) (*alsa.PCM, error) {
		got = config
		return nil, wantErr
	}
	t.Cleanup(func() { openALSACapture = original })

	_, err := openWakeOnlySource(opts{})
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, alsa.MicCard, got.Card)
	assert.Equal(t, alsa.MicDevice, got.Device)
	assert.True(t, got.Capture)
}

func TestLogValue_RemovesRecordSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "gateway.example:8443", logValue("gateway.example\r\n:8443"))
}

type orchestrationSource struct {
	closed chan struct{}
	count  int
	err    error
}

func newOrchestrationSource() *orchestrationSource {
	return &orchestrationSource{closed: make(chan struct{})}
}

func (s *orchestrationSource) Close() error {
	s.count++
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return s.err
}

func TestRunWakeWorkers_NormalEOFClosesSource(t *testing.T) {
	source := newOrchestrationSource()
	err := runWakeWorkers(context.Background(), source, []wakeWorker{
		func(context.Context) error { return nil },
		func(ctx context.Context) error { <-ctx.Done(); return nil },
	})
	require.NoError(t, err)
	assert.Equal(t, 1, source.count)
	assertClosed(t, source.closed)
}

func TestRunWakeWorkers_CancellationInterruptsBlockingSourceBeforeWait(t *testing.T) {
	source := newOrchestrationSource()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runWakeWorkers(ctx, source, []wakeWorker{
			func(context.Context) error { <-source.closed; return nil },
		})
	}()
	cancel()
	require.NoError(t, <-done)
	assert.Equal(t, 1, source.count)
	assertClosed(t, source.closed)
}

func TestRunWakeWorkers_FirstErrorCancelsPeersAndPreservesCleanupErrors(t *testing.T) {
	workerErr := errors.New("worker failed")
	closeErr := errors.New("close failed")
	source := newOrchestrationSource()
	source.err = closeErr
	peerStopped := make(chan struct{})

	err := runWakeWorkers(context.Background(), source, []wakeWorker{
		func(context.Context) error { return workerErr },
		func(ctx context.Context) error { <-ctx.Done(); close(peerStopped); return nil },
	})
	require.ErrorIs(t, err, workerErr)
	require.ErrorIs(t, err, closeErr)
	assert.Equal(t, 1, source.count)
	assertClosed(t, peerStopped)
}

func TestRunWakeWorkers_PreservesLaterPeerErrors(t *testing.T) {
	firstErr := errors.New("first worker failed")
	peerErr := errors.New("peer cleanup failed")
	closeErr := errors.New("close failed")
	source := newOrchestrationSource()
	source.err = closeErr

	err := runWakeWorkers(context.Background(), source, []wakeWorker{
		func(context.Context) error { return firstErr },
		func(ctx context.Context) error { <-ctx.Done(); return peerErr },
	})
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, peerErr)
	require.ErrorIs(t, err, closeErr)
}

type closeCountingPCMSource struct {
	closes int
	err    error
}

func (*closeCountingPCMSource) Format() audio.Format                { return audio.Format{} }
func (*closeCountingPCMSource) ReadInterleaved([]byte) (int, error) { return 0, io.EOF }
func (s *closeCountingPCMSource) Close() error                      { s.closes++; return s.err }

func TestCloseOncePCMSource_IsIdempotentAndPreservesError(t *testing.T) {
	closeErr := errors.New("close failed")
	raw := &closeCountingPCMSource{err: closeErr}
	source := &closeOncePCMSource{PCMSource: raw}
	require.ErrorIs(t, source.Close(), closeErr)
	require.ErrorIs(t, source.Close(), closeErr)
	assert.Equal(t, 1, raw.closes)
}

func assertClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	default:
		t.Fatal("channel was not closed")
	}
}

func TestStartWakeLED_RendersAndClearsConfiguredTestRoot(t *testing.T) {
	root := t.TempDir()
	framePath := filepath.Join(root, "frame")
	require.NoError(t, os.WriteFile(framePath, nil, 0o600))

	animator, clearLED, err := startWakeLED(root)
	require.NoError(t, err)
	require.NotNil(t, animator)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- animator.Run(ctx) }()
	require.Eventually(t, func() bool {
		contents, readErr := os.ReadFile(framePath) //nolint:gosec // framePath is inside the test-owned temporary directory.
		return readErr == nil && len(contents) > 0
	}, time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.NoError(t, clearLED())

	contents, err := os.ReadFile(framePath) //nolint:gosec // framePath is inside the test-owned temporary directory.
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("000000", led.SegmentCount)+"\n", string(contents))
}

func TestStartWakeLED_RejectsMissingConfiguredTestRoot(t *testing.T) {
	_, _, err := startWakeLED(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configured frame")
}
