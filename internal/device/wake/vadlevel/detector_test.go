package vadlevel

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetector_SilenceScoresBelowSpeechFixture(t *testing.T) {
	t.Parallel()

	detector := NewDetector()
	silence := fixtureSamples(t, "silence_16k_mono.wav")
	for _, frame := range steps(silence) {
		detector.Observe(frame)
	}
	silenceScore := detector.SpeechScore()
	tone := fixtureSamples(t, "tone_1k_16k_mono.wav")
	detector.Observe(tone[:1_280])
	speechScore := detector.SpeechScore()

	assert.Less(t, silenceScore, 0.01)
	assert.Greater(t, speechScore-silenceScore, 0.75, "speech and the settled noise bed must have a useful separability margin")
}

func TestDetector_FloorTracksRisingNoiseBed(t *testing.T) {
	t.Parallel()

	detector := NewDetector()
	for range 60 {
		detector.Observe(sineFrame(100, 137))
	}
	detector.Observe(sineFrame(1_000, 137))
	first := detector.SpeechScore()
	for range 1_499 {
		detector.Observe(sineFrame(1_000, 137))
	}
	settled := detector.SpeechScore()

	assert.Greater(t, first, settled)
	assert.Less(t, settled, 0.01)
}

func TestDetector_GainNeverExceedsCeiling(t *testing.T) {
	t.Parallel()

	detector := NewDetector()
	for range 100 {
		detector.Observe(sineFrame(1, 1_000))
	}
	assert.LessOrEqual(t, detector.GainDB(), maxGainDB+1e-9)

	detector.Observe(sineFrame(32_000, 1_000))
	assert.LessOrEqual(t, detector.GainDB(), maxGainDB+1e-9)
}

func TestDetectorEmptyFrameDoesNotChangeState(t *testing.T) {
	t.Parallel()

	detector := NewDetector()
	wantScore, wantGain := detector.SpeechScore(), detector.GainDB()
	detector.Observe(nil)
	assert.InDelta(t, wantScore, detector.SpeechScore(), 0)
	assert.InDelta(t, wantGain, detector.GainDB(), 0)
}

func sineFrame(amplitude int, frequency float64) []int16 {
	frame := make([]int16, 1_280)
	for i := range frame {
		frame[i] = int16(math.Round(float64(amplitude) * math.Sin(2*math.Pi*frequency*float64(i)/sampleRate)))
	}
	return frame
}

func fixtureSamples(t *testing.T, name string) []int16 {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "audio", name)) //nolint:gosec // G304: callers pass fixed fixture names.
	require.NoError(t, err)
	format, samples, err := audio.ReadWAV(bytes.NewReader(contents))
	require.NoError(t, err)
	assert.Equal(t, audio.Format{SampleRate: sampleRate, Channels: 1, Layout: audio.LayoutS16LE}, format)
	return samples
}

func steps(samples []int16) [][]int16 {
	frames := make([][]int16, 0, len(samples)/1_280)
	for len(samples) >= 1_280 {
		frames = append(frames, samples[:1_280])
		samples = samples[1_280:]
	}
	return frames
}
