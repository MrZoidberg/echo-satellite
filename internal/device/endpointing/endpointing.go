// Package endpointing implements device-local command endpoint detection.
package endpointing

import (
	"errors"
	"fmt"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake/vadlevel"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

var ErrActiveTurn = errors.New("endpointing: turn already active")

// Detector is the small consumer-owned interface needed by the state machine.
type Detector interface {
	Observe([]int16)
	SpeechScore() float64
}

type State uint8

const (
	Idle State = iota
	WaitingForSpeech
	InSpeech
	Completed
)

// Controller continuously warms its detector. A configuration is copied at
// Start, so config delivery cannot alter an active voice turn.
type Controller struct {
	detector                Detector
	config                  protocol.EndpointingConfig
	pending                 *protocol.EndpointingConfig
	state                   State
	turn                    protocol.EndpointingConfig
	elapsed, onset, silence time.Duration
}

func New(config protocol.EndpointingConfig, detector Detector) (*Controller, error) {
	if detector == nil {
		return nil, errors.New("endpointing: detector is required")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate endpointing config: %w", err)
	}
	return &Controller{detector: detector, config: config, state: Idle}, nil
}

// NewDefault creates a controller backed by the permanently warmed level VAD.
func NewDefault(config protocol.EndpointingConfig) (*Controller, error) {
	return New(config, vadlevel.NewDetector())
}

func (c *Controller) State() State { return c.state }
func (c *Controller) Idle() bool   { return c.state == Idle || c.state == Completed }

// StageConfig applies a revision at the idle boundary and otherwise holds it
// for the next turn. It returns true when the revision is pending.
func (c *Controller) StageConfig(config protocol.EndpointingConfig) (bool, error) {
	if err := config.Validate(); err != nil {
		return false, fmt.Errorf("validate endpointing config: %w", err)
	}
	if c.Idle() {
		c.config = config
		c.pending = nil
		return false, nil
	}
	c.pending = &config
	return true, nil
}

// Start snapshots the current config. Pre-roll is excluded from speech
// decisions but included in elapsed transmitted audio for the hard turn limit.
func (c *Controller) Start(preRollSamples int) error {
	if !c.Idle() {
		return ErrActiveTurn
	}
	if preRollSamples < 0 {
		return errors.New("endpointing: pre-roll sample count cannot be negative")
	}
	c.state, c.turn = WaitingForSpeech, c.config
	c.elapsed = time.Duration(preRollSamples) * time.Second / 16_000
	c.onset, c.silence = 0, 0
	return nil
}

// Observe warms the detector whether or not a turn is active and returns a
// stop reason only on the frame that completes the current turn.
func (c *Controller) Observe(samples []int16) (protocol.AudioStopReason, bool) {
	c.detector.Observe(samples)
	if c.state != WaitingForSpeech && c.state != InSpeech {
		return "", false
	}
	duration := time.Duration(len(samples)) * time.Second / 16_000
	c.elapsed += duration
	if c.elapsed >= milliseconds(c.turn.MaxTurnMS) {
		return c.complete(protocol.AudioStopTimeout)
	}
	switch speech := c.detector.SpeechScore() >= c.turn.SpeechThreshold; {
	case speech:
		if c.state == WaitingForSpeech {
			c.onset += duration
			if c.onset >= milliseconds(c.turn.SpeechOnsetMS) {
				c.state, c.silence = InSpeech, 0
			}
		} else {
			c.silence = 0
		}
	case c.state == WaitingForSpeech:
		c.onset = 0
	default:
		c.silence += duration
		if c.silence >= milliseconds(c.turn.TrailingSilenceMS) {
			return c.complete(protocol.AudioStopEndpointed)
		}
	}
	if c.state == WaitingForSpeech && c.elapsed >= milliseconds(c.turn.NoSpeechTimeoutMS) {
		return c.complete(protocol.AudioStopNoSpeech)
	}
	return "", false
}

func (c *Controller) EOF() (protocol.AudioStopReason, bool) {
	if c.state != WaitingForSpeech && c.state != InSpeech {
		return "", false
	}
	return c.complete(protocol.AudioStopEOF)
}

// Cancel closes the active turn without inventing a wire-level endpoint reason.
func (c *Controller) Cancel() { c.finish() }

func (c *Controller) complete(reason protocol.AudioStopReason) (protocol.AudioStopReason, bool) {
	c.finish()
	return reason, true
}

func (c *Controller) finish() {
	if c.state == WaitingForSpeech || c.state == InSpeech {
		c.state = Completed
	}
	if c.pending != nil {
		c.config, c.pending = *c.pending, nil
	}
}

func milliseconds(value int) time.Duration { return time.Duration(value) * time.Millisecond }
