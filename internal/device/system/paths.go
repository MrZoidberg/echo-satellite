// Package system provides device identity, resource, path, and bounded-log primitives.
package system

const (
	EtcDir       = "/data/local/etc/echo-satellite"
	WakeModelDir = EtcDir + "/wake-models"
	DeviceIDFile = EtcDir + "/device-id"
	RecordingDir = "/data/local/tmp"
)
