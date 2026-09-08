package led

import (
	"context"
	"fmt"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

// Service owns LED rendering, animation timing, and transitions. Consumers
// communicate only in semantic states and never write controller frames.
type Service struct {
	animator *Animator
	ticker   *time.Ticker
	device   *Device
}

// NewService creates the project LED service with its 50 ms rendering cadence.
func NewService(device *Device) *Service {
	ticker := time.NewTicker(50 * time.Millisecond)
	return &Service{animator: NewAnimator(device, ticker.C), ticker: ticker, device: device}
}

func (s *Service) Set(state protocol.DeviceState) { s.animator.Set(state) }
func (s *Service) Off()                           { s.animator.Off() }
func (s *Service) Run(ctx context.Context) error  { return s.animator.Run(ctx) }

// Close stops rendering and clears project-owned LEDs.
func (s *Service) Close() error {
	s.ticker.Stop()
	if err := s.device.Clear(); err != nil {
		return fmt.Errorf("clear LED service: %w", err)
	}
	return nil
}
