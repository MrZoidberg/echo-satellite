package vadlevel

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import (
	"math"
	"time"
)

const (
	sampleRate = 16_000
	fullScale  = 32_768.0

	targetDBFS  = -23.0
	maxGainDB   = 27.0
	peakDBFS    = -3.0
	startGainDB = 20.0

	// An absolute threshold cannot be right because the room and analog gain
	// are unknown. How far speech stands above the room is comparatively stable.
	speechOverFloorDB     = 12.0
	fullSpeechOverFloorDB = 30.0

	// The floor falls quickly because pauses are the best observation of the
	// room, and rises slowly so continuous speech cannot become the new floor.
	floorFall = 200 * time.Millisecond
	floorRise = 60 * time.Second
	gainFall  = 500 * time.Millisecond
	gainRise  = 5 * time.Second
)

// Detector tracks a room-relative speech score and the gain an AGC would apply.
// It is clock-free: adaptation is derived only from the number of samples seen.
type Detector struct {
	floor float64
	rms   float64
	gain  float64
}

func NewDetector() *Detector {
	d := &Detector{}
	d.Reset()
	return d
}

func (d *Detector) Observe(frame []int16) {
	if len(frame) == 0 {
		return
	}

	var sum float64
	var peak float64
	for _, sample := range frame {
		value := float64(sample)
		sum += value * value
		peak = max(peak, math.Abs(value))
	}
	d.rms = math.Sqrt(sum / float64(len(frame)))
	duration := time.Duration(float64(time.Second) * float64(len(frame)) / sampleRate)
	d.floor = follow(d.floor, d.rms, coefficient(duration, floorFall), coefficient(duration, floorRise), 1)

	if d.rms > d.floor*dbToRatio(speechOverFloorDB) {
		wanted := min(max(dbToAmplitude(targetDBFS)/d.rms, 1), dbToRatio(maxGainDB))
		step := coefficient(duration, gainRise)
		if wanted < d.gain {
			step = coefficient(duration, gainFall)
		}
		d.gain += (wanted - d.gain) * step
	}
	if peak > 0 {
		d.gain = min(d.gain, dbToAmplitude(peakDBFS)/peak)
	}
}

func (d *Detector) SpeechScore() float64 {
	if d.rms <= d.floor {
		return 0
	}
	overFloorDB := 20 * math.Log10(d.rms/d.floor)
	x := min(max((overFloorDB-speechOverFloorDB)/(fullSpeechOverFloorDB-speechOverFloorDB), 0), 1)
	return x * x * x * (x*(6*x-15) + 10)
}

func (d *Detector) GainDB() float64 { return 20 * math.Log10(d.gain) }

func (d *Detector) Reset() {
	d.floor = fullScale
	d.rms = 0
	d.gain = dbToRatio(startGainDB)
}

func coefficient(frame, period time.Duration) float64 {
	return 1 - math.Exp(-frame.Seconds()/period.Seconds())
}

func follow(floor, rms, down, up, least float64) float64 {
	step := up
	if rms < floor {
		step = down
	}
	return max(floor+(rms-floor)*step, least)
}

func dbToAmplitude(db float64) float64 { return dbToRatio(db) * fullScale }

func dbToRatio(db float64) float64 { return math.Pow(10, db/20) }
