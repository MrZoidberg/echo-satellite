package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PCMSource produces complete interleaved PCM frames.
type PCMSource interface {
	ReadInterleaved(buf []byte) (frames int, err error)
	Format() Format
	Close() error
}

// FileSource replays a raw device-shaped PCM file or a 16-bit PCM WAV file.
// Pace makes reads track the recording's real-time duration.
type FileSource struct {
	format  Format
	reader  io.Reader
	closer  io.Closer
	Pace    bool
	started time.Time
	frames  int64
	mu      sync.Mutex
}

// NewFileSource opens path. WAV files obtain their format from the header;
// raw files use rawFormat.
func NewFileSource(path string, rawFormat Format, pace bool) (*FileSource, error) {
	file, err := os.Open(path) //nolint:gosec // G304: the diagnostic caller deliberately selects the fixture path.
	if err != nil {
		return nil, fmt.Errorf("open PCM source %q: %w", path, err)
	}
	if filepath.Ext(path) != ".wav" {
		if validateErr := rawFormat.Validate(); validateErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("open raw PCM source: %w", validateErr)
		}
		return &FileSource{format: rawFormat, reader: file, closer: file, Pace: pace}, nil
	}

	format, samples, err := ReadWAV(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open WAV PCM source: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close WAV PCM source after loading: %w", err)
	}
	encoded := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(encoded[i*2:], uint16(sample)) //nolint:gosec // G115: conversion preserves the PCM bit pattern.
	}
	return &FileSource{format: format, reader: bytes.NewReader(encoded), Pace: pace}, nil
}

func (s *FileSource) Format() Format { return s.format }

func (s *FileSource) ReadInterleaved(buf []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	frameBytes := s.format.BytesPerFrame()
	if frameBytes == 0 || len(buf) == 0 || len(buf)%frameBytes != 0 {
		return 0, fmt.Errorf("read file source requires complete frames of %d bytes", frameBytes)
	}
	n, err := io.ReadFull(s.reader, buf)
	if err == io.ErrUnexpectedEOF && n > 0 {
		err = nil
	}
	frames := n / frameBytes
	if n%frameBytes != 0 {
		return 0, fmt.Errorf("read file source: %w: trailing %d bytes", ErrShortBuffer, n%frameBytes)
	}
	if s.Pace && frames > 0 {
		if s.started.IsZero() {
			s.started = time.Now()
		}
		s.frames += int64(frames)
		deadline := s.started.Add(time.Duration(s.frames) * time.Second / time.Duration(s.format.SampleRate))
		if delay := time.Until(deadline); delay > 0 {
			time.Sleep(delay)
		}
	}
	return frames, err
}

func (s *FileSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closer == nil {
		return nil
	}
	err := s.closer.Close()
	s.closer = nil
	if err != nil {
		return fmt.Errorf("close file source: %w", err)
	}
	return nil
}
