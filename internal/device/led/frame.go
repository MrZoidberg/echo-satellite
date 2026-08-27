// Package led renders semantic device states and writes frames to the Echo
// Dot's IS31FL3236 LED controller.
package led

import (
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	// SegmentCount is the number of RGB segments in the Echo Dot light ring.
	SegmentCount = 12
	// Channels is the number of PWM channels in one hardware frame.
	Channels = SegmentCount * 3
)

// ErrBadFrame indicates malformed hexadecimal frame data.
var ErrBadFrame = errors.New("bad LED frame")

// RGB is one eight-bit red, green, blue segment.
type RGB struct {
	R uint8
	G uint8
	B uint8
}

// Frame is one complete light-ring frame in physical segment order.
type Frame [SegmentCount]RGB

// EncodeHex returns the 36 PWM channels as lower-case R,G,B hexadecimal bytes.
func (f Frame) EncodeHex() string {
	raw := make([]byte, 0, Channels)
	for _, segment := range f {
		raw = append(raw, segment.R, segment.G, segment.B)
	}
	return hex.EncodeToString(raw)
}

// ParseHex decodes one complete R,G,B hardware frame.
func ParseHex(encoded string) (Frame, error) {
	if len(encoded) != Channels*2 {
		return Frame{}, fmt.Errorf("%w: got %d hexadecimal characters, want %d", ErrBadFrame, len(encoded), Channels*2)
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return Frame{}, fmt.Errorf("%w: decode hexadecimal data: %w", ErrBadFrame, err)
	}
	var frame Frame
	for i := range frame {
		frame[i] = RGB{R: raw[i*3], G: raw[i*3+1], B: raw[i*3+2]}
	}
	return frame, nil
}

// Uniform returns a frame with every segment set to color.
func Uniform(color RGB) Frame {
	var frame Frame
	for i := range frame {
		frame[i] = color
	}
	return frame
}
