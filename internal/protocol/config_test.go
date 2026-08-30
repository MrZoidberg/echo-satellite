package protocol

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceConfig_Validate(t *testing.T) {
	require.NoError(t, testDeviceConfig().Validate())

	tests := []struct {
		name   string
		mutate func(*DeviceConfig)
	}{
		{"zero version", func(c *DeviceConfig) { c.Version = 0 }},
		{"missing model", func(c *DeviceConfig) { c.Wake.Model = "" }},
		{"nonfinite wake threshold", func(c *DeviceConfig) { c.Wake.Threshold = math.NaN() }},
		{"invalid VAD threshold", func(c *DeviceConfig) { c.Wake.VADThreshold = 1.1 }},
		{"invalid disabled VAD threshold", func(c *DeviceConfig) { c.Wake.VADEnabled = false; c.Wake.VADThreshold = 1.1 }},
		{"invalid endpoint threshold", func(c *DeviceConfig) { c.Endpointing.SpeechThreshold = -0.1 }},
		{"zero duration", func(c *DeviceConfig) { c.Endpointing.MaxTurnMS = 0 }},
		{"invalid log level", func(c *DeviceConfig) { c.Logs.ForwardLevel = "verbose" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testDeviceConfig()
			tt.mutate(&config)
			assert.Error(t, config.Validate())
		})
	}
}

func TestDeviceConfig_UnmarshalJSONRequiresCompleteConfig(t *testing.T) {
	data := []byte(`{"version":1,"wake":{"engine":"openwakeword"},"endpointing":{},"logs":{}}`)
	var config DeviceConfig
	require.Error(t, json.Unmarshal(data, &config))
}

func TestConfigResult_Validate(t *testing.T) {
	require.NoError(t, (ConfigResult{Version: 1, Status: ConfigResultPending}).Validate())
	require.Error(t, (ConfigResult{Version: 0, Status: ConfigResultApplied}).Validate())
	require.Error(t, (ConfigResult{Version: 1, Status: "unknown"}).Validate())
	require.Error(t, (ConfigResult{Version: 1, Status: ConfigResultRejected}).Validate())
	assert.NoError(t, (ConfigResult{Version: 1, Status: ConfigResultRejected, Code: "invalid"}).Validate())
}

func TestLogRecord_Validate(t *testing.T) {
	require.NoError(t, (LogRecord{Level: LogLevelInfo, Message: "connected", Fields: map[string]string{"server": "gateway"}}).Validate())
	require.Error(t, (LogRecord{Level: "verbose", Message: "connected"}).Validate())
	require.Error(t, (LogRecord{Level: LogLevelInfo}).Validate())
	assert.Error(t, (LogRecord{Level: LogLevelInfo, Message: "connected", Fields: map[string]string{"": "value"}}).Validate())
}

func TestAudioStopReason_Valid(t *testing.T) {
	for _, reason := range []AudioStopReason{AudioStopEndpointed, AudioStopNoSpeech, AudioStopTimeout, AudioStopEOF, AudioStopCaptureOverrun} {
		assert.True(t, reason.Valid())
	}
	assert.False(t, AudioStopReason("complete").Valid())
	assert.Error(t, (AudioStop{Reason: "complete"}).Validate())
}
