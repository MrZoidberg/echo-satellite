package mixer

import "errors"

var (
	ErrControlNotFound     = errors.New("ALSA mixer control not found")
	ErrUnsupportedPlatform = errors.New("ALSA mixer is unsupported on this platform")
)
