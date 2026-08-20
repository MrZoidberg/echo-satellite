// Package alsa provides static, cgo-free access to the Linux ALSA PCM UAPI.
package alsa

import "fmt"

const maxUint32 = int(^uint32(0))

// Format is an ALSA snd_pcm_format_t value.
type Format int

const (
	FormatS16LE  Format = 2
	FormatS243LE Format = 32
)

const (
	MicCard         = 0
	MicDevice       = 24
	MicRate         = 16_000
	MicChannels     = 9
	MicFormat       = FormatS243LE
	MicPeriodFrames = 320
	MicPeriods      = 8

	SpeakerDevice       = 23
	SpeakerRate         = 48_000
	SpeakerChannels     = 2
	SpeakerFormat       = FormatS16LE
	SpeakerPeriodFrames = 1024
	SpeakerPeriods      = 4
)

// Config describes one directly accessed ALSA PCM device.
type Config struct {
	Card         int
	Device       int
	Rate         int
	Channels     int
	Format       Format
	PeriodFrames int
	Periods      int
	Capture      bool
}

func (c Config) Validate() error {
	if c.Card < 0 {
		return fmt.Errorf("ALSA card must not be negative: %d", c.Card)
	}
	if c.Device < 0 {
		return fmt.Errorf("ALSA device must not be negative: %d", c.Device)
	}
	if c.Rate <= 0 {
		return fmt.Errorf("ALSA rate must be positive: %d", c.Rate)
	}
	if c.Rate > maxUint32 {
		return fmt.Errorf("ALSA rate exceeds the UAPI range: %d", c.Rate)
	}
	if c.Channels <= 0 {
		return fmt.Errorf("ALSA channel count must be positive: %d", c.Channels)
	}
	if c.Channels > maxUint32/24 {
		return fmt.Errorf("ALSA channel count exceeds the UAPI range: %d", c.Channels)
	}
	if c.Format != FormatS16LE && c.Format != FormatS243LE {
		return fmt.Errorf("unsupported ALSA format: %d", c.Format)
	}
	if c.PeriodFrames <= 0 {
		return fmt.Errorf("ALSA period frames must be positive: %d", c.PeriodFrames)
	}
	if c.PeriodFrames > maxUint32 {
		return fmt.Errorf("ALSA period frames exceed the UAPI range: %d", c.PeriodFrames)
	}
	if c.Periods <= 0 {
		return fmt.Errorf("ALSA period count must be positive: %d", c.Periods)
	}
	if c.Periods > maxUint32 {
		return fmt.Errorf("ALSA period count exceeds the UAPI range: %d", c.Periods)
	}
	return nil
}

func DevicePath(c Config) string {
	suffix := "p"
	if c.Capture {
		suffix = "c"
	}
	return fmt.Sprintf("/dev/snd/pcmC%dD%d%s", c.Card, c.Device, suffix)
}

func (c Config) bytesPerSample() int {
	if c.Format == FormatS243LE {
		return 3
	}
	return 2
}

func (c Config) startsOnOpen() bool {
	// Capture needs an explicit start so the first blocking read can receive
	// frames. Playback starts when frames are written; starting its empty buffer
	// produces EPIPE on the Echo Dot codec.
	return c.Capture
}

func (c Config) recoverXRun(prepare, start func() error) error {
	if err := prepare(); err != nil {
		return err
	}
	if c.startsOnOpen() {
		return start()
	}
	return nil
}
