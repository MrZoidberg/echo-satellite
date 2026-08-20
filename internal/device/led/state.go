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
		return comet(tick, colorThinking, 5), true
	case protocol.StateSpeaking:
		return Uniform(colorSpeaking), true
	case protocol.StateMuted:
		return Uniform(colorMuted), true
	case protocol.StateOffline:
		return Uniform(colorOffline), true
	case protocol.StateError:
		return errorPattern(tick), true
	case protocol.StateUpdating:
		return comet(tick, colorUpdating, 4), true
	case protocol.StateUpdateTrial:
		return comet(tick, colorTrial, 4), true
	default:
		return Frame{}, false
	}
}

func comet(tick int, color RGB, tailLength int) Frame {
	const framesPerSegment = 2
	step := positiveMod(tick, SegmentCount*framesPerSegment)
	head := step / framesPerSegment
	fraction := float64(step%framesPerSegment) / framesPerSegment
	return blendFrames(cometAt(head, color, tailLength), cometAt(head+1, color, tailLength), fraction)
}

func cometAt(head int, color RGB, tailLength int) Frame {
	tail := [...]float64{1, 0.55, 0.3, 0.16, 0.08}
	var frame Frame
	for offset := 0; offset < tailLength && offset < len(tail); offset++ {
		frame[positiveMod(head-offset, SegmentCount)] = scale(color, tail[offset])
	}
	return frame
}

func blendFrames(from, to Frame, fraction float64) Frame {
	var frame Frame
	for i := range frame {
		frame[i] = RGB{
			R: blendChannel(from[i].R, to[i].R, fraction),
			G: blendChannel(from[i].G, to[i].G, fraction),
			B: blendChannel(from[i].B, to[i].B, fraction),
		}
	}
	return frame
}

func blendChannel(from, to uint8, fraction float64) uint8 {
	return uint8(float64(from)*(1-fraction) + float64(to)*fraction)
}

func scale(color RGB, factor float64) RGB {
	return RGB{
		R: uint8(float64(color.R) * factor),
		G: uint8(float64(color.G) * factor),
		B: uint8(float64(color.B) * factor),
	}
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
