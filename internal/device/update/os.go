package update

import (
	"fmt"
	"io/fs"
	"os"
)

// OSFileSystem is the production filesystem adapter.
type OSFileSystem struct{}

func (OSFileSystem) CreateTemp(dir, pattern string, perm fs.FileMode) (File, string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, "", fmt.Errorf("create temporary file: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, "", fmt.Errorf("set temporary file permissions: %w", err)
	}
	name := f.Name()
	return f, name, nil
}
func (OSFileSystem) Open(path string) (File, error) {
	f, err := os.Open(path) //nolint:gosec // installer selects its own configured staging path.
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}
func (OSFileSystem) ReadFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // installer selects its own configured metadata path.
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return raw, nil
}
func (OSFileSystem) Rename(oldpath, newpath string) error {
	if err := os.Rename(oldpath, newpath); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}
	return nil
}
func (OSFileSystem) Remove(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove file: %w", err)
	}
	return nil
}
func (OSFileSystem) Chmod(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("change file permissions: %w", err)
	}
	return nil
}
func (OSFileSystem) SyncDir(path string) error {
	f, err := os.Open(path) //nolint:gosec // installer selects its own configured containing directory.
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
