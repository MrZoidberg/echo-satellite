package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/mixer"
)

func TestSpeakerTest_WritesResampled48kStereoToFileSink(t *testing.T) {
	out := filepath.Join(t.TempDir(), "speaker.wav")
	var report bytes.Buffer
	require.NoError(t, speakerTest(&report, speakerTestCommand{In: audioFixture("tone_1k_16k_mono.wav"), Seconds: 1, ToFile: out, Resampler: "sinc", Volume: 1}))
	file, err := os.Open(out) //nolint:gosec // Test opens the path created in its private temporary directory.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	format, samples, err := audio.ReadWAV(file)
	require.NoError(t, err)
	assert.Equal(t, 48_000, format.SampleRate)
	assert.Equal(t, 2, format.Channels)
	assert.Len(t, samples, 48_000*2)
}

type failingSink struct{ closed bool }

func (*failingSink) Format() audio.Format {
	return audio.Format{SampleRate: 48_000, Channels: 2, Layout: audio.LayoutS16LE}
}
func (*failingSink) WriteInterleaved([]byte) (int, error) { return 0, errors.New("playback failed") }
func (s *failingSink) Close() error                       { s.closed = true; return nil }

type recordingAmp struct {
	values          []string
	closed          bool
	failRestoreOnce bool
}

func (*recordingAmp) Get(string) (string, error) { return mixer.ValueOff, nil }
func (a *recordingAmp) Set(_, value string) error {
	a.values = append(a.values, value)
	if value == mixer.ValueOff && a.failRestoreOnce {
		a.failRestoreOnce = false
		return errors.New("restore failed")
	}
	return nil
}

func TestSpeakerTest_ReportsPlaybackAndRestoreFailureThenRetriesRestore(t *testing.T) {
	sink := &failingSink{}
	amp := &recordingAmp{failRestoreOnce: true}
	oldSink, oldAmp := playbackSinkOpener, openAmpControl
	playbackSinkOpener = func(speakerTestCommand) (audio.PCMSink, error) { return sink, nil }
	openAmpControl = func(int) (ampControl, error) { return amp, nil }
	t.Cleanup(func() { playbackSinkOpener, openAmpControl = oldSink, oldAmp })
	err := speakerTest(&bytes.Buffer{}, speakerTestCommand{Seconds: 0.01, Resampler: "hold", Volume: 1})
	require.Error(t, err)
	require.ErrorContains(t, err, "playback failed")
	require.ErrorContains(t, err, "restore failed")
	assert.Equal(t, []string{mixer.ValueOn, mixer.ValueOff, mixer.ValueOff}, amp.values)
	assert.True(t, amp.closed)
}
func (a *recordingAmp) Close() error { a.closed = true; return nil }

func TestSpeakerTest_RestoresAmpStateOnFailure(t *testing.T) {
	sink := &failingSink{}
	amp := &recordingAmp{}
	oldSink, oldAmp := playbackSinkOpener, openAmpControl
	playbackSinkOpener = func(speakerTestCommand) (audio.PCMSink, error) { return sink, nil }
	openAmpControl = func(int) (ampControl, error) { return amp, nil }
	t.Cleanup(func() { playbackSinkOpener, openAmpControl = oldSink, oldAmp })
	err := speakerTest(&bytes.Buffer{}, speakerTestCommand{Seconds: 0.01, Resampler: "hold", Volume: 1})
	require.Error(t, err)
	assert.Equal(t, []string{mixer.ValueOn, mixer.ValueOff}, amp.values)
	assert.True(t, amp.closed)
	assert.True(t, sink.closed)
}
