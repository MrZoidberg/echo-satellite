package audio

import (
	"fmt"
	"io"
)

// PCMSink consumes complete interleaved PCM frames.
type PCMSink interface {
	WriteInterleaved(buf []byte) (frames int, err error)
	Format() Format
	Close() error
}

type WAVSink struct {
	format Format
	writer *WAVWriter
	decode []int16
}

func NewWAVSink(w io.WriteSeeker, format Format) (*WAVSink, error) {
	writer, err := NewWAVWriter(w, format)
	if err != nil {
		return nil, fmt.Errorf("create WAV sink: %w", err)
	}
	return &WAVSink{format: format, writer: writer}, nil
}

func (s *WAVSink) Format() Format { return s.format }

func (s *WAVSink) WriteInterleaved(buf []byte) (int, error) {
	bytesPerFrame := s.format.BytesPerFrame()
	if len(buf)%bytesPerFrame != 0 {
		return 0, fmt.Errorf("write WAV sink: %w: %d bytes", ErrShortBuffer, len(buf))
	}
	samples := len(buf) / 2
	if cap(s.decode) < samples {
		s.decode = make([]int16, samples)
	}
	s.decode = s.decode[:samples]
	if _, err := DecodeS16LE(s.decode, buf); err != nil {
		return 0, fmt.Errorf("write WAV sink: %w", err)
	}
	written, err := s.writer.Write(s.decode)
	if err != nil {
		return written / s.format.Channels, err
	}
	return written / s.format.Channels, nil
}

func (s *WAVSink) Close() error { return s.writer.Close() }

type NullSink struct{ format Format }

func NewNullSink(format Format) (*NullSink, error) {
	if err := format.Validate(); err != nil {
		return nil, fmt.Errorf("create null sink: %w", err)
	}
	return &NullSink{format: format}, nil
}

func (s *NullSink) Format() Format { return s.format }

func (s *NullSink) WriteInterleaved(buf []byte) (int, error) {
	bytesPerFrame := s.format.BytesPerFrame()
	if len(buf)%bytesPerFrame != 0 {
		return 0, fmt.Errorf("write null sink: %w: %d bytes", ErrShortBuffer, len(buf))
	}
	return len(buf) / bytesPerFrame, nil
}

func (*NullSink) Close() error { return nil }
