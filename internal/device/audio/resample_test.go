package audio

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldResampler_RepeatsSamples(t *testing.T) {
	t.Parallel()
	r := NewHoldResampler(2, 4)
	dst := make([]int16, 6)
	n := r.Resample(dst, []int16{10, 20, 30})
	assert.InDelta(t, 2.0, r.Ratio(), 0)
	assert.Equal(t, 6, n)
	assert.Equal(t, []int16{10, 10, 20, 20, 30, 30}, dst)
}

func TestLinearResampler_ProducesExactRatioLength(t *testing.T) {
	t.Parallel()
	r := NewLinearResampler(16_000, 48_000)
	dst := make([]int16, 12)
	n := r.Resample(dst, []int16{0, 300, 600, 900})
	assert.Equal(t, 12, n)
	assert.Equal(t, []int16{0, 100, 200, 300, 400, 500, 600, 700, 800, 900, 900, 900}, dst)
}

func TestSincResampler_16kTo48kKeepsToneSNRAbove40dB(t *testing.T) {
	t.Parallel()
	const sourceRate = 16_000
	const destinationRate = 48_000
	source := generatedTone(sourceRate, sourceRate)
	destination := make([]int16, len(source)*3)
	n := NewSincResampler(sourceRate, destinationRate).Resample(destination, source)
	reference := generatedTone(destinationRate, n)

	// Ignore the filter's boundary region; the steady-state output is the
	// quality relevant to streamed periods.
	snr := signalToNoise(reference[96:n-96], destination[96:n-96])
	t.Logf("measured sinc resampler SNR: %.2f dB", snr)
	assert.Greater(t, snr, 40.0)
}

func TestSincResampler_SuppressesAliasingOnSweepFixture(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(audioFixtureDir(), "sweep_16k_mono.wav"))
	require.NoError(t, err)
	format, samples, err := ReadWAV(bytes.NewReader(contents))
	require.NoError(t, err)
	require.Equal(t, CanonicalSampleRate, format.SampleRate)

	// The latter half of the fixture sweeps above the 8 kHz output's Nyquist
	// limit. A hold converter aliases that energy; the sinc low-pass rejects it.
	highBand := samples[len(samples)*3/4:]
	sincOutput := make([]int16, len(highBand)/2)
	holdOutput := make([]int16, len(highBand)/2)
	NewSincResampler(16_000, 8_000).Resample(sincOutput, highBand)
	NewHoldResampler(16_000, 8_000).Resample(holdOutput, highBand)
	sincRMS := rms(sincOutput[64 : len(sincOutput)-64])
	holdRMS := rms(holdOutput[64 : len(holdOutput)-64])
	t.Logf("high-band RMS: sinc %.2f, hold %.2f", sincRMS, holdRMS)
	assert.Less(t, sincRMS, holdRMS*0.65)
}

func TestResamplers_EmptyAndBoundedDestination(t *testing.T) {
	t.Parallel()
	resamplers := []Resampler{
		NewHoldResampler(16_000, 48_000),
		NewLinearResampler(16_000, 48_000),
		NewSincResampler(16_000, 48_000),
	}
	for _, r := range resamplers {
		assert.Zero(t, r.Resample(nil, []int16{1}))
		assert.Zero(t, r.Resample(make([]int16, 2), nil))
		dst := make([]int16, 2)
		assert.Equal(t, 2, r.Resample(dst, []int16{1, 2}))
	}
}

func generatedTone(sampleRate, count int) []int16 {
	samples := make([]int16, count)
	for i := range samples {
		samples[i] = int16(12_000 * math.Sin(2*math.Pi*1_000*float64(i)/float64(sampleRate)))
	}
	return samples
}

func signalToNoise(reference, actual []int16) float64 {
	var signal, noise float64
	for i, value := range reference {
		signal += float64(value) * float64(value)
		difference := float64(value) - float64(actual[i])
		noise += difference * difference
	}
	return 10 * math.Log10(signal/noise)
}

func rms(samples []int16) float64 {
	var sum float64
	for _, sample := range samples {
		sum += float64(sample) * float64(sample)
	}
	return math.Sqrt(sum / float64(len(samples)))
}
