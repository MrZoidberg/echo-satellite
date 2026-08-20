package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const ledTickInterval = 100 * time.Millisecond

func ledTest(w io.Writer, command ledTestCommand) error {
	if command.Seconds <= 0 {
		return errors.New("seconds must be greater than zero")
	}
	states, err := selectedLEDStates(command)
	if err != nil {
		return err
	}
	if command.Root != led.DefaultRoot {
		if err = os.MkdirAll(command.Root, 0o750); err != nil {
			return fmt.Errorf("create LED test root: %w", err)
		}
	}
	device := led.New(command.Root)
	if currentErr := device.SetCurrent(command.Current); currentErr != nil {
		return fmt.Errorf("set LED current: %w", currentErr)
	}
	for _, state := range states {
		if renderErr := renderLEDState(device, state, time.Duration(command.Seconds*float64(time.Second))); renderErr != nil {
			return renderErr
		}
		if _, err = fmt.Fprintf(w, "state: %s\n", state); err != nil {
			return fmt.Errorf("write LED test report: %w", err)
		}
	}
	if command.Clear {
		if clearErr := device.Clear(); clearErr != nil {
			return fmt.Errorf("clear LED ring: %w", clearErr)
		}
	}
	return nil
}

func selectedLEDStates(command ledTestCommand) ([]protocol.DeviceState, error) {
	if command.AllStates {
		return protocol.AllDeviceStates(), nil
	}
	state := protocol.DeviceState(command.State)
	if slices.Contains(protocol.AllDeviceStates(), state) {
		return []protocol.DeviceState{state}, nil
	}
	return nil, fmt.Errorf("unknown LED state %q", command.State)
}

func renderLEDState(device *led.Device, state protocol.DeviceState, duration time.Duration) error {
	ticker := time.NewTicker(ledTickInterval)
	defer ticker.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	animator := led.NewAnimator(device, ticker.C)
	animator.Set(state)
	if err := animator.Run(ctx); err != nil {
		return fmt.Errorf("animate LED state %s: %w", state, err)
	}
	return nil
}
