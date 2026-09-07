package endpointing

import (
	"flag"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateFixtures = flag.Bool("update-fixtures", false, "rewrite endpointing audio fixtures")

func TestFixturesMatchGenerator(t *testing.T) {
	want := endpointingFixture()
	got, err := os.ReadFile(endpointingFixturePath())
	require.NoError(t, err, "missing command endpointing fixture")
	require.Equal(t, want, got, "command endpointing fixture is stale")
}

func TestEndpointingFixtureSections(t *testing.T) {
	file, err := os.Open(endpointingFixturePath())
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	format, samples, err := audio.ReadWAV(file)
	require.NoError(t, err)
	assert.Equal(t, audio.Format{SampleRate: 16_000, Channels: 1, Layout: audio.LayoutS16LE}, format)
	require.Len(t, samples, 60_800)
	assert.Less(t, rms(samples[:8_000]), 200.0)
	assert.Greater(t, rms(samples[8_000:16_000]), 4_000.0)
	assert.Less(t, rms(samples[16_000:27_200]), 200.0)
	assert.Greater(t, rms(samples[27_200:35_200]), 4_000.0)
	assert.Less(t, rms(samples[35_200:]), 200.0)
}

func TestFixturesRegenerate(t *testing.T) {
	if !*updateFixtures {
		t.Skip("run with -update-fixtures to rewrite endpointing audio fixtures")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(endpointingFixturePath()), 0o750))
	require.NoError(t, os.WriteFile(endpointingFixturePath(), endpointingFixture(), 0o600))
}

func endpointingFixturePath() string {
	return filepath.Join("..", "..", "..", "testdata", "audio", "command_endpointing_16k_mono.wav")
}

func endpointingFixture() []byte {
	const sampleRate = 16_000
	// 0.5 s room noise, 0.5 s speech, 0.7 s pause, 0.5 s more speech, 1.6 s trailing silence.
	durations := []float64{.5, .5, .7, .5, 1.6}
	amplitudes := []float64{150, 7000, 150, 7000, 150}
	rng := rand.New(rand.NewPCG(0x6563686f, 0x656e6470)) //nolint:gosec // G404: fixture noise must be repeatable.
	samples := make([]int16, 0, 61_000)
	for section, seconds := range durations {
		for index := range int(seconds * sampleRate) {
			value := float64(rng.Int32N(301) - 150)
			if amplitudes[section] > 1_000 {
				value += amplitudes[section] * math.Sin(2*math.Pi*180*float64(index)/sampleRate)
			}
			samples = append(samples, int16(math.Round(value)))
		}
	}
	file, createErr := os.CreateTemp("", "endpointing-fixture-*.wav")
	if createErr != nil {
		panic(createErr)
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	writeErr := audio.WriteWAV(file, audio.Format{SampleRate: sampleRate, Channels: 1, Layout: audio.LayoutS16LE}, samples)
	if writeErr != nil {
		panic(writeErr)
	}
	closeErr := file.Close()
	if closeErr != nil {
		panic(closeErr)
	}
	data, readErr := os.ReadFile(name) //nolint:gosec // G304: test reads its own temporary file.
	if readErr != nil {
		panic(readErr)
	}
	return data
}

func rms(samples []int16) float64 {
	var sum float64
	for _, sample := range samples {
		sum += float64(sample) * float64(sample)
	}
	return math.Sqrt(sum / float64(len(samples)))
}
