package audio

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type Ring struct {
	mu         sync.Mutex
	samples    []int16
	sampleRate int
	start      int
	length     int
}

func NewRing(format Format, duration time.Duration) (*Ring, error) {
	if err := format.Validate(); err != nil {
		return nil, fmt.Errorf("create audio ring: %w", err)
	}
	if format.Layout != LayoutS16LE || format.Channels != 1 {
		return nil, errors.New("create audio ring: canonical mono s16le required")
	}
	if duration < 0 {
		return nil, fmt.Errorf("create audio ring: duration must not be negative: %s", duration)
	}
	capacity := int((int64(format.SampleRate) * int64(duration)) / int64(time.Second))
	return &Ring{samples: make([]int16, capacity), sampleRate: format.SampleRate}, nil
}

func (r *Ring) Write(samples []int16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.samples) == 0 || len(samples) == 0 {
		return
	}
	if len(samples) >= len(r.samples) {
		copy(r.samples, samples[len(samples)-len(r.samples):])
		r.start = 0
		r.length = len(r.samples)
		return
	}
	for _, sample := range samples {
		index := (r.start + r.length) % len(r.samples)
		if r.length == len(r.samples) {
			r.start = (r.start + 1) % len(r.samples)
		} else {
			r.length++
		}
		r.samples[index] = sample
	}
}

func (r *Ring) Tail(duration time.Duration) []int16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if duration <= 0 || r.length == 0 {
		return []int16{}
	}
	wanted := int((int64(r.sampleRate) * int64(duration)) / int64(time.Second))
	wanted = min(wanted, r.length)
	result := make([]int16, wanted)
	first := (r.start + r.length - wanted) % len(r.samples)
	for i := range wanted {
		result[i] = r.samples[(first+i)%len(r.samples)]
	}
	return result
}

func (r *Ring) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = 0
	r.length = 0
}

// EnsureDuration expands the ring when a new pre-roll setting requires more
// history. It never shrinks the allocation, preserving recent audio while a
// gateway configuration changes at runtime.
func (r *Ring) EnsureDuration(duration time.Duration) error {
	if duration < 0 {
		return fmt.Errorf("expand audio ring: duration must not be negative: %s", duration)
	}
	capacity := int((int64(r.sampleRate) * int64(duration)) / int64(time.Second))
	r.mu.Lock()
	defer r.mu.Unlock()
	if capacity <= len(r.samples) {
		return nil
	}
	next := make([]int16, capacity)
	if r.length > 0 {
		for index := range r.length {
			next[index] = r.samples[(r.start+index)%len(r.samples)]
		}
	}
	r.samples, r.start = next, 0
	return nil
}
