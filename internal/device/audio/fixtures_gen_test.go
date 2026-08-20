package audio

import (
	"encoding/binary"
	"flag"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateAudioFixtures = flag.Bool("update-fixtures", false, "rewrite the fixtures under testdata/audio")

func buildFixtures(t *testing.T) map[string][]byte {
	t.Helper()

	const (
		sampleRate = 16_000
		duration   = 2
	)
	tone := make([]int16, sampleRate*duration)
	sweep := make([]int16, sampleRate*duration)
	silence := make([]int16, sampleRate*duration)
	noise := make([]int16, sampleRate*duration)
	rng := rand.New(rand.NewPCG(0x6563686f, 0x73617465)) //nolint:gosec // G404: deterministic fixture noise is required, not cryptographic randomness.
	for i := range tone {
		timeSeconds := float64(i) / sampleRate
		tone[i] = int16(math.Round(12_000 * math.Sin(2*math.Pi*1_000*timeSeconds)))
		frequencyRatio := 7_000.0 / 100.0
		phase := 2 * math.Pi * 100 * (math.Pow(frequencyRatio, timeSeconds/duration) - 1) / (math.Log(frequencyRatio) / duration)
		sweep[i] = int16(math.Round(10_000 * math.Sin(phase)))
		noise[i] = int16(rng.Int32N(16_001) - 8_000) //nolint:gosec // G115: generated values are explicitly bounded to [-8000,8000].
	}

	return map[string][]byte{
		"tone_1k_16k_mono.wav":  fixtureWAV(t, tone),
		"sweep_16k_mono.wav":    fixtureWAV(t, sweep),
		"silence_16k_mono.wav":  fixtureWAV(t, silence),
		"noise_16k_mono.wav":    fixtureWAV(t, noise),
		"dot_mic_9ch_s24le.raw": fixtureRawCapture(),
	}
}

func fixtureWAV(t *testing.T, samples []int16) []byte {
	t.Helper()
	w := &memoryWriteSeeker{}
	require.NoError(t, WriteWAV(w, Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, samples))
	return w.data
}

func fixtureRawCapture() []byte {
	const (
		sampleRate = 16_000
		channels   = 9
		duration   = 2
	)
	raw := make([]byte, sampleRate*channels*duration*3)
	for frame := range sampleRate * duration {
		timeSeconds := float64(frame) / sampleRate
		for channel := range channels {
			frequency := float64((channel + 1) * 500)
			amplitude := float64(2_000 + channel*1_000)
			sample := int32(math.Round(amplitude*math.Sin(2*math.Pi*frequency*timeSeconds))) << 8
			offset := (frame*channels + channel) * 3
			var encoded [4]byte
			binary.LittleEndian.PutUint32(encoded[:], uint32(sample)) //nolint:gosec // G115: conversion preserves signed two's-complement sample bits.
			copy(raw[offset:offset+3], encoded[:3])
		}
	}
	return raw
}

func TestFixtures_MatchGenerator(t *testing.T) {
	for name, want := range buildFixtures(t) {
		got, err := os.ReadFile(filepath.Join(audioFixtureDir(), name)) //nolint:gosec // G304: test-controlled fixture path
		require.NoError(t, err, "missing fixture %s", name)
		assert.Equal(t, want, got, "fixture %s is stale", name)
	}
}

func TestFixtures_Regenerate(t *testing.T) {
	if !*updateAudioFixtures {
		t.Skip("run with -update-fixtures to rewrite testdata/audio")
	}
	require.NoError(t, os.MkdirAll(audioFixtureDir(), 0o750))
	for name, content := range buildFixtures(t) {
		require.NoError(t, os.WriteFile(filepath.Join(audioFixtureDir(), name), content, 0o600))
	}
}

func audioFixtureDir() string {
	return filepath.Join("..", "..", "..", "testdata", "audio")
}
