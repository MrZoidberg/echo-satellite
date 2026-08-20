package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDevicePaths(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/data/local/etc/echo-satellite", EtcDir)
	assert.Equal(t, "/data/local/etc/echo-satellite/wake-models", WakeModelDir)
	assert.Equal(t, "/data/local/etc/echo-satellite/device-id", DeviceIDFile)
	assert.Equal(t, "/data/local/tmp", RecordingDir)
}
