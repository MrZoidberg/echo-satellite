package turns

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestReceiver_DefaultDiscardAndFraming(t *testing.T) {
	t.Parallel()
	receiver := Receiver{}
	active, err := receiver.Begin("turn-1", protocol.TurnStart{Trigger: protocol.TriggerWake}, time.Now())
	require.NoError(t, err)
	require.ErrorIs(t, receiver.Write(active, []byte{1, 2}), ErrAudioNotOpen)
	require.NoError(t, receiver.StartAudio(active, "turn-1", protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}))
	require.NoError(t, receiver.Write(active, []byte{1, 2, 3, 4}))
	turn, err := receiver.Stop(active, "turn-1", protocol.AudioStop{Reason: protocol.AudioStopEndpointed})
	require.NoError(t, err)
	assert.EqualValues(t, 4, turn.Bytes)
	assert.Empty(t, turn.WAVPath)
	require.ErrorIs(t, receiver.Write(active, []byte{1, 2}), ErrAudioNotOpen)
}

func TestReceiver_RejectsInvalidAudio(t *testing.T) {
	t.Parallel()
	receiver := Receiver{}
	active, err := receiver.Begin("turn-1", protocol.TurnStart{Trigger: protocol.TriggerButton}, time.Now())
	require.NoError(t, err)
	assert.ErrorIs(t, receiver.StartAudio(active, "turn-1", protocol.AudioStart{SampleRate: 48000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}), ErrInvalidAudio)
}

func TestReceiver_PromotesCompletedDiagnosticWAV(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	receiver := Receiver{Directory: directory}
	active, err := receiver.Begin("turn/1", protocol.TurnStart{Trigger: protocol.TriggerWake}, time.Now())
	require.NoError(t, err)
	require.NoError(t, receiver.StartAudio(active, "turn/1", protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}))
	require.NoError(t, receiver.Write(active, []byte{1, 2, 3, 4}))
	turn, err := receiver.Stop(active, "turn/1", protocol.AudioStop{Reason: protocol.AudioStopEndpointed})
	require.NoError(t, err)
	assert.FileExists(t, turn.WAVPath)
	assert.DirExists(t, filepath.Dir(turn.WAVPath))
	data, err := os.ReadFile(turn.WAVPath)
	require.NoError(t, err)
	assert.Equal(t, "RIFF", string(data[:4]))
	assert.Equal(t, []byte{1, 2, 3, 4}, data[44:])
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestReceiver_AbortsDiagnosticWAV(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	receiver := Receiver{Directory: directory}
	active, err := receiver.Begin("turn-1", protocol.TurnStart{Trigger: protocol.TriggerWake}, time.Now())
	require.NoError(t, err)
	require.NoError(t, receiver.StartAudio(active, "turn-1", protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}))
	active.Abort()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
