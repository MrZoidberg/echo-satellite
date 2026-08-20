package main

import (
	"fmt"
	"os"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
)

var (
	openALSACapture  = alsa.OpenCapture
	openALSAPlayback = alsa.OpenPlayback
)

type formattedSource struct {
	*alsa.PCM
	format audio.Format
}

func (s *formattedSource) Format() audio.Format { return s.format }

type formattedSink struct {
	*alsa.PCM
	format audio.Format
}

func (s *formattedSink) Format() audio.Format { return s.format }

func openCaptureSource(c micRecordCommand) (audio.PCMSource, error) {
	format := audio.Format{SampleRate: alsa.MicRate, Channels: alsa.MicChannels, Layout: audio.LayoutS24_3LE}
	if c.FromFile != "" {
		source, err := audio.NewFileSource(c.FromFile, format, false)
		if err != nil {
			return nil, fmt.Errorf("open fixture capture source: %w", err)
		}
		return source, nil
	}
	pcm, err := openALSACapture(alsa.Config{Card: c.Card, Device: c.Device, Rate: alsa.MicRate, Channels: alsa.MicChannels, Format: alsa.MicFormat, PeriodFrames: alsa.MicPeriodFrames, Periods: alsa.MicPeriods, Capture: true})
	if err != nil {
		return nil, fmt.Errorf("open microphone capture: %w", err)
	}
	return &formattedSource{PCM: pcm, format: format}, nil
}

func openPlaybackSink(c speakerTestCommand) (audio.PCMSink, error) {
	format := audio.Format{SampleRate: alsa.SpeakerRate, Channels: alsa.SpeakerChannels, Layout: audio.LayoutS16LE}
	if c.ToFile != "" {
		file, err := os.Create(c.ToFile)
		if err != nil {
			return nil, fmt.Errorf("create playback output: %w", err)
		}
		sink, err := audio.NewWAVSink(file, format)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("create playback WAV sink: %w", err)
		}
		return &fileSink{PCMSink: sink, file: file}, nil
	}
	pcm, err := openALSAPlayback(alsa.Config{Card: c.Card, Device: c.Device, Rate: alsa.SpeakerRate, Channels: alsa.SpeakerChannels, Format: alsa.SpeakerFormat, PeriodFrames: alsa.SpeakerPeriodFrames, Periods: alsa.SpeakerPeriods})
	if err != nil {
		return nil, fmt.Errorf("open speaker playback: %w", err)
	}
	return &formattedSink{PCM: pcm, format: format}, nil
}

type fileSink struct {
	audio.PCMSink
	file *os.File
}

func (s *fileSink) Close() error {
	if err := s.PCMSink.Close(); err != nil {
		_ = s.file.Close()
		return fmt.Errorf("close playback WAV sink: %w", err)
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close playback output: %w", err)
	}
	return nil
}
