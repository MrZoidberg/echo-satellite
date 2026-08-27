//go:build !linux

package mixer

// Mixer is unavailable on platforms without the Linux ALSA control API.
type Mixer struct{}

// Open opens the ALSA mixer for card. On non-Linux platforms it returns
// ErrUnsupportedPlatform.
func Open(card int) (*Mixer, error) {
	return nil, ErrUnsupportedPlatform
}

// Get reads a named mixer control. Callers that temporarily change a control
// must read its prior value first and restore that value when finished.
func (m *Mixer) Get(name string) (string, error) {
	return "", ErrUnsupportedPlatform
}

// Set writes a named mixer control. Callers that temporarily change a control
// must restore the value returned by Get, including on failure or interruption.
func (m *Mixer) Set(name, value string) error {
	return ErrUnsupportedPlatform
}

// Close closes the mixer.
func (m *Mixer) Close() error {
	return ErrUnsupportedPlatform
}
