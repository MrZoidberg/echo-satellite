package audio

import (
	"math"

	"github.com/MrZoidberg/echo-satellite/internal/device/vec"
)

const (
	sincTaps   = 32
	sincPhases = 1024
)

// Resampler converts mono PCM between fixed sample rates.
type Resampler interface {
	Resample(dst, src []int16) int
	Ratio() float64
}

// HoldResampler uses nearest-neighbor sample-and-hold conversion.
type HoldResampler struct{ ratio float64 }

func NewHoldResampler(sourceRate, destinationRate int) HoldResampler {
	return HoldResampler{ratio: sampleRateRatio(sourceRate, destinationRate)}
}

func (r HoldResampler) Ratio() float64 { return r.ratio }

func (r HoldResampler) Resample(dst, src []int16) int {
	n := resampledLength(len(src), len(dst), r.ratio)
	for i := range n {
		dst[i] = src[min(int(float64(i)/r.ratio), len(src)-1)]
	}
	return n
}

// LinearResampler linearly interpolates between adjacent input samples.
type LinearResampler struct{ ratio float64 }

func NewLinearResampler(sourceRate, destinationRate int) LinearResampler {
	return LinearResampler{ratio: sampleRateRatio(sourceRate, destinationRate)}
}

func (r LinearResampler) Ratio() float64 { return r.ratio }

func (r LinearResampler) Resample(dst, src []int16) int {
	n := resampledLength(len(src), len(dst), r.ratio)
	for i := range n {
		position := float64(i) / r.ratio
		left := min(int(position), len(src)-1)
		right := min(left+1, len(src)-1)
		fraction := position - float64(left)
		dst[i] = clampInt16(float64(src[left])*(1-fraction) + float64(src[right])*fraction)
	}
	return n
}

// SincResampler uses a windowed-sinc polyphase low-pass filter. Its tables and
// work buffers are retained, so calls allocate only when a larger input arrives.
// A SincResampler must not be used concurrently.
type SincResampler struct {
	ratio        float64
	coefficients [sincPhases][sincTaps]float32
	input        []float32
	window       []float32
}

func NewSincResampler(sourceRate, destinationRate int) *SincResampler {
	r := &SincResampler{ratio: sampleRateRatio(sourceRate, destinationRate), window: make([]float32, sincTaps)}
	cutoff := min(1.0, r.ratio)
	if r.ratio < 1 {
		// Leave a narrow transition band below the destination Nyquist limit;
		// a finite window cannot change from passband to stopband instantly.
		cutoff *= 0.94
	}
	for phase := range sincPhases {
		fraction := float64(phase) / sincPhases
		var sum float64
		for tap := range sincTaps {
			x := float64(tap-(sincTaps/2-1)) - fraction
			coefficient := cutoff * sinc(cutoff*x) * blackman(float64(tap)/float64(sincTaps-1))
			r.coefficients[phase][tap] = float32(coefficient)
			sum += coefficient
		}
		for tap := range sincTaps {
			r.coefficients[phase][tap] /= float32(sum)
		}
	}
	return r
}

func (r *SincResampler) Ratio() float64 { return r.ratio }

func (r *SincResampler) Resample(dst, src []int16) int {
	n := resampledLength(len(src), len(dst), r.ratio)
	if n == 0 {
		return 0
	}
	if cap(r.input) < len(src) {
		r.input = make([]float32, len(src))
	}
	r.input = r.input[:len(src)]
	for i, sample := range src {
		r.input[i] = float32(sample)
	}
	for i := range n {
		position := float64(i) / r.ratio
		center := int(position)
		fraction := position - float64(center)
		phase := min(int(fraction*sincPhases+0.5), sincPhases-1)
		start := center - (sincTaps/2 - 1)
		for tap := range sincTaps {
			index := max(0, min(start+tap, len(src)-1))
			r.window[tap] = r.input[index]
		}
		dst[i] = clampInt16(float64(vec.Dot(r.window, r.coefficients[phase][:])))
	}
	return n
}

func sampleRateRatio(sourceRate, destinationRate int) float64 {
	if sourceRate <= 0 || destinationRate <= 0 {
		panic("audio: sample rates must be positive")
	}
	return float64(destinationRate) / float64(sourceRate)
}

func resampledLength(sourceLength, destinationLength int, ratio float64) int {
	if sourceLength == 0 || destinationLength == 0 {
		return 0
	}
	return min(int(math.Ceil(float64(sourceLength)*ratio)), destinationLength)
}

func clampInt16(value float64) int16 {
	value = math.Round(value)
	return int16(max(float64(math.MinInt16), min(value, float64(math.MaxInt16))))
}

func sinc(x float64) float64 {
	if math.Abs(x) < 1e-12 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

func blackman(position float64) float64 {
	return 0.42 - 0.5*math.Cos(2*math.Pi*position) + 0.08*math.Cos(4*math.Pi*position)
}
