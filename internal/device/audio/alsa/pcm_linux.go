//go:build linux

package alsa

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type transfer struct {
	result int64
	buffer uintptr
	frames uintptr
}

// PCM is a configured ALSA PCM stream.
type PCM struct {
	file       *os.File
	config     Config
	frameBytes int
}

func OpenCapture(config Config) (*PCM, error) {
	config.Capture = true
	return open(config)
}

func OpenPlayback(config Config) (*PCM, error) {
	config.Capture = false
	return open(config)
}

func open(config Config) (*PCM, error) {
	params, err := encodeHWParams(config)
	if err != nil {
		return nil, err
	}

	flags := os.O_WRONLY | unix.O_NONBLOCK
	if config.Capture {
		flags = os.O_RDONLY | unix.O_NONBLOCK
	}
	path := DevicePath(config)
	file, err := os.OpenFile(path, flags, 0) //nolint:gosec // G304: validated numeric card/device fields construct a fixed /dev/snd path.
	if err != nil {
		if errors.Is(err, unix.EBUSY) || errors.Is(err, unix.EAGAIN) {
			// A running Alexa service is the usual owner on an unprepared Dot.
			return nil, fmt.Errorf("%w: %s", ErrDeviceBusy, path)
		}
		return nil, fmt.Errorf("open ALSA device %s: %w", path, err)
	}
	closeOnError := func(openErr error) (*PCM, error) {
		_ = file.Close()
		return nil, openErr
	}

	if err := unix.SetNonblock(int(file.Fd()), false); err != nil {
		return closeOnError(fmt.Errorf("configure blocking ALSA I/O: %w", err))
	}
	if err := ioctl(file.Fd(), ioctlHWParams, unsafe.Pointer(&params[0])); err != nil { //nolint:gosec // G103: ioctl requires the UAPI buffer address.
		return closeOnError(fmt.Errorf("configure ALSA hardware: %w", err))
	}
	if err := validateGranted(params, config); err != nil {
		return closeOnError(err)
	}
	if err := ioctlNoArg(file.Fd(), ioctlPrepare); err != nil {
		return closeOnError(fmt.Errorf("prepare ALSA stream: %w", err))
	}
	if config.startsOnOpen() {
		if err := ioctlNoArg(file.Fd(), ioctlStart); err != nil {
			return closeOnError(fmt.Errorf("start ALSA stream: %w", err))
		}
	}

	return &PCM{file: file, config: config, frameBytes: config.Channels * config.bytesPerSample()}, nil
}

func validateGranted(params []byte, config Config) error {
	for _, parameter := range []struct {
		name  string
		index int
		want  uint32
	}{
		{"rate", paramRate, uint32(config.Rate)},                          //nolint:gosec // G115: Config.Validate bounds the UAPI value.
		{"channels", paramChannels, uint32(config.Channels)},              //nolint:gosec // G115: Config.Validate bounds the UAPI value.
		{"period frames", paramPeriodFrames, uint32(config.PeriodFrames)}, //nolint:gosec // G115: Config.Validate bounds the UAPI value.
		{"periods", paramPeriods, uint32(config.Periods)},                 //nolint:gosec // G115: Config.Validate bounds the UAPI value.
	} {
		if got := intervalValue(params, parameter.index); got != parameter.want {
			return fmt.Errorf("ALSA granted %s %d, requested %d", parameter.name, got, parameter.want)
		}
	}
	return nil
}

func intervalValue(params []byte, param int) uint32 {
	offset := intervalOffset + (param-firstInterval)*intervalSize
	return binary.LittleEndian.Uint32(params[offset:])
}

func (p *PCM) ReadInterleaved(buffer []byte) (int, error) {
	if p == nil || p.file == nil || !p.config.Capture {
		return 0, ErrNotConfigured
	}
	return p.transfer(buffer, ioctlReadI)
}

func (p *PCM) WriteInterleaved(buffer []byte) (int, error) {
	if p == nil || p.file == nil || p.config.Capture {
		return 0, ErrNotConfigured
	}
	return p.transfer(buffer, ioctlWriteI)
}

func (p *PCM) transfer(buffer []byte, request uintptr) (int, error) {
	if len(buffer) == 0 || len(buffer)%p.frameBytes != 0 {
		return 0, fmt.Errorf("ALSA transfer requires whole frames of %d bytes", p.frameBytes)
	}
	xfer := transfer{buffer: uintptr(unsafe.Pointer(&buffer[0])), frames: uintptr(len(buffer) / p.frameBytes)} //nolint:gosec // G103: snd_xferi requires the PCM buffer address.
	err := ioctl(p.file.Fd(), request, unsafe.Pointer(&xfer))                                                  //nolint:gosec // G103: ioctl requires the snd_xferi address.
	runtime.KeepAlive(buffer)
	if err != nil {
		if errors.Is(err, unix.EPIPE) {
			if recoveryErr := p.config.recoverXRun(p.Prepare, p.Start); recoveryErr != nil {
				return 0, fmt.Errorf("%w; recovery failed: %w", ErrXRun, recoveryErr)
			}
			return 0, ErrXRun
		}
		return 0, fmt.Errorf("transfer ALSA frames: %w", err)
	}
	return int(xfer.result), nil
}

func (p *PCM) Prepare() error {
	if p == nil || p.file == nil {
		return ErrNotConfigured
	}
	if err := ioctlNoArg(p.file.Fd(), ioctlPrepare); err != nil {
		return fmt.Errorf("prepare ALSA stream: %w", err)
	}
	return nil
}

func (p *PCM) Start() error {
	if p == nil || p.file == nil {
		return ErrNotConfigured
	}
	if err := ioctlNoArg(p.file.Fd(), ioctlStart); err != nil {
		return fmt.Errorf("start ALSA stream: %w", err)
	}
	return nil
}

func (p *PCM) Drop() error {
	if p == nil || p.file == nil {
		return ErrNotConfigured
	}
	if err := ioctlNoArg(p.file.Fd(), ioctlDrop); err != nil {
		return fmt.Errorf("drop ALSA stream: %w", err)
	}
	return nil
}

func (p *PCM) Close() error {
	if p == nil || p.file == nil {
		return ErrNotConfigured
	}
	_ = p.Drop()
	err := p.file.Close()
	p.file = nil
	if err != nil {
		return fmt.Errorf("close ALSA stream: %w", err)
	}
	return nil
}
