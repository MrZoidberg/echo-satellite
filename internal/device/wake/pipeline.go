package wake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
)

var (
	ErrInvalidPipeline = errors.New("wake: invalid pipeline")
	ErrInvalidScore    = errors.New("wake: score is outside [0,1]")
)

type FrameSource interface {
	Frames() <-chan audio.Frame
}

type Event struct {
	ModelID   string
	WakeScore float64
	VADScore  float64
	PreRoll   []int16
	At        time.Time
}

type Pipeline struct {
	Engines []Engine
	VAD     VAD
	Gate    Gate
	Ring    *audio.Ring
	Stats   *Stats
	Config  Config
	Now     func() time.Time
}

func (p *Pipeline) Run(ctx context.Context, source FrameSource, events chan<- Event) error {
	if err := p.validate(source, events); err != nil {
		return err
	}
	step := make([]int16, 0, StepSamples*2)
	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-source.Frames():
			if !ok {
				return nil
			}
			step = append(step, frame.Samples...)
			for len(step) >= StepSamples {
				p.Ring.Write(step[:StepSamples])
				if err := p.processStep(ctx, step[:StepSamples], events); err != nil {
					return err
				}
				copy(step, step[StepSamples:])
				step = step[:len(step)-StepSamples]
			}
		}
	}
}

func (p *Pipeline) validate(source FrameSource, events chan<- Event) error {
	if source == nil || events == nil || p.VAD == nil || p.Ring == nil || p.Stats == nil || len(p.Engines) == 0 {
		return ErrInvalidPipeline
	}
	for _, engine := range p.Engines {
		if engine == nil {
			return ErrInvalidPipeline
		}
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	return nil
}

func (p *Pipeline) processStep(ctx context.Context, step []int16, events chan<- Event) error {
	vadStarted := time.Now()
	vadScore, err := p.VAD.Score(step)
	vadElapsed := time.Since(vadStarted)
	if err != nil {
		return fmt.Errorf("score wake VAD: %w", err)
	}
	if !finite(vadScore) || vadScore < 0 || vadScore > 1 {
		return fmt.Errorf("%w: VAD returned %.4g", ErrInvalidScore, vadScore)
	}
	type scoredCandidate struct {
		engine      Engine
		wakeScore   float64
		wakeElapsed time.Duration
		measured    bool
		observedAt  time.Time
	}
	scored := make([]scoredCandidate, 0, len(p.Engines))
	for _, engine := range p.Engines {
		candidate := scoredCandidate{engine: engine, observedAt: p.Now()}
		if p.Config.AlwaysScoreWake || !p.Config.VAD.Enabled || vadScore >= p.Gate.Thresholds.VAD {
			candidate.measured = true
			wakeStarted := time.Now()
			candidate.wakeScore, err = engine.Score(step)
			candidate.wakeElapsed = time.Since(wakeStarted)
			if err != nil {
				return fmt.Errorf("score wake model %q: %w", engine.ID(), err)
			}
			if !finite(candidate.wakeScore) || candidate.wakeScore < 0 || candidate.wakeScore > 1 {
				return fmt.Errorf("%w: model %q returned %.4g", ErrInvalidScore, engine.ID(), candidate.wakeScore)
			}
		}
		scored = append(scored, candidate)
	}

	observation := Observation{VADScore: vadScore, VADElapsed: vadElapsed}
	accepted := make([]Event, 0, len(p.Engines))
	for _, candidate := range scored {
		decision := DecisionBelowWake
		if candidate.measured {
			decision = p.Gate.Decide(Candidate{
				ModelID: candidate.engine.ID(), WakeScore: candidate.wakeScore, VADScore: vadScore,
				VADEnabled: p.Config.VAD.Enabled, At: candidate.observedAt,
			})
		}
		observation.Candidates = append(observation.Candidates, CandidateObservation{
			WakeScore: candidate.wakeScore, Decision: decision,
			WakeElapsed: candidate.wakeElapsed, Measured: candidate.measured,
		})
		if decision == DecisionAccepted {
			accepted = append(accepted, Event{
				ModelID: candidate.engine.ID(), WakeScore: candidate.wakeScore, VADScore: vadScore,
				PreRoll: p.Ring.Tail(time.Duration(p.Config.PreRollMS) * time.Millisecond), At: candidate.observedAt,
			})
		}
	}
	p.Stats.Observe(observation)
	for _, event := range accepted {
		select {
		case events <- event:
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}
