package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
)

func audioFixture(name string) string { return filepath.Join("..", "..", "testdata", "audio", name) }

func TestMicRecord_WritesWAVFromFixtureSource(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mic.wav")
	healthOut := filepath.Join(t.TempDir(), "mic.health.json")
	var report bytes.Buffer
	require.NoError(t, micRecord(&report, micRecordCommand{Seconds: 0.02, Out: out, HealthOut: healthOut, Channels: "all", FromFile: audioFixture("dot_mic_9ch_s24le.raw")}))
	file, err := os.Open(out) //nolint:gosec // Test opens the path created in its private temporary directory.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	format, samples, err := audio.ReadWAV(file)
	require.NoError(t, err)
	assert.Equal(t, 7, format.Channels)
	assert.Len(t, samples, 320*7)
	data, err := os.ReadFile(healthOut) //nolint:gosec // Test reads the sidecar it wrote in its private temporary directory.
	require.NoError(t, err)
	var health micCaptureHealth
	require.NoError(t, json.Unmarshal(data, &health))
	digest, err := fileSHA256(out)
	require.NoError(t, err)
	assert.Equal(t, digest, health.SHA256)
	assert.Zero(t, health.XRuns)
	assert.Zero(t, health.DroppedFrames)
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6}, health.Channels)
	assert.Equal(t, 320, health.Frames)
}

func TestMicRecord_PrintsPerChannelLevels(t *testing.T) {
	var report bytes.Buffer
	err := micRecord(&report, micRecordCommand{Seconds: 0.02, Out: filepath.Join(t.TempDir(), "mic.wav"), Channels: "all", FromFile: audioFixture("dot_mic_9ch_s24le.raw"), PrintLevels: true})
	require.NoError(t, err)
	expectedPeaks := []string{"-24.29", "-20.77", "-18.27", "-16.33", "-14.75", "-13.41", "-12.25"}
	for channel, peak := range expectedPeaks {
		assert.Contains(t, report.String(), "channel mic"+string(rune('0'+channel))+": peak "+peak+" dBFS")
	}
}

func TestParseMicChannels_RejectsLoopbackAndDuplicates(t *testing.T) {
	_, err := parseMicChannels("mic7", 9)
	require.Error(t, err)
	_, err = parseMicChannels("mic0,mic0", 9)
	require.Error(t, err)
}

func TestMicScorecard_WritesSevenChannelMetricsAndDeletesInput(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "speech.wav")
	noise := filepath.Join(directory, "noise.wav")
	health := filepath.Join(directory, "speech.health.json")
	writeScorecardWAV(t, input, 800)
	writeScorecardWAV(t, noise, 10)
	digest, err := fileSHA256(input)
	require.NoError(t, err)
	require.NoError(t, writeJSONFile(health, micCaptureHealth{SHA256: digest, SampleRate: 16_000, Channels: []int{0, 1, 2, 3, 4, 5, 6}, Frames: 512}))
	out := filepath.Join(directory, "scorecard.json")

	var report bytes.Buffer
	err = micScorecard(&report, micScorecardCommand{Input: input, Noise: noise, Health: health, Out: out, Position: "front", DistanceMM: 1000, Condition: "normal-speech"})
	require.NoError(t, err)
	assert.Contains(t, report.String(), "raw audio deleted")
	_, err = os.Stat(input)
	require.ErrorIs(t, err, os.ErrNotExist)

	data, err := os.ReadFile(out) //nolint:gosec // Test reads the scorecard it wrote in its private temporary directory.
	require.NoError(t, err)
	var scorecard micScorecardReport
	require.NoError(t, json.Unmarshal(data, &scorecard))
	assert.Equal(t, 7, scorecard.Capture.Channels)
	assert.Equal(t, "S16_LE", scorecard.Capture.Layout)
	assert.Equal(t, "verified", scorecard.Capture.Health)
	assert.Equal(t, "front", scorecard.Position)
	assert.Equal(t, 1000, scorecard.DistanceMM)
	require.NotNil(t, scorecard.XRuns)
	require.NotNil(t, scorecard.DroppedFrames)
	assert.Zero(t, *scorecard.XRuns)
	assert.Zero(t, *scorecard.DroppedFrames)
	require.Len(t, scorecard.Channels, 7)
	assert.Equal(t, "reference", scorecard.Channels[0].Polarity)
	assert.Equal(t, "inverted", scorecard.Channels[1].Polarity)
	assert.InDelta(t, -1, scorecard.Channels[1].CorrelationWithMic0, 0.001)
	require.NotNil(t, scorecard.Channels[0].NoiseFloorDBFS)
	require.NotNil(t, scorecard.Channels[0].SpeechNoiseSeparationDB)
	assert.InDelta(t, 38.06, *scorecard.Channels[0].SpeechNoiseSeparationDB, 0.1)
}

func TestMicScorecard_SilenceIsJSONSafeAndHealthIsUnavailableWithoutSidecar(t *testing.T) {
	input := filepath.Join(t.TempDir(), "silence.wav")
	writeScorecardWAV(t, input, 0)
	out := filepath.Join(t.TempDir(), "scorecard.json")
	require.NoError(t, micScorecard(io.Discard, micScorecardCommand{Input: input, Out: out, Position: "front", DistanceMM: 1000, Condition: "silence"}))
	data, err := os.ReadFile(out) //nolint:gosec // Test reads its private scorecard artifact.
	require.NoError(t, err)
	var scorecard micScorecardReport
	require.NoError(t, json.Unmarshal(data, &scorecard))
	assert.Nil(t, scorecard.XRuns)
	assert.Nil(t, scorecard.DroppedFrames)
	assert.Equal(t, "unverified", scorecard.Capture.Health)
	require.Len(t, scorecard.Channels, 7)
	assert.Nil(t, scorecard.Channels[0].PeakDBFS)
	assert.Nil(t, scorecard.Channels[0].RMSDBFS)
}

func TestStrongestCorrelation_ReportsKnownDelayAndStablePeriodicTie(t *testing.T) {
	const frames = 600
	samples := make([]int16, frames*2)
	for frame := range frames {
		value := int16((frame*37)%1000 - 500)
		samples[frame*2] = value
		if frame >= 3 {
			samples[frame*2+1] = samples[(frame-3)*2]
		}
	}
	delay, correlation := strongestCorrelation(samples, 2, 1)
	assert.Equal(t, 3, delay)
	assert.InDelta(t, 1, correlation, 0.001)

	for frame := range frames {
		value := int16(100)
		if frame%2 == 1 {
			value = -value
		}
		samples[frame*2], samples[frame*2+1] = value, value
	}
	delay, correlation = strongestCorrelation(samples, 2, 1)
	assert.Zero(t, delay)
	assert.InDelta(t, 1, correlation, 0.001)
}

func TestMicScorecard_RejectsNonPhysicalChannelCapture(t *testing.T) {
	input := filepath.Join(t.TempDir(), "nine-channel.wav")
	file, err := os.Create(input) //nolint:gosec // Test creates a fixture in its private temporary directory.
	require.NoError(t, err)
	wav, err := audio.NewWAVWriter(file, audio.Format{SampleRate: 16_000, Channels: 9, Layout: audio.LayoutS16LE})
	require.NoError(t, err)
	_, err = wav.Write(make([]int16, 9*10))
	require.NoError(t, err)
	require.NoError(t, wav.Close())
	require.NoError(t, file.Close())

	err = micScorecard(io.Discard, micScorecardCommand{Input: input, Out: filepath.Join(t.TempDir(), "scorecard.json"), Position: "front", DistanceMM: 1000, Condition: "silence"})
	require.ErrorContains(t, err, "exactly seven physical microphones")
}

func writeScorecardWAV(t *testing.T, path string, amplitude int16) {
	t.Helper()
	file, err := os.Create(path) //nolint:gosec // Test creates a fixture in its private temporary directory.
	require.NoError(t, err)
	wav, err := audio.NewWAVWriter(file, audio.Format{SampleRate: 16_000, Channels: 7, Layout: audio.LayoutS16LE})
	require.NoError(t, err)
	samples := make([]int16, 7*512)
	for frame := range 512 {
		value := amplitude
		if frame%2 == 1 {
			value = -value
		}
		for channel := range 7 {
			samples[frame*7+channel] = value
		}
		// A phase-inverted mic1 proves that polarity is measured, not inferred.
		samples[frame*7+1] = -value
	}
	_, err = wav.Write(samples)
	require.NoError(t, err)
	require.NoError(t, wav.Close())
	require.NoError(t, file.Close())
}
