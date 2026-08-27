package buttons

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	DefaultInputDir    = "/dev/input"
	DefaultSysClassDir = "/sys/class/input"
	DeviceNameKeypad   = "mtk-kpd"
	DeviceNameGPIOKeys = "keys"
)

// Device identifies one measured button input node and the keys consumed from it.
type Device struct {
	Path string
	Name string
	Keys []Key
}

// ErrNoInputDevice means no event node matched the requested device names.
var ErrNoInputDevice = errors.New("no matching input device")

// FindDevices returns event nodes whose sysfs device name matches wantNames.
// An empty wantNames accepts every event node, which is useful for diagnostics
// before the device's actual input name has been measured.
func FindDevices(inputDir, sysClassDir string, wantNames []string) ([]string, error) {
	nodes, err := filepath.Glob(filepath.Join(inputDir, "event*"))
	if err != nil {
		return nil, fmt.Errorf("glob input devices: %w", err)
	}
	matches := make([]string, 0, len(nodes))
	for _, path := range nodes {
		namePath := filepath.Join(sysClassDir, filepath.Base(path), "device", "name")
		data, readErr := os.ReadFile(namePath) //nolint:gosec // The injected sysfs root and discovered event basename determine this diagnostic path.
		if readErr != nil {
			continue
		}
		name := strings.TrimSpace(string(data))
		if len(wantNames) == 0 || slices.ContainsFunc(wantNames, func(want string) bool {
			return strings.EqualFold(name, strings.TrimSpace(want))
		}) {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNoInputDevice, inputDir)
	}
	slices.Sort(matches)
	return matches, nil
}

// FindControlDevices returns the two measured Dot button nodes with per-node
// key filters. Volume Down is advertised by both nodes, but only the gpio-keys
// node provides the complete volume press/release stream.
func FindControlDevices(inputDir, sysClassDir string) ([]Device, error) {
	keypad, err := FindDevices(inputDir, sysClassDir, []string{DeviceNameKeypad})
	if err != nil {
		return nil, err
	}
	gpioKeys, err := FindDevices(inputDir, sysClassDir, []string{DeviceNameGPIOKeys})
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(keypad)+len(gpioKeys))
	for _, path := range keypad {
		devices = append(devices, Device{Path: path, Name: DeviceNameKeypad, Keys: []Key{KeyAction, KeyMute}})
	}
	for _, path := range gpioKeys {
		devices = append(devices, Device{Path: path, Name: DeviceNameGPIOKeys, Keys: []Key{KeyVolumeUp, KeyVolumeDown}})
	}
	return devices, nil
}
