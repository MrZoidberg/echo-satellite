package audio

import (
	"errors"
	"math"
	"time"
)

const (
	// MaxConditioningGainDB bounds automatic output gain for every candidate.
	MaxConditioningGainDB = 12.0
	conditioningHeadroom  = 1.0
	conditioningTargetDB  = -20.0
	conditioningAttack    = 20 * time.Millisecond
	conditioningRelease   = 500 * time.Millisecond
	// ConditioningCadenceSamples is the canonical 80 ms processing block.
	ConditioningCadenceSamples = CanonicalSampleRate * 80 / 1000
)

// ConditioningCandidate identifies one processor being qualified. These names
// are diagnostic-only: the gateway exposes only a successfully qualified
// hardware profile.
type ConditioningCandidate string

const (
	CandidateChannel0     ConditioningCandidate = "channel-0"
	CandidateUnsteeredMix ConditioningCandidate = "unsteered-mix"
	CandidateDelaySum     ConditioningCandidate = "echolocal-delay-sum"
)

// ConditioningMetrics describes output from a bounded conditioning processor.
// The values are cumulative until ResetMetrics and are intended for logs and
// comparison diagnostics, not control decisions.
type ConditioningMetrics struct {
	Profile            string        `json:"profile"`
	AppliedGainDB      float64       `json:"applied_gain_db"`
	PeakDBFS           float64       `json:"peak_dbfs"`
	RMSDBFS            float64       `json:"rms_dbfs"`
	ClippingCount      uint64        `json:"clipping_count"`
	ClippingFraction   float64       `json:"clipping_fraction"`
	NoiseLevelDBFS     float64       `json:"noise_level_dbfs"`
	SpeechNoiseDB      float64       `json:"speech_noise_separation_db"`
	ProcessingDuration time.Duration `json:"processing_duration_ns"`
	MaxBlockDuration   time.Duration `json:"max_block_duration_ns"`
}

// Conditioning is a complete candidate: a seven-microphone reducer followed
// by one bounded output leveler. It is not safe for concurrent Process calls.
type Conditioning struct {
	mixer          MultichannelPreprocessor
	leveler        *Leveler
	duration       time.Duration
	maxBlock       time.Duration
	reducedSquare  float64
	reducedSamples uint64
}

// NewConditioning builds a candidate with fixed physical-channel polarity.
// The qualified Task 9 scorecard found mic0 through mic6 to have normal
// polarity; loopback channels are excluded by the MultichannelPreprocessor
// contract and Capturer's exact channel-map check.
func NewConditioning(candidate ConditioningCandidate) (*Conditioning, error) {
	var mixer MultichannelPreprocessor
	switch candidate {
	case CandidateChannel0:
		mixer = Channel0{}
	case CandidateUnsteeredMix:
		mixer = UnsteeredMix{}
	case CandidateDelaySum:
		mixer = NewEchoLocalDelaySum()
	default:
		return nil, ErrUnknownConditioningCandidate
	}
	return &Conditioning{mixer: mixer, leveler: NewLeveler()}, nil
}

// Process applies output leveling after multichannel reduction.
func (c *Conditioning) Process(in []int16) []int16 {
	started := time.Now()
	out := c.leveler.Process(in)
	c.duration += time.Since(started)
	return out
}

// ProcessChannels reduces exactly the physical microphone array.
func (c *Conditioning) ProcessChannels(mics [][]int16) []int16 {
	started := time.Now()
	out := c.mixer.ProcessChannels(mics)
	c.duration += time.Since(started)
	c.recordReduced(out)
	return out
}

// ProcessBlock applies one canonical 80 ms (or shorter final) capture block
// and records its end-to-end processing time for qualification diagnostics.
func (c *Conditioning) ProcessBlock(mics [][]int16) ([]int16, []int16) {
	started := time.Now()
	reduced := c.mixer.ProcessChannels(mics)
	c.recordReduced(reduced)
	out := c.leveler.Process(reduced)
	duration := time.Since(started)
	c.duration += duration
	c.maxBlock = max(c.maxBlock, duration)
	return out, reduced
}

// Name identifies the candidate currently being qualified.
func (c *Conditioning) Name() string { return c.mixer.Name() }

// Metrics returns output metrics. Noise is an optional matched capture's RMS.
func (c *Conditioning) Metrics(noise []int16) ConditioningMetrics {
	metrics := c.leveler.Metrics()
	metrics.Profile = c.Name()
	metrics.ProcessingDuration = c.duration
	metrics.MaxBlockDuration = c.maxBlock
	if len(noise) > 0 {
		metrics.NoiseLevelDBFS = samplesDBFS(noise)
		if c.reducedSamples > 0 && metrics.NoiseLevelDBFS > -120 {
			speech := amplitudeDBFS(math.Sqrt(c.reducedSquare / float64(c.reducedSamples)))
			if speech > -120 {
				metrics.SpeechNoiseDB = speech - metrics.NoiseLevelDBFS
			}
		}
	}
	return metrics
}

func (c *Conditioning) recordReduced(samples []int16) {
	for _, sample := range samples {
		value := float64(sample)
		c.reducedSquare += value * value
	}
	c.reducedSamples += uint64(len(samples))
}

// ErrUnknownConditioningCandidate prevents silently choosing a candidate.
var ErrUnknownConditioningCandidate = errors.New("unknown conditioning candidate")

// Channel0 selects physical microphone zero without mixing it with loopback.
type Channel0 struct{}

func (Channel0) Process(in []int16) []int16 { return in }
func (Channel0) Name() string               { return string(CandidateChannel0) }
func (Channel0) ProcessChannels(mics [][]int16) []int16 {
	if !validPhysicalMicrophones(mics) {
		return nil
	}
	return append([]int16(nil), mics[0]...)
}

// UnsteeredMix averages the Task 9 polarity-qualified physical microphones.
type UnsteeredMix struct{}

func (UnsteeredMix) Process(in []int16) []int16 { return in }
func (UnsteeredMix) Name() string               { return string(CandidateUnsteeredMix) }
func (UnsteeredMix) ProcessChannels(mics [][]int16) []int16 {
	if !validPhysicalMicrophones(mics) {
		return nil
	}
	out := make([]int16, len(mics[0]))
	for sample := range out {
		var sum int32
		for mic := range mics {
			sum += int32(mics[mic][sample])
		}
		out[sample] = int16(sum / PhysicalMicrophones)
	}
	return out
}

// Leveler applies bounded block gain with a fast attenuation attack and a slow
// gain recovery. Its one-dBFS peak ceiling is enforced before conversion.
type Leveler struct {
	gain      float64
	peak      float64
	sumSquare float64
	samples   uint64
	clipped   uint64
	duration  time.Duration
}

func NewLeveler() *Leveler { return &Leveler{gain: 1} }

func (l *Leveler) Process(in []int16) []int16 {
	started := time.Now()
	out := make([]int16, len(in))
	peak, rms := peakAndRMS(in)
	target := desiredGain(peak, rms)
	l.gain = transitionGain(l.gain, target, len(in))
	// A sudden loud block cannot wait for the smoothing attack: preserve the
	// headroom ceiling even while the normal gain trajectory is recovering.
	if peak > 0 {
		ceiling := math.Pow(10, -conditioningHeadroom/20) * math.MaxInt16 / peak
		l.gain = min(l.gain, ceiling)
	}
	for i, sample := range in {
		value := float64(sample) * l.gain
		if value > math.MaxInt16 || value < math.MinInt16 {
			l.clipped++
		}
		out[i] = clampPCM(float32(value))
		absolute := math.Abs(float64(out[i]))
		l.peak = max(l.peak, absolute)
		l.sumSquare += absolute * absolute
	}
	l.samples += uint64(len(out))
	l.duration += time.Since(started)
	return out
}

func (l *Leveler) Metrics() ConditioningMetrics {
	rms := 0.0
	if l.samples > 0 {
		rms = math.Sqrt(l.sumSquare / float64(l.samples))
	}
	fraction := 0.0
	if l.samples > 0 {
		fraction = float64(l.clipped) / float64(l.samples)
	}
	return ConditioningMetrics{AppliedGainDB: 20 * math.Log10(l.gain), PeakDBFS: amplitudeDBFS(l.peak), RMSDBFS: amplitudeDBFS(rms), ClippingCount: l.clipped, ClippingFraction: fraction, ProcessingDuration: l.duration}
}

func desiredGain(peak, rms float64) float64 {
	if peak == 0 || rms == 0 {
		return math.Pow(10, MaxConditioningGainDB/20)
	}
	target := math.Pow(10, conditioningTargetDB/20) * math.MaxInt16 / rms
	ceiling := math.Pow(10, -conditioningHeadroom/20) * math.MaxInt16 / peak
	return min(math.Pow(10, MaxConditioningGainDB/20), target, ceiling)
}

func transitionGain(current, target float64, samples int) float64 {
	period := time.Duration(samples) * time.Second / CanonicalSampleRate
	constant := conditioningRelease
	if target < current {
		constant = conditioningAttack
	}
	alpha := 1 - math.Exp(-float64(period)/float64(constant))
	return current + alpha*(target-current)
}

func peakAndRMS(samples []int16) (float64, float64) {
	var peak, sum float64
	for _, sample := range samples {
		value := math.Abs(float64(sample))
		peak = max(peak, value)
		sum += value * value
	}
	if len(samples) == 0 {
		return 0, 0
	}
	return peak, math.Sqrt(sum / float64(len(samples)))
}

func samplesDBFS(samples []int16) float64 {
	_, rms := peakAndRMS(samples)
	return amplitudeDBFS(rms)
}

func amplitudeDBFS(value float64) float64 {
	if value == 0 {
		// JSON diagnostics cannot represent -Inf; this is an explicit reporting
		// floor, not a claim that digital silence contains measurable energy.
		return -120
	}
	return 20 * math.Log10(value/math.MaxInt16)
}
