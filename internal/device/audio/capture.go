package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
)

type CaptureConfig struct {
	Device       Format
	Channels     []int
	Preprocessor Preprocessor
	StepSamples  int
}

type Capturer struct {
	source PCMSource
	config CaptureConfig
	logger *slog.Logger
	xruns  atomic.Uint64
}

func NewCapturer(source PCMSource, config CaptureConfig, logger *slog.Logger) (*Capturer, error) {
	if source == nil {
		return nil, errors.New("create capturer: nil PCM source")
	}
	if err := config.Device.Validate(); err != nil {
		return nil, fmt.Errorf("create capturer: %w", err)
	}
	if config.Device != source.Format() {
		return nil, fmt.Errorf("create capturer: source format %+v does not match configured format %+v", source.Format(), config.Device)
	}
	if config.Device.SampleRate != CanonicalSampleRate {
		return nil, fmt.Errorf("create capturer: sample rate must be %d", CanonicalSampleRate)
	}
	if len(config.Channels) == 0 {
		return nil, errors.New("create capturer: at least one channel is required")
	}
	for _, channel := range config.Channels {
		if channel < 0 || channel >= config.Device.Channels {
			return nil, fmt.Errorf("create capturer: channel %d outside [0,%d)", channel, config.Device.Channels)
		}
	}
	if config.Preprocessor == nil {
		return nil, errors.New("create capturer: nil preprocessor")
	}
	if config.StepSamples <= 0 {
		return nil, fmt.Errorf("create capturer: step samples must be positive: %d", config.StepSamples)
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Capturer{source: source, config: config, logger: logger}, nil
}

func (c *Capturer) XRuns() uint64 { return c.xruns.Load() }

func (c *Capturer) Run(ctx context.Context, out func(Frame) error) error {
	if out == nil {
		return errors.New("run capturer: nil output callback")
	}
	deviceBytes := make([]byte, c.config.StepSamples*c.config.Device.BytesPerFrame())
	decoded := make([]int16, c.config.StepSamples*c.config.Device.Channels)
	selected := make([]int16, c.config.StepSamples*len(c.config.Channels))
	mono := make([]int16, c.config.StepSamples)
	var offset int64

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		frames, err := c.source.ReadInterleaved(deviceBytes)
		if errors.Is(err, alsa.ErrXRun) {
			count := c.xruns.Add(1)
			c.logger.Warn("ALSA capture overrun", "xruns", count)
			continue
		}
		if frames < 0 || frames > c.config.StepSamples {
			return fmt.Errorf("capture PCM: source returned %d frames, expected [0,%d]", frames, c.config.StepSamples)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("capture PCM: %w", err)
		}
		if frames > 0 {
			frame, convertErr := c.convert(deviceBytes[:frames*c.config.Device.BytesPerFrame()], decoded, selected, mono)
			if convertErr != nil {
				return convertErr
			}
			processed := c.config.Preprocessor.Process(frame)
			owned := append([]int16(nil), processed...)
			if emitErr := out(Frame{Offset: offset, Samples: owned}); emitErr != nil {
				return fmt.Errorf("emit captured frame: %w", emitErr)
			}
			offset += int64(len(owned))
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return nil
}

func (c *Capturer) convert(raw []byte, decoded, selected, mono []int16) ([]int16, error) {
	var samples int
	var err error
	switch c.config.Device.Layout {
	case LayoutS16LE:
		samples, err = DecodeS16LE(decoded, raw)
	case LayoutS24_3LE:
		samples, err = DecodeS24_3LE(decoded, raw)
	default:
		err = ErrUnsupportedLayout
	}
	if err != nil {
		return nil, fmt.Errorf("decode captured PCM: %w", err)
	}
	selectedSamples, err := SelectChannels(selected, decoded[:samples], c.config.Device.Channels, c.config.Channels)
	if err != nil {
		return nil, fmt.Errorf("select capture channels: %w", err)
	}
	frames, err := MonoDownmix(mono, selected[:selectedSamples], len(c.config.Channels))
	if err != nil {
		return nil, fmt.Errorf("downmix capture channels: %w", err)
	}
	return mono[:frames], nil
}
