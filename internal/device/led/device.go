package led

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// DefaultRoot is the Dot's IS31FL3236 sysfs device directory.
const DefaultRoot = "/sys/bus/i2c/devices/0-003f"

// Device writes the LED controller's sysfs attributes. The controller keeps
// its last frame after the process exits, so callers must Clear or deliberately
// render a final state during shutdown.
type Device struct {
	root    string
	mu      sync.Mutex
	last    Frame
	hasLast bool
}

// New returns a device rooted at root.
func New(root string) *Device { return &Device{root: root} }

// WriteFrame writes frame unless it is identical to the last successful write.
func (d *Device) WriteFrame(frame Frame) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hasLast && d.last == frame {
		return nil
	}
	if err := d.write("frame", frame.EncodeHex()); err != nil {
		return err
	}
	d.last = frame
	d.hasLast = true
	return nil
}

// SetCurrent sets the controller's global LED current.
func (d *Device) SetCurrent(current uint8) error {
	return d.write("led_current", strconv.FormatUint(uint64(current), 10))
}

// SetBootAnimation enables or disables the firmware boot animation.
func (d *Device) SetBootAnimation(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return d.write("boot_animation", value)
}

// Clear turns every segment off.
func (d *Device) Clear() error { return d.WriteFrame(Frame{}) }

func (d *Device) write(name, value string) error {
	path := filepath.Join(d.root, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return fmt.Errorf("write LED %s: %w", name, err)
	}
	return nil
}
