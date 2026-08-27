package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFrameDuration(t *testing.T) {
	t.Parallel()

	frame := Frame{Offset: 42, Samples: make([]int16, 1_280)}
	assert.Equal(t, 80*time.Millisecond, frame.Duration())
	assert.Equal(t, int64(42), frame.Offset)
}
