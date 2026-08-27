package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNoSerial = errors.New("device serial is unavailable")

type SerialReader struct {
	Root string
}

func (r SerialReader) Read() (string, error) {
	readers := []func() (string, error){
		func() (string, error) { return r.readTrimmed("/sys/devices/soc0/serial_number") },
		r.readCmdline,
		func() (string, error) { return r.readTrimmed("/proc/device-tree/serial-number") },
	}
	for _, read := range readers {
		serial, err := read()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		if serial != "" {
			return serial, nil
		}
	}
	return "", ErrNoSerial
}

func (r SerialReader) readCmdline() (string, error) {
	contents, err := os.ReadFile(r.path("/proc/cmdline"))
	if err != nil {
		return "", fmt.Errorf("read kernel command line: %w", err)
	}
	for field := range strings.FieldsSeq(string(contents)) {
		if serial, found := strings.CutPrefix(field, "androidboot.serialno="); found {
			return cleanSerial(serial), nil
		}
	}
	return "", nil
}

func (r SerialReader) readTrimmed(name string) (string, error) {
	contents, err := os.ReadFile(r.path(name))
	if err != nil {
		return "", fmt.Errorf("read serial source %s: %w", name, err)
	}
	return cleanSerial(string(contents)), nil
}

func (r SerialReader) path(name string) string {
	root := r.Root
	if root == "" {
		root = string(filepath.Separator)
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
}

func cleanSerial(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\x00")
}
