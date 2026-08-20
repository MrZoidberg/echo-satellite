package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenCaptureSource_FileUsesDotDeviceFormat(t *testing.T) {
	source, err := openCaptureSource(micRecordCommand{FromFile: audioFixture("dot_mic_9ch_s24le.raw")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, source.Close()) })
	assert.Equal(t, 16_000, source.Format().SampleRate)
	assert.Equal(t, 9, source.Format().Channels)
}
