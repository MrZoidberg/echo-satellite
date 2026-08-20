package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/MrZoidberg/echo-satellite/internal/device/mixer"
)

type ampControl interface {
	Get(string) (string, error)
	Set(string, string) error
	Close() error
}

var openAmpControl = func(card int) (ampControl, error) { return mixer.Open(card) }
var playbackSinkOpener = openPlaybackSink

func speakerTest(w io.Writer, c speakerTestCommand) error {
	if c.Seconds <= 0 {
		return fmt.Errorf("seconds must be positive: %g", c.Seconds)
	}
	if c.Volume < 0 || c.Volume > 1 {
		return fmt.Errorf("volume must be between 0 and 1: %g", c.Volume)
	}
	samples, err := speakerInput(c)
	if err != nil {
		return err
	}
	for i := range samples {
		samples[i] = int16(math.Round(float64(samples[i]) * c.Volume))
	}
	sink, err := playbackSinkOpener(c)
	if err != nil {
		return err
	}
	sinkClosed := false
	defer func() {
		if !sinkClosed {
			_ = sink.Close()
		}
	}()
	restore, err := enableAmp(c)
	if err != nil {
		return err
	}
	defer func() { _ = restore() }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	player, err := audio.NewPlayer(sink, selectResampler(c.Resampler), alsa.SpeakerPeriodFrames)
	if err != nil {
		return fmt.Errorf("create speaker player: %w", err)
	}
	playErr := player.Play(ctx, samples)
	restoreErr := restore()
	closeErr := sink.Close()
	if closeErr == nil {
		sinkClosed = true
	}
	if combined := errors.Join(wrapOptional("play speaker test", playErr), restoreErr, wrapOptional("close speaker playback", closeErr)); combined != nil {
		return combined
	}
	return writeReport(w, []string{fmt.Sprintf("played: %d frames at %d Hz stereo", int(math.Ceil(float64(len(samples))*3)), alsa.SpeakerRate)})
}

func wrapOptional(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func speakerInput(c speakerTestCommand) ([]int16, error) {
	limit := int(math.Round(c.Seconds * audio.CanonicalSampleRate))
	if c.In == "" {
		out := make([]int16, limit)
		for i := range out {
			out[i] = int16(math.Round(8192 * math.Sin(2*math.Pi*1000*float64(i)/audio.CanonicalSampleRate)))
		}
		return out, nil
	}
	file, err := os.Open(c.In)
	if err != nil {
		return nil, fmt.Errorf("open speaker input: %w", err)
	}
	defer func() { _ = file.Close() }()
	format, samples, err := audio.ReadWAV(file)
	if err != nil {
		return nil, fmt.Errorf("read speaker WAV: %w", err)
	}
	if format.SampleRate != audio.CanonicalSampleRate || format.Channels != 1 {
		return nil, errors.New("speaker input must be 16 kHz mono PCM")
	}
	return samples[:min(len(samples), limit)], nil
}

func selectResampler(name string) audio.Resampler {
	switch name {
	case "linear":
		return audio.NewLinearResampler(audio.CanonicalSampleRate, alsa.SpeakerRate)
	case "hold":
		return audio.NewHoldResampler(audio.CanonicalSampleRate, alsa.SpeakerRate)
	default:
		return audio.NewSincResampler(audio.CanonicalSampleRate, alsa.SpeakerRate)
	}
}

func enableAmp(c speakerTestCommand) (func() error, error) {
	if c.NoAmp || c.ToFile != "" {
		return func() error { return nil }, nil
	}
	control, err := openAmpControl(c.Card)
	if err != nil {
		return nil, fmt.Errorf("open speaker amplifier: %w", err)
	}
	prior, err := control.Get(mixer.ControlSpeakerAmp)
	if err != nil {
		_ = control.Close()
		return nil, fmt.Errorf("read speaker amplifier: %w", err)
	}
	if err = control.Set(mixer.ControlSpeakerAmp, mixer.ValueOn); err != nil {
		_ = control.Close()
		return nil, fmt.Errorf("enable speaker amplifier: %w", err)
	}
	done := false
	return func() error {
		if done {
			return nil
		}
		if err := control.Set(mixer.ControlSpeakerAmp, prior); err != nil {
			return fmt.Errorf("restore speaker amplifier: %w", err)
		}
		done = true
		if err := control.Close(); err != nil {
			return fmt.Errorf("close speaker amplifier: %w", err)
		}
		return nil
	}, nil
}
