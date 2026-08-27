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
	device *Device
	ticks  <-chan time.Time
	mu     sync.RWMutex
	state  protocol.DeviceState
}

// NewAnimator creates an animator with an injectable tick channel.
func NewAnimator(device *Device, ticks <-chan time.Time) *Animator {
	return &Animator{device: device, ticks: ticks, state: protocol.StateIdle}
}

// Set changes the semantic state rendered by subsequent ticks.
func (a *Animator) Set(state protocol.DeviceState) {
	a.mu.Lock()
	a.state = state
	a.mu.Unlock()
}

// Run renders until the context is canceled or the tick channel closes.
func (a *Animator) Run(ctx context.Context) error {
	for tick := 0; ; tick++ {
		a.mu.RLock()
		state := a.state
		a.mu.RUnlock()
		if err := a.device.WriteFrame(Render(state, tick)); err != nil {
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
