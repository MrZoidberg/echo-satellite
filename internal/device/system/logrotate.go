package system

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type RotatingWriter struct {
	path      string
	fileCount int
	fileBytes int64
	file      *os.File
	current   int64
	closed    bool
}

func NewRotatingWriter(path string, totalBytes int64, fileCount int) (*RotatingWriter, error) {
	if totalBytes <= 0 || fileCount <= 0 || int64(fileCount) > totalBytes {
		return nil, errors.New("create rotating writer: limits must allow at least one byte per file")
	}
	w := &RotatingWriter{path: path, fileCount: fileCount, fileBytes: totalBytes / int64(fileCount)}
	if err := w.enforceExistingLimit(); err != nil {
		return nil, err
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) enforceExistingLimit() error {
	if err := w.removeStaleGenerations(); err != nil {
		return err
	}
	for index := range w.fileCount {
		path := rotatedPath(w.path, index)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat existing rotating log %d: %w", index, err)
		}
		if info.Size() <= w.fileBytes {
			continue
		}
		if err := os.Truncate(path, w.fileBytes); err != nil {
			return fmt.Errorf("bound existing rotating log %d: %w", index, err)
		}
	}
	return nil
}

func (w *RotatingWriter) removeStaleGenerations() error {
	directory := filepath.Dir(w.path)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list rotating log directory: %w", err)
	}
	prefix := filepath.Base(w.path) + "."
	for _, entry := range entries {
		suffix, found := strings.CutPrefix(entry.Name(), prefix)
		if !found {
			continue
		}
		index, parseErr := strconv.Atoi(suffix)
		if parseErr != nil || index < w.fileCount {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale rotating log %d: %w", index, err)
		}
	}
	return nil
}

func (w *RotatingWriter) Write(payload []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write rotating log: writer is closed")
	}
	written := 0
	for written < len(payload) {
		if w.current == w.fileBytes {
			if err := w.rotate(); err != nil {
				return written, err
			}
		}
		remaining := w.fileBytes - w.current
		chunk := min(int64(len(payload)-written), remaining)
		n, err := w.file.Write(payload[written : written+int(chunk)])
		written += n
		w.current += int64(n)
		if err != nil {
			return written, fmt.Errorf("write rotating log: %w", err)
		}
		if int64(n) != chunk {
			return written, fmt.Errorf("write rotating log: %w", io.ErrShortWrite)
		}
	}
	return written, nil
}

func (w *RotatingWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close rotating log: %w", err)
	}
	return nil
}

func (w *RotatingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open rotating log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat rotating log: %w", err)
	}
	w.file = file
	w.current = info.Size()
	if w.current > w.fileBytes {
		return w.rotate()
	}
	return nil
}

func (w *RotatingWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close rotating log before rotation: %w", err)
		}
	}
	if err := os.Remove(rotatedPath(w.path, w.fileCount-1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove oldest rotating log: %w", err)
	}
	for index := w.fileCount - 2; index >= 0; index-- {
		if err := os.Rename(rotatedPath(w.path, index), rotatedPath(w.path, index+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate log file %d: %w", index, err)
		}
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open new rotating log: %w", err)
	}
	w.file = file
	w.current = 0
	return nil
}

func rotatedPath(path string, index int) string {
	if index == 0 {
		return path
	}
	return path + "." + strconv.Itoa(index)
}
