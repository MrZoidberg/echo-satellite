package system

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errInvalidPersistedID = errors.New("persisted device ID is blank")

// Identity contains the hardware serial when available and the stable fleet ID.
// DeviceID deliberately preserves a readable sanitized serial rather than hashing
// it: the serial is device identity, not a secret, and readability aids diagnostics.
type Identity struct {
	Serial   string
	DeviceID string
}

func Resolve(reader SerialReader, override, persistPath string) (Identity, error) {
	if override != "" {
		deviceID := sanitizeDeviceID(override)
		if !hasASCIIAlphanumeric(deviceID) {
			return Identity{}, errors.New("resolve device identity: override has no usable characters")
		}
		return Identity{DeviceID: deviceID}, nil
	}

	serial, err := reader.Read()
	if err == nil {
		deviceID := sanitizeDeviceID(serial)
		if !hasASCIIAlphanumeric(deviceID) {
			return Identity{}, errors.New("resolve device identity: serial has no usable characters")
		}
		return Identity{Serial: serial, DeviceID: deviceID}, nil
	}
	if !errors.Is(err, ErrNoSerial) {
		return Identity{}, fmt.Errorf("resolve device serial: %w", err)
	}

	persisted, err := readPersistedID(persistPath)
	if err == nil {
		return Identity{DeviceID: persisted}, nil
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errInvalidPersistedID) {
		return Identity{}, err
	}
	return generateAndPersistID(persistPath)
}

func sanitizeDeviceID(value string) string {
	var result strings.Builder
	separator := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			if separator {
				result.WriteByte('-')
			}
			result.WriteRune(char)
			separator = false
			continue
		}
		separator = true
	}
	return result.String()
}

func hasASCIIAlphanumeric(value string) bool {
	return strings.IndexFunc(value, func(char rune) bool {
		return char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
	}) >= 0
}

func readPersistedID(path string) (string, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // G304: the caller supplies the configured identity path by design.
	if err != nil {
		return "", fmt.Errorf("read persisted device ID: %w", err)
	}
	deviceID := sanitizeDeviceID(string(contents))
	if !hasASCIIAlphanumeric(deviceID) {
		return "", fmt.Errorf("read persisted device ID: %w", errInvalidPersistedID)
	}
	return deviceID, nil
}

func generateAndPersistID(path string) (Identity, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Identity{}, fmt.Errorf("generate device ID: %w", err)
	}
	deviceID := hex.EncodeToString(random[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Identity{}, fmt.Errorf("create device ID directory: %w", err)
	}
	directory := filepath.Dir(path)
	file, createErr := os.CreateTemp(directory, ".device-id-")
	if createErr != nil {
		return Identity{}, fmt.Errorf("create temporary device ID: %w", createErr)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return Identity{}, fmt.Errorf("set temporary device ID permissions: %w", err)
	}
	if _, err := file.WriteString(deviceID + "\n"); err != nil {
		_ = file.Close()
		return Identity{}, fmt.Errorf("write temporary device ID: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Identity{}, fmt.Errorf("sync temporary device ID: %w", err)
	}
	if err := file.Close(); err != nil {
		return Identity{}, fmt.Errorf("close temporary device ID: %w", err)
	}
	if err := publishDeviceID(temporaryPath, path); err != nil {
		return Identity{}, err
	}
	persisted, err := readPersistedID(path)
	if err != nil {
		return Identity{}, err
	}
	return Identity{DeviceID: persisted}, nil
}

func publishDeviceID(temporaryPath, path string) error {
	if err := os.Link(temporaryPath, path); err == nil {
		return syncDirectory(filepath.Dir(path))
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("publish persisted device ID: %w", err)
	}

	if _, err := readPersistedID(path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove invalid persisted device ID: %w", err)
	}
	if err := os.Link(temporaryPath, path); errors.Is(err, os.ErrExist) {
		if _, readErr := readPersistedID(path); readErr != nil {
			return readErr
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("replace invalid persisted device ID: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // G304: path is the configured identity file's already-created parent directory.
	if err != nil {
		return fmt.Errorf("open device ID directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync device ID directory: %w", err)
	}
	return nil
}
