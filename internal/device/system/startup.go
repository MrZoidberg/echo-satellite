package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServiceController owns the minimal FireOS service interaction needed before
// echod claims the indicator and mDNS resources.
type ServiceController interface {
	Stop(string) error
}

// StartupLED is the portion of the LED device needed during preparation.
type StartupLED interface {
	SetBootAnimation(bool) error
	Clear() error
}

// StartupAnimation announces that project ownership has begun.
type StartupAnimation interface {
	Start() error
}

// StartupAnimationFunc adapts a callback to StartupAnimation.
type StartupAnimationFunc func() error

func (f StartupAnimationFunc) Start() error { return f() }

// StartupPreparer claims device resources in the order required by the Dot.
// It never changes the physical microphone-cut value; cleanup restores the
// value observed before setup began.
type StartupPreparer struct {
	Services  ServiceController
	LED       StartupLED
	Animation StartupAnimation
	GPIORoot  string
}

// Prepare returns a cleanup function only after all setup actions succeeded.
func (p StartupPreparer) Prepare() (cleanup func() error, returnErr error) {
	if p.Services == nil || p.LED == nil || p.Animation == nil {
		return nil, errors.New("prepare device startup: services, LED, and animation are required")
	}
	value, err := p.ensureMuteGPIO()
	if err != nil {
		return nil, fmt.Errorf("snapshot microphone-cut GPIO 444: %w", err)
	}
	value = []byte(strings.TrimSpace(string(value)))
	if string(value) != "0" && string(value) != "1" {
		return nil, fmt.Errorf("snapshot microphone-cut GPIO 444: invalid value %q", value)
	}
	cleanupFn := func() error {
		clearErr := p.LED.Clear()
		writeErr := os.WriteFile(filepath.Join(p.GPIORoot, "gpio444", "value"), append(value, '\n'), 0o600)
		return errors.Join(wrapCleanup("clear project LEDs", clearErr), wrapCleanup("restore microphone-cut GPIO 444", writeErr))
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, cleanupFn())
		}
	}()
	if err := p.Services.Stop("ledcontroller"); err != nil {
		return nil, fmt.Errorf("stop ledcontroller: %w", err)
	}
	if err := p.Services.Stop("mdnsd"); err != nil {
		return nil, fmt.Errorf("stop mdnsd: %w", err)
	}
	if err := p.LED.SetBootAnimation(false); err != nil {
		return nil, fmt.Errorf("disable firmware LED boot animation: %w", err)
	}
	if err := p.LED.Clear(); err != nil {
		return nil, fmt.Errorf("clear project LEDs before startup: %w", err)
	}
	if err := p.Animation.Start(); err != nil {
		return nil, fmt.Errorf("start project LED animation: %w", err)
	}
	return cleanupFn, nil
}

func (p StartupPreparer) ensureMuteGPIO() ([]byte, error) {
	valuePath := filepath.Join(p.GPIORoot, "gpio444", "value")
	value, err := os.ReadFile(valuePath) //nolint:gosec // Path is constructed from the injected device GPIO root.
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read microphone-cut GPIO 444: %w", err)
	}
	writeErr := os.WriteFile(filepath.Join(p.GPIORoot, "export"), []byte("444\n"), 0o600)
	if writeErr != nil {
		return nil, fmt.Errorf("export GPIO 444: %w", writeErr)
	}
	writeErr = os.WriteFile(filepath.Join(p.GPIORoot, "gpio444", "direction"), []byte("out\n"), 0o600)
	if writeErr != nil {
		return nil, fmt.Errorf("set GPIO 444 direction: %w", writeErr)
	}
	writeErr = os.WriteFile(valuePath, []byte("0\n"), 0o600)
	if writeErr != nil {
		return nil, fmt.Errorf("enable microphone GPIO 444: %w", writeErr)
	}
	value, err = os.ReadFile(valuePath) //nolint:gosec // Path is constructed from the injected device GPIO root.
	if err != nil {
		return nil, fmt.Errorf("read exported microphone-cut GPIO 444: %w", err)
	}
	return value, nil
}

func wrapCleanup(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
