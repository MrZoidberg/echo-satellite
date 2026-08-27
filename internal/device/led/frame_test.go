package led

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrame_EncodeHexWrites36ChannelsInRGBOrder(t *testing.T) {
	frame := Uniform(RGB{R: 1, G: 2, B: 3})
	frame[1] = RGB{R: 0xaa, G: 0xbb, B: 0xcc}
	encoded := frame.EncodeHex()
	assert.Len(t, encoded, 72)
	assert.Equal(t, "010203aabbcc", encoded[:12])
	decoded, err := ParseHex(encoded)
	require.NoError(t, err)
	assert.Equal(t, frame, decoded)
}

func TestFrame_ParseHexRejectsWrongLength(t *testing.T) {
	_, err := ParseHex("00")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBadFrame)
	_, err = ParseHex(string(make([]byte, 72)))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBadFrame)
}
