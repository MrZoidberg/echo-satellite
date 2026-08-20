package alsa

import "errors"

var (
	ErrUnsupportedPlatform = errors.New("ALSA is unsupported on this platform")
	ErrDeviceBusy          = errors.New("ALSA device is busy")
	ErrXRun                = errors.New("ALSA stream overrun or underrun")
	ErrNotConfigured       = errors.New("ALSA stream is not configured")
)
