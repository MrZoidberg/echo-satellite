package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEchoLocalDelaySum_Silence(t *testing.T) {
	t.Parallel()

	quiet := make([][]int16, PhysicalMicrophones)
	for mic := range quiet {
		quiet[mic] = make([]int16, 320)
	}

	assert.Equal(t, make([]int16, 320), NewEchoLocalDelaySum().Mix(quiet))
}

func TestEchoLocalDelaySum_ExcludesLoopbackAndMalformedChannels(t *testing.T) {
	t.Parallel()

	physical := make([][]int16, PhysicalMicrophones+2)
	for mic := range physical {
		if mic >= PhysicalMicrophones {
			break
		}
		physical[mic] = []int16{int16(mic + 1)}
	}
	physical[7] = []int16{30}
	physical[8] = []int16{40}
	assert.Nil(t, NewEchoLocalDelaySum().Mix(physical))
	assert.Nil(t, NewEchoLocalDelaySum().Mix(physical[:PhysicalMicrophones-1]))
	physical = physical[:PhysicalMicrophones]
	physical[3] = nil
	assert.Nil(t, NewEchoLocalDelaySum().Mix(physical))
}

func TestEchoLocalDelaySum_ImpulseFixedDelayAndPolarity(t *testing.T) {
	t.Parallel()

	input := make([][]int16, PhysicalMicrophones)
	for mic := range input {
		input[mic] = make([]int16, 2)
	}
	input[0][0] = 700

	beam := zeroSteeringBeam()
	for direction := range echoLocalBeams {
		beam.taps[direction][0].back = 1
	}
	// The impulse is delayed by exactly one sample before it is averaged with
	// the other six silent physical microphones.
	assert.Equal(t, []int16{0, 100}, beam.Mix(input))

	input[0][0] = -700
	assert.Equal(t, []int16{0, -100}, zeroSteeringBeamWithDelay().Mix(input))
}

func zeroSteeringBeam() *EchoLocalDelaySum {
	beam := NewEchoLocalDelaySum()
	for direction := range echoLocalBeams {
		for mic := range PhysicalMicrophones {
			beam.taps[direction][mic] = echoLocalTap{}
		}
	}
	return beam
}

func zeroSteeringBeamWithDelay() *EchoLocalDelaySum {
	beam := zeroSteeringBeam()
	for direction := range echoLocalBeams {
		beam.taps[direction][0].back = 1
	}
	return beam
}

func TestEchoLocalDelaySum_Saturates(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int16(32_767), clampPCM(32_768))
	assert.Equal(t, int16(-32_768), clampPCM(-32_769))
}
