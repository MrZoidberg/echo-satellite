//go:build !linux

package alsa

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPCM_UnsupportedOnNonLinuxReturnsErrUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	_, err := OpenCapture(micConfig())
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
	_, err = OpenPlayback(speakerConfig())
	require.ErrorIs(t, err, ErrUnsupportedPlatform)

	pcm := &PCM{}
	_, err = pcm.ReadInterleaved(nil)
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
	_, err = pcm.WriteInterleaved(nil)
	require.ErrorIs(t, err, ErrUnsupportedPlatform)
	require.ErrorIs(t, pcm.Prepare(), ErrUnsupportedPlatform)
	require.ErrorIs(t, pcm.Start(), ErrUnsupportedPlatform)
	require.ErrorIs(t, pcm.Drop(), ErrUnsupportedPlatform)
	require.ErrorIs(t, pcm.Close(), ErrUnsupportedPlatform)
}
