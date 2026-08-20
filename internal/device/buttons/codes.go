// Package buttons decodes Echo Dot evdev button events and applies semantic
// tap, hold, and repeat behavior.
package buttons

import "fmt"

const evTypeKey uint16 = 1

// Key is a Linux input key code used by the Echo Dot controls.
type Key uint16

const (
	KeyMute       Key = 113
	KeyVolumeDown Key = 114
	KeyVolumeUp   Key = 115
	KeyAction     Key = 138
)

func (k Key) String() string {
	switch k {
	case KeyMute:
		return "mute"
	case KeyVolumeDown:
		return "volume-down"
	case KeyVolumeUp:
		return "volume-up"
	case KeyAction:
		return "action"
	default:
		return fmt.Sprintf("key-%d", k)
	}
}
