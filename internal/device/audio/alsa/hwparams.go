package alsa

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import (
	"encoding/binary"
	"fmt"
)

// snd_pcm_hw_params uses the Linux 64-bit UAPI layout. The only word-sized
// member is fifo_size; all supported build targets use an eight-byte long.
const (
	maskOffset     = 4
	maskSize       = 32
	intervalOffset = maskOffset + 8*maskSize
	intervalSize   = 12
	rmaskOffset    = intervalOffset + 21*intervalSize
	infoOffset     = rmaskOffset + 8
	hwParamsSize   = rmaskOffset + 6*4 + 8 + 64
)

const (
	paramAccess       = 0
	paramFormat       = 1
	paramSubformat    = 2
	paramSampleBits   = 8
	paramFrameBits    = 9
	paramChannels     = 10
	paramRate         = 11
	paramPeriodFrames = 13
	paramPeriods      = 15
	firstInterval     = paramSampleBits
	lastInterval      = 19

	accessRWInterleaved = 3
	subformatStandard   = 0
)

func encodeHWParams(config Config) ([]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("encode ALSA hardware parameters: %w", err)
	}

	params := make([]byte, hwParamsSize)
	for mask := paramAccess; mask <= paramSubformat; mask++ {
		putUint32(params, maskOffset+mask*maskSize, ^uint32(0))
		putUint32(params, maskOffset+mask*maskSize+4, ^uint32(0))
	}
	for param := firstInterval; param <= lastInterval; param++ {
		offset := intervalOffset + (param-firstInterval)*intervalSize
		putUint32(params, offset, 0)
		putUint32(params, offset+4, ^uint32(0))
	}
	putUint32(params, rmaskOffset, ^uint32(0))
	putUint32(params, infoOffset, ^uint32(0))

	setMask(params, paramAccess, accessRWInterleaved)
	setMask(params, paramFormat, int(config.Format))
	setMask(params, paramSubformat, subformatStandard)
	bits := config.bytesPerSample() * 8
	setInterval(params, paramSampleBits, uint32(bits))                  //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	setInterval(params, paramFrameBits, uint32(bits*config.Channels))   //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	setInterval(params, paramChannels, uint32(config.Channels))         //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	setInterval(params, paramRate, uint32(config.Rate))                 //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	setInterval(params, paramPeriodFrames, uint32(config.PeriodFrames)) //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	setInterval(params, paramPeriods, uint32(config.Periods))           //nolint:gosec // G115: Validate bounds all UAPI values before encoding.
	return params, nil
}

func setMask(params []byte, param, bit int) {
	offset := maskOffset + param*maskSize
	clear(params[offset : offset+maskSize])
	putUint32(params, offset+(bit/32)*4, 1<<uint(bit%32))
}

func setInterval(params []byte, param int, value uint32) {
	offset := intervalOffset + (param-firstInterval)*intervalSize
	putUint32(params, offset, value)
	putUint32(params, offset+4, value)
	putUint32(params, offset+8, 1<<2) // snd_interval.integer
}

func intervalValue(params []byte, param int) uint32 {
	offset := intervalOffset + (param-firstInterval)*intervalSize
	return binary.LittleEndian.Uint32(params[offset:])
}

func putUint32(dst []byte, offset int, value uint32) {
	binary.LittleEndian.PutUint32(dst[offset:], value)
}
