package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecode_RoundTripPayloads(t *testing.T) {
	ts := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		msgType MessageType
		payload any
		into    func() any
	}{
		{
			name:    "hello",
			msgType: TypeHello,
			payload: Hello{
				DeviceID:          "dot-1",
				AgentVersion:      "0.1.0",
				SupervisorVersion: "1",
				Protocol:          ProtocolVersion,
				Capabilities:      NewCapabilities(CapWakeLocal, CapAudioCapture),
				WakeConfig: WakeConfig{
					Engine: "openwakeword", Models: []string{"okay_nabu"},
					WakeThreshold: 0.6, VADThreshold: 0.5, PreRollMS: 500,
				},
				UpdateState: PhaseIdle,
			},
			into: func() any { return &Hello{} },
		},
		{
			name:    "welcome",
			msgType: TypeWelcome,
			payload: Welcome{ServerID: "home-gateway", Protocol: ProtocolVersion,
				Config: map[string]any{"volume": float64(7)}},
			into: func() any { return &Welcome{} },
		},
		{
			name:    "turn.start",
			msgType: TypeTurnStart,
			payload: TurnStart{Trigger: TriggerWake, Model: "okay_nabu",
				WakeScore: 0.87, VADScore: 0.93, PreRollMS: 500},
			into: func() any { return &TurnStart{} },
		},
		{
			name:    "audio.start",
			msgType: TypeAudioStart,
			payload: AudioStart{SampleRate: 16000, Channels: 1, Format: AudioFormatPCMS16LE},
			into:    func() any { return &AudioStart{} },
		},
		{
			name:    "audio.stop",
			msgType: TypeAudioStop,
			payload: AudioStop{Reason: "endpointed"},
			into:    func() any { return &AudioStop{} },
		},
		{
			name:    "play.start",
			msgType: TypePlayStart,
			payload: PlayStart{SampleRate: 24000, Channels: 1, Format: AudioFormatPCMS16LE},
			into:    func() any { return &PlayStart{} },
		},
		{
			name:    "play.stop",
			msgType: TypePlayStop,
			payload: PlayStop{Reason: "complete"},
			into:    func() any { return &PlayStop{} },
		},
		{
			name:    "state",
			msgType: TypeState,
			payload: State{State: StateThinking, Detail: "asking assistant"},
			into:    func() any { return &State{} },
		},
		{
			name:    "update.offer",
			msgType: TypeUpdateOffer,
			payload: UpdateOffer{Version: "0.3.0", BuildID: "git-abc123",
				ArtifactURL: "https://gw.local/artifacts/echod", Size: 12849320, SHA256: "deadbeef"},
			into: func() any { return &UpdateOffer{} },
		},
		{
			name:    "update.progress",
			msgType: TypeUpdateProgress,
			payload: UpdateProgress{Phase: PhaseDownloading, Percent: 42},
			into:    func() any { return &UpdateProgress{} },
		},
		{
			name:    "update.failed",
			msgType: TypeUpdateFailed,
			payload: UpdateFailed{Code: "digest_mismatch", Message: "sha256 did not match manifest"},
			into:    func() any { return &UpdateFailed{} },
		},
		{
			name:    "error",
			msgType: TypeError,
			payload: Error{Code: "unauthorized", Message: "unknown device"},
			into:    func() any { return &Error{} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Encode(tt.msgType, "msg-1", ts, tt.payload)
			require.NoError(t, err)

			env, err := Decode(data)
			require.NoError(t, err)
			assert.Equal(t, tt.msgType, env.Type)
			assert.Equal(t, "msg-1", env.ID)
			assert.True(t, env.TS.Equal(ts))
			assert.True(t, env.Type.Known())

			got := tt.into()
			require.NoError(t, env.DecodePayload(got))

			want, err := json.Marshal(tt.payload)
			require.NoError(t, err)
			gotJSON, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(gotJSON))
		})
	}
}

func TestEncode_NoPayload(t *testing.T) {
	data, err := Encode(TypePing, "", time.Unix(0, 0).UTC(), nil)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "payload")

	env, err := Decode(data)
	require.NoError(t, err)
	assert.Equal(t, TypePing, env.Type)
	assert.ErrorIs(t, env.DecodePayload(&State{}), ErrNoPayload)
}

func TestEncode_EmptyType(t *testing.T) {
	_, err := Encode("", "id", time.Now(), nil)
	assert.ErrorIs(t, err, ErrEmptyMessageType)
}

func TestDecode_UnknownTypeIsNotAnError(t *testing.T) {
	env, err := Decode([]byte(`{"type":"turn.teleport","ts":"2026-08-19T12:00:00Z"}`))
	require.NoError(t, err)
	assert.Equal(t, MessageType("turn.teleport"), env.Type)
	assert.False(t, env.Type.Known(), "a newer peer's message type must decode and be reported unknown")
}

func TestDecode_Errors(t *testing.T) {
	t.Run("malformed json", func(t *testing.T) {
		_, err := Decode([]byte(`{`))
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrEmptyMessageType)
	})

	t.Run("missing type", func(t *testing.T) {
		_, err := Decode([]byte(`{"ts":"2026-08-19T12:00:00Z"}`))
		assert.ErrorIs(t, err, ErrEmptyMessageType)
	})
}

func TestDecodePayload_Malformed(t *testing.T) {
	env := Envelope{Type: TypeState, Payload: []byte(`{"state":`)}
	require.Error(t, env.DecodePayload(&State{}))
}
