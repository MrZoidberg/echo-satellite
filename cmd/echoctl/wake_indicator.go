package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const wakeIndicatorBlinkInterval = 200 * time.Millisecond

var wakeIndicatorSleep = time.Sleep
var runWakeStartSpeakerTest = speakerTest

var playWakeStartAudio = func(path string) error {
	return runWakeStartSpeakerTest(io.Discard, speakerTestCommand{
		In: path, Seconds: 60, Resampler: "sinc", Volume: 0.25,
		Card: 0, Device: 23,
	})
}

func startLiveWakeIndicator(input wakeInputOptions) (func() error, error) {
	if input.FromFile != "" {
		return func() error { return nil }, nil
	}
	device := led.New(input.LEDRoot)
	if err := device.SetBootAnimation(false); err != nil {
		return nil, fmt.Errorf("disable wake-test LED boot animation: %w", err)
	}
	if err := device.SetCurrent(0); err != nil {
		return nil, fmt.Errorf("set wake-test LED current: %w", err)
	}
	red := led.Render(protocol.StateMuted, 0)
	for range 2 {
		if err := device.WriteFrame(red); err != nil {
			return nil, fmt.Errorf("blink wake-test LED red: %w", err)
		}
		wakeIndicatorSleep(wakeIndicatorBlinkInterval)
		if err := device.Clear(); err != nil {
			return nil, fmt.Errorf("blink wake-test LED off: %w", err)
		}
		wakeIndicatorSleep(wakeIndicatorBlinkInterval)
	}
	if err := device.WriteFrame(red); err != nil {
		return nil, fmt.Errorf("hold wake-test LED red: %w", err)
	}
	if err := playWakeStartAudio(input.StartAudio); err != nil {
		return nil, errors.Join(fmt.Errorf("play wake-test start audio: %w", err), wrapOptional("clear wake-test LED", device.Clear()))
	}
	return func() error {
		if err := device.Clear(); err != nil {
			return fmt.Errorf("clear wake-test LED: %w", err)
		}
		return nil
	}, nil
}
