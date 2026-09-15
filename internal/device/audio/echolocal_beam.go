package audio

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import "math"

// PhysicalMicrophones is the number of Echo Dot Gen 2 microphone inputs. The
// playback loopback channels 7 and 8 are deliberately not accepted here.
const PhysicalMicrophones = 7

const (
	echoLocalRingRadius   = 0.036
	echoLocalFirstMic     = 108 * math.Pi / 180
	echoLocalMicSpacing   = 60 * math.Pi / 180
	echoLocalSpeedOfSound = 343.0
	echoLocalBeams        = 6
	echoLocalBeamHold     = 8
	echoLocalDelayLine    = 8
)

type echoLocalTap struct {
	back int
	frac float32
}

// EchoLocalDelaySum is EchoLocal's steerable, fractional-delay, delay-and-sum
// candidate. Its geometry and channel order are an upstream baseline, not a
// selected calibration for this Dot. It is not safe for concurrent use.
type EchoLocalDelaySum struct {
	taps  [echoLocalBeams][PhysicalMicrophones]echoLocalTap
	hist  [PhysicalMicrophones][echoLocalDelayLine]float32
	at    int
	beam  int
	votes int
	next  int
	prev  [echoLocalBeams]float32
}

// NewEchoLocalDelaySum builds the upstream steering-delay baseline for the
// canonical 16 kHz capture rate.
func NewEchoLocalDelaySum() *EchoLocalDelaySum {
	b := &EchoLocalDelaySum{}
	spread := echoLocalRingRadius / echoLocalSpeedOfSound * float64(CanonicalSampleRate)
	for beam := range echoLocalBeams {
		bearing := float64(beam) * 2 * math.Pi / echoLocalBeams
		for mic := range PhysicalMicrophones {
			delay := spread
			if mic != PhysicalMicrophones-1 {
				delay = spread * (1 + math.Cos(bearing-(echoLocalFirstMic+float64(mic)*echoLocalMicSpacing)))
			}
			back := int(delay)
			b.taps[beam][mic] = echoLocalTap{back: back, frac: float32(delay - float64(back))}
		}
	}
	return b
}

// Mix combines exactly seven equally long physical-microphone frames. Invalid
// input is rejected without changing the beamformer's state.
func (b *EchoLocalDelaySum) Mix(mics [][]int16) []int16 {
	if !validPhysicalMicrophones(mics) || len(mics[0]) == 0 {
		return nil
	}

	out := make([]int16, len(mics[0]))
	energy := [echoLocalBeams]float64{}
	for sample := range out {
		for mic, samples := range mics {
			b.hist[mic][b.at] = float32(samples[sample])
		}
		for beam := range echoLocalBeams {
			value := b.sum(beam)
			delta := value - b.prev[beam]
			b.prev[beam] = value
			energy[beam] += float64(delta * delta)
			if beam == b.beam {
				out[sample] = clampPCM(value)
			}
		}
		b.at = (b.at + 1) & (echoLocalDelayLine - 1)
	}
	b.steer(energy)
	return out
}

// ProcessChannels implements MultichannelPreprocessor.
func (b *EchoLocalDelaySum) ProcessChannels(mics [][]int16) []int16 { return b.Mix(mics) }

// Process is retained for the Preprocessor seam. Capturer calls
// ProcessChannels before mono reduction for this candidate.
func (b *EchoLocalDelaySum) Process(in []int16) []int16 { return in }

// Name identifies this unqualified candidate in diagnostics.
func (*EchoLocalDelaySum) Name() string { return "echolocal-delay-sum" }

// Beam reports the presently selected upstream beam index.
func (b *EchoLocalDelaySum) Beam() int { return b.beam }

func validPhysicalMicrophones(mics [][]int16) bool {
	if len(mics) != PhysicalMicrophones {
		return false
	}
	for mic := 1; mic < len(mics); mic++ {
		if len(mics[mic]) != len(mics[0]) {
			return false
		}
	}
	return true
}

func (b *EchoLocalDelaySum) sum(beam int) float32 {
	var total float32
	for mic := range PhysicalMicrophones {
		tap := b.taps[beam][mic]
		older := b.hist[mic][(b.at-tap.back)&(echoLocalDelayLine-1)]
		oldest := b.hist[mic][(b.at-tap.back-1)&(echoLocalDelayLine-1)]
		total += older + (oldest-older)*tap.frac
	}
	return total / PhysicalMicrophones
}

func (b *EchoLocalDelaySum) steer(energy [echoLocalBeams]float64) {
	best := 0
	for beam := range echoLocalBeams {
		if energy[beam] > energy[best] {
			best = beam
		}
	}
	if best == b.beam {
		b.votes = 0
		return
	}
	if best != b.next {
		b.next, b.votes = best, 0
	}
	b.votes++
	if b.votes >= echoLocalBeamHold {
		b.beam, b.votes = best, 0
	}
}

func clampPCM(value float32) int16 {
	switch {
	case value > math.MaxInt16:
		return math.MaxInt16
	case value < math.MinInt16:
		return math.MinInt16
	default:
		return int16(value)
	}
}
