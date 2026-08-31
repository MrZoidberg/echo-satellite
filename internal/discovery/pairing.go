package discovery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrCorruptPairing is returned when persisted paired-gateway state cannot be
// decoded or fails validation. The original file is retained for diagnosis.
var (
	// ErrCorruptPairing indicates persisted state is unusable and was retained.
	ErrCorruptPairing = errors.New("discovery: corrupt paired-gateway state")
	// ErrNoPairing indicates no authenticated gateway has been persisted yet.
	ErrNoPairing = errors.New("discovery: no paired gateway")
)

// PairingStore persists the last gateway that completed authenticated welcome.
// It intentionally stores endpoint metadata only: credentials belong to the
// transport's credential store and must never enter this file.
type PairingStore struct{ Path string }

// Load returns ErrNoPairing when no paired gateway has been persisted.
func (s PairingStore) Load() (*Instance, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoPairing
	}
	if err != nil {
		return nil, fmt.Errorf("read paired-gateway state: %w", err)
	}
	var inst Instance
	if err := unmarshalPairing(data, &inst); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptPairing, err)
	}
	return &inst, nil
}

// Save atomically replaces endpoint metadata after a valid authenticated
// welcome. Callers must not call it for an unauthenticated discovery result.
func (s PairingStore) Save(inst Instance) error {
	if err := inst.Validate(); err != nil {
		return fmt.Errorf("validate paired gateway: %w", err)
	}
	data, err := marshalPairing(inst)
	if err != nil {
		return fmt.Errorf("marshal paired-gateway state: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(s.Path), 0o750); err != nil {
		return fmt.Errorf("create paired-gateway state directory: %w", err)
	}
	staged := s.Path + ".part"
	f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: path comes from the device composition root.
	if err != nil {
		return fmt.Errorf("open staged paired-gateway state: %w", err)
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write staged paired-gateway state: %w", err)
	}
	if err = os.Rename(staged, s.Path); err != nil {
		return fmt.Errorf("promote paired-gateway state: %w", err)
	}
	// Windows does not support opening/syncing directories. The file was
	// durably flushed before rename; Unix additionally flushes the directory
	// entry to make the rename durable across power loss.
	if runtime.GOOS != "windows" {
		dir, err := os.Open(filepath.Dir(s.Path))
		if err != nil {
			return fmt.Errorf("open paired-gateway state directory: %w", err)
		}
		if err := dir.Sync(); err != nil {
			_ = dir.Close()
			return fmt.Errorf("sync paired-gateway state directory: %w", err)
		}
		if err := dir.Close(); err != nil {
			return fmt.Errorf("close paired-gateway state directory: %w", err)
		}
	}
	return nil
}
