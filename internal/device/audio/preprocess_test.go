package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBypass_ReturnsInputUnchanged(t *testing.T) {
	t.Parallel()

	in := []int16{-3, 0, 7}
	bypass := Bypass{}
	out := bypass.Process(in)

	assert.Equal(t, "bypass", bypass.Name())
	assert.Equal(t, in, out)
	assert.Same(t, &in[0], &out[0])
}
