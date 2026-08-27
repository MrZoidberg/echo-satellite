package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, (Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}).Validate())
	assert.Equal(t, 4, (Format{SampleRate: 16_000, Channels: 2, Layout: LayoutS16LE}).BytesPerFrame())
	assert.Equal(t, 27, (Format{SampleRate: 48_000, Channels: 9, Layout: LayoutS24_3LE}).BytesPerFrame())
	assert.Equal(t, 0, (Format{SampleRate: 16_000, Channels: 2}).BytesPerFrame())

	for name, format := range map[string]Format{
		"sample rate": {Channels: 1, Layout: LayoutS16LE},
		"channels":    {SampleRate: 16_000, Layout: LayoutS16LE},
		"layout":      {SampleRate: 16_000, Channels: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := format.Validate()
			require.Error(t, err)
			if name == "layout" {
				assert.ErrorIs(t, err, ErrUnsupportedLayout)
			}
		})
	}
}
