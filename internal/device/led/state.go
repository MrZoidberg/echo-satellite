package led

import "github.com/MrZoidberg/echo-satellite/internal/protocol"

var (
	colorIdle      = RGB{B: 6}
	colorListening = RGB{G: 110, B: 150}
	colorThinking  = RGB{B: 180}
	colorSpeaking  = RGB{B: 120}
	colorMuted     = RGB{R: 180}
	colorOffline   = RGB{R: 18, B: 30}
	colorError     = RGB{R: 180, G: 55}
	colorUpdating  = RGB{R: 100, G: 100, B: 100}
	colorTrial     = RGB{R: 180, G: 70}
)

// Render maps a semantic state and animation tick to a complete LED frame.
// Unknown states deliberately render the error pattern.
func Render(state protocol.DeviceState, tick int) Frame {
	frame, known := renderKnown(state, tick)
	if known {
		return frame
	}
	return errorPattern(tick)
}

func renderKnown(state protocol.DeviceState, tick int) (Frame, bool) {
	switch state {
	case protocol.StateIdle:
		return Uniform(colorIdle), true
	case protocol.StateListening:
		greenLevels := [...]uint8{33, 55, 80, 110, 80, 55}
		blueLevels := [...]uint8{45, 75, 110, 150, 110, 75}
		color := colorListening
		phase := positiveMod(tick, len(blueLevels))
		color.G = greenLevels[phase]
		color.B = blueLevels[phase]
		return Uniform(color), true
	case protocol.StateThinking:
		return chase(tick, colorThinking, 1), true
	case protocol.StateSpeaking:
		return Uniform(colorSpeaking), true
	case protocol.StateMuted:
		return Uniform(colorMuted), true
	case protocol.StateOffline:
		return Uniform(colorOffline), true
	case protocol.StateError:
		return errorPattern(tick), true
	case protocol.StateUpdating:
		return chase(tick, colorUpdating, 3), true
	case protocol.StateUpdateTrial:
		return chase(tick, colorTrial, 2), true
	default:
		return Frame{}, false
	}
}

func chase(tick int, color RGB, width int) Frame {
	var frame Frame
	start := positiveMod(tick, SegmentCount)
	for offset := range width {
		frame[(start+offset)%SegmentCount] = color
	}
	return frame
}

func errorPattern(tick int) Frame {
	phase := positiveMod(tick, 8)
	if phase == 0 || phase == 2 {
		return Uniform(colorError)
	}
	return Frame{}
}

func positiveMod(value, modulus int) int {
	result := value % modulus
	if result < 0 {
		return result + modulus
	}
	return result
}
