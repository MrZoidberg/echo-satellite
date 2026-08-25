package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/device/mixer"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const wakeOnlyIndicatorBlinkInterval = 200 * time.Millisecond

var wakeOnlyIndicatorSleep = time.Sleep
var playWakeOnlyStartAudio = playWakeOnlyAudio

func startWakeOnlyTestIndicator(ctx context.Context, o opts) (func() error, error) {
	if o.MicFromFile != "" {
		return func() error { return nil }, nil
	}
	device := led.New(o.LEDRoot)
	if err := device.SetBootAnimation(false); err != nil {
		return nil, fmt.Errorf("disable wake-only test LED boot animation: %w", err)
	}
	if err := device.SetCurrent(0); err != nil {
		return nil, fmt.Errorf("set wake-only test LED current: %w", err)
	}
	red := led.Render(protocol.StateMuted, 0)
	for range 2 {
		if err := device.WriteFrame(red); err != nil {
			return nil, fmt.Errorf("blink wake-only test LED red: %w", err)
		}
		wakeOnlyIndicatorSleep(wakeOnlyIndicatorBlinkInterval)
		if err := device.Clear(); err != nil {
			return nil, fmt.Errorf("blink wake-only test LED off: %w", err)
		}
		wakeOnlyIndicatorSleep(wakeOnlyIndicatorBlinkInterval)
	}
	if err := device.WriteFrame(red); err != nil {
		return nil, fmt.Errorf("hold wake-only test LED red: %w", err)
	}
	if err := playWakeOnlyStartAudio(ctx, o.TestStartAudio); err != nil {
		return nil, errors.Join(fmt.Errorf("play wake-only test start audio: %w", err), wrapWakeOnlyOptional("clear wake-only test LED", device.Clear()))
	}
	return func() error { return wrapWakeOnlyOptional("clear wake-only test LED", device.Clear()) }, nil
}

func playWakeOnlyAudio(ctx context.Context, path string) error {
	file, err := os.Open(path) //nolint:gosec // operator-selected local diagnostic cue.
	if err != nil {
		return fmt.Errorf("open start cue: %w", err)
	}
	format, samples, readErr := audio.ReadWAV(file)
	closeFileErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("read start cue: %w", readErr)
	}
	if closeFileErr != nil {
		return fmt.Errorf("close start cue: %w", closeFileErr)
	}
	want := audio.Format{SampleRate: audio.CanonicalSampleRate, Channels: 1, Layout: audio.LayoutS16LE}
	if format != want {
		return fmt.Errorf("start cue format %+v does not match %+v", format, want)
	}
	scaleWakeOnlyCue(samples)
	pcm, err := alsa.OpenPlayback(alsa.Config{Card: alsa.MicCard, Device: alsa.SpeakerDevice, Rate: alsa.SpeakerRate, Channels: alsa.SpeakerChannels, Format: alsa.SpeakerFormat, PeriodFrames: alsa.SpeakerPeriodFrames, Periods: alsa.SpeakerPeriods})
	if err != nil {
		return fmt.Errorf("open start-cue playback: %w", err)
	}
	control, err := mixer.Open(alsa.MicCard)
	if err != nil {
		_ = pcm.Close()
		return fmt.Errorf("open start-cue amplifier: %w", err)
	}
	prior, err := control.Get(mixer.ControlSpeakerAmp)
	if err == nil {
		err = control.Set(mixer.ControlSpeakerAmp, mixer.ValueOn)
	}
	if err != nil {
		_ = control.Close()
		_ = pcm.Close()
		return fmt.Errorf("enable start-cue amplifier: %w", err)
	}
	player, playErr := audio.NewPlayer(wakeOnlyPCMSink{pcm}, audio.NewSincResampler(audio.CanonicalSampleRate, alsa.SpeakerRate), alsa.SpeakerPeriodFrames)
	if playErr == nil {
		playErr = player.Play(ctx, samples)
	}
	return errors.Join(wrapWakeOnlyOptional("play start cue", playErr), wrapWakeOnlyOptional("restore start-cue amplifier", control.Set(mixer.ControlSpeakerAmp, prior)), wrapWakeOnlyOptional("close start-cue amplifier", control.Close()), wrapWakeOnlyOptional("close start-cue playback", pcm.Close()))
}

func scaleWakeOnlyCue(samples []int16) {
	for index := range samples {
		samples[index] /= 4
	}
}

type wakeOnlyPCMSink struct{ *alsa.PCM }

func (wakeOnlyPCMSink) Format() audio.Format {
	return audio.Format{SampleRate: alsa.SpeakerRate, Channels: alsa.SpeakerChannels, Layout: audio.LayoutS16LE}
}

func wrapWakeOnlyOptional(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
