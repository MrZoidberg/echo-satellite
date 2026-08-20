package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

var (
	ErrNotRIFF        = errors.New("not a RIFF WAVE file")
	ErrUnsupportedWAV = errors.New("unsupported WAV format")
)

const maxWAVDataBytes = 256 << 20

const maxWAVFormatBytes = 64 << 10

func WriteWAV(w io.WriteSeeker, format Format, samples []int16) error {
	wav, err := NewWAVWriter(w, format)
	if err != nil {
		return err
	}
	if _, err := wav.Write(samples); err != nil {
		return err
	}
	return wav.Close()
}

func ReadWAV(r io.Reader) (Format, []int16, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Format{}, nil, fmt.Errorf("read WAV header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return Format{}, nil, ErrNotRIFF
	}
	riffSize := binary.LittleEndian.Uint32(header[4:8])
	if riffSize < 4 {
		return Format{}, nil, ErrUnsupportedWAV
	}
	remaining := &io.LimitedReader{R: r, N: int64(riffSize - 4)}
	state, err := readWAVChunks(remaining)
	if err != nil {
		return Format{}, nil, err
	}
	if !state.haveFormat || state.data == nil || len(state.data)%(state.format.Channels*2) != 0 {
		return Format{}, nil, ErrUnsupportedWAV
	}
	samples := make([]int16, len(state.data)/2)
	if _, err := DecodeS16LE(samples, state.data); err != nil {
		return Format{}, nil, fmt.Errorf("read WAV samples: %w", err)
	}
	return state.format, samples, nil
}

type wavReadState struct {
	format     Format
	haveFormat bool
	data       []byte
}

func readWAVChunks(remaining *io.LimitedReader) (wavReadState, error) {
	var state wavReadState
	for remaining.N > 0 {
		var chunk [8]byte
		if _, err := io.ReadFull(remaining, chunk[:]); err != nil {
			return wavReadState{}, fmt.Errorf("read WAV chunk: %w", err)
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		paddedSize := int64(size) + int64(size%2)
		if paddedSize > remaining.N {
			return wavReadState{}, fmt.Errorf("read WAV %q chunk: %w", chunk[0:4], io.ErrUnexpectedEOF)
		}
		if err := consumeWAVChunk(remaining, &state, string(chunk[0:4]), size); err != nil {
			return wavReadState{}, err
		}
		if size%2 != 0 {
			var padding [1]byte
			if _, err := io.ReadFull(remaining, padding[:]); err != nil {
				return wavReadState{}, fmt.Errorf("read WAV chunk padding: %w", err)
			}
		}
	}
	return state, nil
}

func consumeWAVChunk(r io.Reader, state *wavReadState, id string, size uint32) error {
	switch id {
	case "fmt ":
		if size > maxWAVFormatBytes {
			return ErrUnsupportedWAV
		}
		payload, err := readWAVChunk(r, size)
		if err != nil {
			return fmt.Errorf("read WAV fmt chunk: %w", err)
		}
		format, err := parseWAVFormat(payload)
		if err != nil {
			return err
		}
		state.format = format
		state.haveFormat = true
	case "data":
		if size > maxWAVDataBytes {
			return ErrUnsupportedWAV
		}
		data, err := readWAVChunk(r, size)
		if err != nil {
			return fmt.Errorf("read WAV data chunk: %w", err)
		}
		state.data = data
	default:
		if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
			return fmt.Errorf("skip WAV %q chunk: %w", id, err)
		}
	}
	return nil
}

func parseWAVFormat(payload []byte) (Format, error) {
	if len(payload) < 16 || binary.LittleEndian.Uint16(payload[0:2]) != 1 || binary.LittleEndian.Uint16(payload[14:16]) != 16 {
		return Format{}, ErrUnsupportedWAV
	}
	format := Format{SampleRate: int(binary.LittleEndian.Uint32(payload[4:8])), Channels: int(binary.LittleEndian.Uint16(payload[2:4])), Layout: LayoutS16LE}
	if err := format.Validate(); err != nil {
		return Format{}, fmt.Errorf("read WAV format: %w", err)
	}
	blockAlign := uint64(format.Channels * 2)                  //nolint:gosec // G115: WAV channel count originates from uint16.
	expectedByteRate := uint64(format.SampleRate) * blockAlign //nolint:gosec // G115: WAV sample rate originates from uint32.
	if uint64(binary.LittleEndian.Uint16(payload[12:14])) != blockAlign || uint64(binary.LittleEndian.Uint32(payload[8:12])) != expectedByteRate {
		return Format{}, ErrUnsupportedWAV
	}
	return format, nil
}

func readWAVChunk(r io.Reader, size uint32) ([]byte, error) {
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read chunk payload: %w", err)
	}
	return payload, nil
}

type WAVWriter struct {
	w          io.WriteSeeker
	dataBytes  uint64
	closed     bool
	writeError error
}

func NewWAVWriter(w io.WriteSeeker, format Format) (*WAVWriter, error) {
	if err := validateWAVFormat(format); err != nil {
		return nil, err
	}
	if _, err := w.Write(wavHeader(format, 0)); err != nil {
		return nil, fmt.Errorf("write WAV header: %w", err)
	}
	return &WAVWriter{w: w}, nil
}

func (w *WAVWriter) Write(samples []int16) (int, error) {
	if w.closed {
		return 0, errors.New("write WAV: writer is closed")
	}
	buf := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(sample)) //nolint:gosec // G115: conversion preserves the signed PCM bit pattern.
	}
	written, err := w.w.Write(buf)
	w.dataBytes += uint64(written) //nolint:gosec // G115: io.Writer byte counts are never negative.
	if err != nil {
		w.writeError = err
		return written / 2, fmt.Errorf("write WAV samples: %w", err)
	}
	if written != len(buf) {
		w.writeError = io.ErrShortWrite
		return written / 2, fmt.Errorf("write WAV samples: %w", io.ErrShortWrite)
	}
	return len(samples), nil
}

func (w *WAVWriter) Close() error {
	if w.closed {
		return w.writeError
	}
	w.closed = true
	if w.writeError != nil {
		return w.writeError
	}
	if w.dataBytes > math.MaxUint32-36 {
		return ErrUnsupportedWAV
	}
	dataBytes := uint32(w.dataBytes)
	if _, err := w.w.Seek(4, io.SeekStart); err != nil {
		return fmt.Errorf("seek WAV RIFF size: %w", err)
	}
	if err := binary.Write(w.w, binary.LittleEndian, uint32(36)+dataBytes); err != nil {
		return fmt.Errorf("patch WAV RIFF size: %w", err)
	}
	if _, err := w.w.Seek(40, io.SeekStart); err != nil {
		return fmt.Errorf("seek WAV data size: %w", err)
	}
	if err := binary.Write(w.w, binary.LittleEndian, dataBytes); err != nil {
		return fmt.Errorf("patch WAV data size: %w", err)
	}
	_, err := w.w.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("seek WAV end: %w", err)
	}
	return nil
}

func validateWAVFormat(format Format) error {
	if err := format.Validate(); err != nil {
		return fmt.Errorf("WAV format: %w", err)
	}
	// The uint64 conversions are safe after Format.Validate establishes positive values.
	byteRate := uint64(format.SampleRate) * uint64(format.Channels) * 2 //nolint:gosec // G115: validated positive ints convert losslessly to uint64.
	if format.Layout != LayoutS16LE || format.Channels > math.MaxUint16/2 || byteRate > math.MaxUint32 {
		return ErrUnsupportedWAV
	}
	return nil
}

func wavHeader(format Format, dataBytes uint32) []byte {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataBytes)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(format.Channels))   //nolint:gosec // G115: validateWAVFormat bounds channel count.
	binary.LittleEndian.PutUint32(header[24:28], uint32(format.SampleRate)) //nolint:gosec // G115: validateWAVFormat bounds byte rate.
	byteRate := uint32(format.SampleRate * format.Channels * 2)             //nolint:gosec // G115: validateWAVFormat bounds byte rate.
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	binary.LittleEndian.PutUint16(header[32:34], uint16(format.Channels*2)) //nolint:gosec // G115: validateWAVFormat bounds block alignment.
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataBytes)
	return header
}
