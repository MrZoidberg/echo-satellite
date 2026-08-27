package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
)

func micRecord(w io.Writer, c micRecordCommand) error {
	if c.Seconds <= 0 {
		return fmt.Errorf("seconds must be positive: %g", c.Seconds)
	}
	source, err := openCaptureSource(c)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	channels, err := parseMicChannels(c.Channels, source.Format().Channels)
	if err != nil {
		return err
	}
	file, err := os.Create(c.Out)
	if err != nil {
		return fmt.Errorf("create microphone WAV: %w", err)
	}
	defer func() { _ = file.Close() }()
	wav, err := audio.NewWAVWriter(file, audio.Format{SampleRate: source.Format().SampleRate, Channels: len(channels), Layout: audio.LayoutS16LE})
	if err != nil {
		return fmt.Errorf("create microphone WAV writer: %w", err)
	}
	limit := int(math.Round(c.Seconds * float64(source.Format().SampleRate)))
	peaks, sums, count := make([]float64, len(channels)), make([]float64, len(channels)), 0
	raw := make([]byte, 320*source.Format().BytesPerFrame())
	decoded := make([]int16, 320*source.Format().Channels)
	selected := make([]int16, 320*len(channels))
	for count < limit {
		frames, readErr := source.ReadInterleaved(raw)
		if frames > limit-count {
			frames = limit - count
		}
		if frames > 0 {
			n, decodeErr := decodeCapture(decoded, raw[:frames*source.Format().BytesPerFrame()], source.Format().Layout)
			if decodeErr != nil {
				return decodeErr
			}
			n, selectErr := audio.SelectChannels(selected, decoded[:n], source.Format().Channels, channels)
			if selectErr != nil {
				return fmt.Errorf("select microphone channels: %w", selectErr)
			}
			if _, err = wav.Write(selected[:n]); err != nil {
				return fmt.Errorf("write microphone WAV: %w", err)
			}
			accumulateLevels(selected[:n], len(channels), peaks, sums)
			count += frames
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read microphone PCM: %w", readErr)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err = wav.Close(); err != nil {
		return fmt.Errorf("close microphone WAV: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close microphone output: %w", err)
	}
	if err = source.Close(); err != nil {
		return fmt.Errorf("close microphone capture: %w", err)
	}
	lines := []string{fmt.Sprintf("recorded: %s (%d Hz, %d channels, %d frames)", c.Out, source.Format().SampleRate, len(channels), count)}
	if c.PrintLevels {
		for i, channel := range channels {
			lines = append(lines, fmt.Sprintf("channel mic%d: peak %.2f dBFS, rms %.2f dBFS", channel, dbfs(peaks[i]), dbfs(math.Sqrt(sums[i]/float64(max(count, 1))))))
		}
	}
	return writeReport(w, lines)
}

func decodeCapture(dst []int16, raw []byte, layout audio.SampleLayout) (int, error) {
	if layout == audio.LayoutS24_3LE {
		n, err := audio.DecodeS24_3LE(dst, raw)
		if err != nil {
			return 0, fmt.Errorf("decode S24_3LE capture: %w", err)
		}
		return n, nil
	}
	n, err := audio.DecodeS16LE(dst, raw)
	if err != nil {
		return 0, fmt.Errorf("decode S16LE capture: %w", err)
	}
	return n, nil
}

func parseMicChannels(value string, available int) ([]int, error) {
	if value == "all" {
		channels := make([]int, available)
		for i := range channels {
			channels[i] = i
		}
		return channels, nil
	}
	parts := strings.Split(value, ",")
	channels := make([]int, 0, len(parts))
	seen := make(map[int]bool)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "mic") {
			return nil, fmt.Errorf("invalid microphone channel %q", part)
		}
		channel, err := strconv.Atoi(strings.TrimPrefix(part, "mic"))
		if err != nil || channel < 0 || channel >= min(7, available) || seen[channel] {
			return nil, fmt.Errorf("invalid microphone channel %q", part)
		}
		seen[channel] = true
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return nil, errors.New("at least one microphone channel is required")
	}
	return channels, nil
}

func accumulateLevels(samples []int16, channels int, peaks, sums []float64) {
	for i, sample := range samples {
		value := math.Abs(float64(sample))
		channel := i % channels
		peaks[channel] = max(peaks[channel], value)
		sums[channel] += value * value
	}
}

func dbfs(value float64) float64 {
	if value == 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(value/32768)
}
