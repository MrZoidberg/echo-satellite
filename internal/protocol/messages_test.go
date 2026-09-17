package protocol

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validTelemetry() TurnTelemetry {
	return TurnTelemetry{Version: 1, StoppedAt: time.Now().UTC(), Duration: time.Second, Capture: CaptureHealth{Frames: 10}, Conditioning: ConditioningHealth{Profile: "dot-gen2-qualified-v1", PeakDBFS: -12, RMSDBFS: -30, ClippingFraction: 0.01}, Resources: ResourceHealth{CPUPercent: 20, RSSBytes: 1024, Available: true}}
}

func TestHealthAndAudioStopTelemetryValidateAndRoundTrip(t *testing.T) {
	report := Health{Version: 1, Conditioning: ConditioningHealth{Profile: "dot-gen2-qualified-v1", PeakDBFS: -12, RMSDBFS: -30}, Resources: ResourceHealth{Available: false}}
	data, err := Encode(TypeHealth, "", time.Now(), report)
	require.NoError(t, err)
	envelope, err := Decode(data)
	require.NoError(t, err)
	var decoded Health
	require.NoError(t, envelope.DecodePayload(&decoded))
	assert.Equal(t, report, decoded)
	telemetry := validTelemetry()
	assert.NoError(t, (AudioStop{Reason: AudioStopEndpointed, Telemetry: &telemetry}).Validate())
}

func TestTelemetryValidationRejectsUnsafeValues(t *testing.T) {
	base := validTelemetry()
	for name, mutate := range map[string]func(*TurnTelemetry){
		"nan":               func(v *TurnTelemetry) { v.Conditioning.PeakDBFS = math.NaN() },
		"infinite":          func(v *TurnTelemetry) { v.Resources.CPUPercent = math.Inf(1) },
		"negative duration": func(v *TurnTelemetry) { v.Duration = -time.Second },
		"path profile":      func(v *TurnTelemetry) { v.Conditioning.Profile = "/tmp/raw.wav" },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			assert.Error(t, value.Validate())
		})
	}
}

func TestHealthHasNoFileOrSecretFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "messages.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "RawAudio")
	assert.NotContains(t, string(data), "Token")
}

func TestAllMessageTypes_CoversDesignFamilies(t *testing.T) {
	// the list from docs/DESIGN.md 8.6, verbatim
	want := []string{
		"hello", "welcome", "config", "config.result", "state", "health", "log",
		"turn.start", "turn.cancel",
		"wake.models", "wake.status",
		"audio.start", "audio.stop", "play.start", "play.stop",
		"update.offer", "update.decision", "update.progress", "update.confirmed",
		"update.cancelled", "update.failed",
		"button", "mute", "volume",
		"ping", "pong", "error",
	}

	got := make([]string, 0, len(AllMessageTypes()))
	for _, mt := range AllMessageTypes() {
		got = append(got, mt.String())
	}
	assert.ElementsMatch(t, want, got)

	for _, name := range want {
		assert.True(t, MessageType(name).Known(), "%s must be a known message type", name)
	}
}

func TestAllMessageTypes_ReturnsACopy(t *testing.T) {
	first := AllMessageTypes()
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.NotEqual(t, MessageType("mutated"), AllMessageTypes()[0])
}

func TestMessageType_Known(t *testing.T) {
	assert.True(t, TypeTurnStart.Known())
	assert.False(t, MessageType("").Known())
	assert.False(t, MessageType("wake.score").Known(), "the gateway never scores wake words")
}

func TestTurnTrigger_Valid(t *testing.T) {
	assert.True(t, TriggerWake.Valid())
	assert.True(t, TriggerButton.Valid())
	assert.False(t, TurnTrigger("gateway").Valid(), "a turn is never triggered by the gateway")
	assert.False(t, TurnTrigger("").Valid())
}

func TestAllDeviceStates_ReturnsDocumentedStatesAndACopy(t *testing.T) {
	want := []DeviceState{
		StateIdle, StateListening, StateThinking, StateSpeaking, StateMuted,
		StateOffline, StateError, StateUpdating,
	}
	first := AllDeviceStates()
	assert.Equal(t, want, first)
	require.NotEmpty(t, first)
	first[0] = "mutated"
	assert.Equal(t, StateIdle, AllDeviceStates()[0])
}

func TestError_ErrorString(t *testing.T) {
	assert.Equal(t, "unauthorized: unknown device", Error{Code: "unauthorized", Message: "unknown device"}.Error())
	assert.Equal(t, "unknown device", Error{Message: "unknown device"}.Error())
}
