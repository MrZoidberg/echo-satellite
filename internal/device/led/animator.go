package led

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

// Animator renders the current semantic state once initially and on each tick.
type Animator struct {
	device    *Device
	ticks     <-chan time.Time
	mu        sync.RWMutex
	state     protocol.DeviceState
	off       bool
	changed   bool
	last      Frame
	from      Frame
	target    Frame
	steps     int
	stateTick int
}

const transitionFrames = 6 // 6 × the service's 50 ms cadence = 300 ms.

// NewAnimator creates an animator with an injectable tick channel.
func NewAnimator(device *Device, ticks <-chan time.Time) *Animator {
	return &Animator{device: device, ticks: ticks, state: protocol.StateIdle, changed: true}
}

// Set changes the semantic state rendered by subsequent ticks.
func (a *Animator) Set(state protocol.DeviceState) {
	a.mu.Lock()
	a.state = state
	a.off = false
	a.changed = true
	a.mu.Unlock()
}

// Off clears the ring on the next animation tick and keeps it clear until a
// later Set call requests a semantic state.
func (a *Animator) Off() {
	a.mu.Lock()
	a.off = true
	a.changed = true
	a.mu.Unlock()
}

// Run renders until the context is canceled or the tick channel closes.
func (a *Animator) Run(ctx context.Context) error {
	for {
		a.mu.Lock()
		state := a.state
		off := a.off
		if a.changed {
			a.from = a.last
			a.target = Frame{}
			if !off {
				a.target = Render(state, 0)
			}
			a.steps, a.stateTick, a.changed = transitionFrames, 0, false
		}
		frame := a.target
		if a.steps > 0 {
			progress := float64(transitionFrames-a.steps+1) / float64(transitionFrames)
			frame = blendFrames(a.from, a.target, progress)
			a.steps--
		} else if !off {
			frame = Render(state, a.stateTick)
			a.stateTick++
		}
		a.last = frame
		a.mu.Unlock()
		if err := a.device.WriteFrame(frame); err != nil {
			return fmt.Errorf("render LED state %s: %w", state, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-a.ticks:
			if !ok {
				return nil
			}
		}
	}
}
