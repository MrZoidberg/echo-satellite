package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrFanoutRunning = errors.New("audio fanout is already running")

// Fanout is the DESIGN.md section 7.2 single-capture-path mechanism. The ALSA
// device is opened exactly once for the process lifetime. A Milestone 2 turn
// streamer must be added as an additional Subscribe call and must never open
// its own PCM device.
type Fanout struct {
	capturer      *Capturer
	mu            sync.Mutex
	running       bool
	subscriptions []*subscriptionState
}

type Subscription struct {
	Frames  <-chan Frame
	dropped *atomic.Uint64
}

type subscriptionState struct {
	name    string
	frames  chan Frame
	dropped atomic.Uint64
}

func NewFanout(capturer *Capturer) *Fanout { return &Fanout{capturer: capturer} }

func (f *Fanout) Subscribe(name string, depth int) (*Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running {
		return nil, ErrFanoutRunning
	}
	if name == "" {
		return nil, errors.New("subscribe to audio fanout: empty name")
	}
	if depth <= 0 {
		return nil, fmt.Errorf("subscribe to audio fanout: depth must be positive: %d", depth)
	}
	state := &subscriptionState{name: name, frames: make(chan Frame, depth)}
	f.subscriptions = append(f.subscriptions, state)
	return &Subscription{Frames: state.frames, dropped: &state.dropped}, nil
}

func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

func (f *Fanout) Run(ctx context.Context) error {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return ErrFanoutRunning
	}
	if f.capturer == nil {
		f.mu.Unlock()
		return errors.New("run audio fanout: nil capturer")
	}
	f.running = true
	subscriptions := append([]*subscriptionState(nil), f.subscriptions...)
	f.mu.Unlock()
	defer func() {
		for _, subscription := range subscriptions {
			close(subscription.frames)
		}
	}()

	return f.capturer.Run(ctx, func(frame Frame) error {
		for _, subscription := range subscriptions {
			select {
			case subscription.frames <- frame:
			default:
				subscription.dropped.Add(1)
			}
		}
		return nil
	})
}
