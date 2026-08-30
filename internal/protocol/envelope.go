// Package protocol defines the Echo Satellite wire contract between a device
// agent (echod or dotsim) and the gateway. It is the single shared definition
// of message types, payloads, announced capabilities and update phases.
//
// # Framing
//
// The transport is one long-lived outbound secure WebSocket per device.
// Control and event messages travel as JSON text frames carrying an Envelope.
// Audio travels as raw PCM in binary WebSocket frames, and is only valid
// between audio.start and audio.stop (device to gateway) or between play.start
// and play.stop (gateway to device). No audio is exchanged outside those
// windows: the connection may stay idle for hours while the device listens
// locally for its wake word.
//
// # Boundaries
//
// Wake detection, including the VAD that gates wake candidates, is device-local
// and never crosses this protocol. turn.start is always produced by the device,
// carrying the scores the device already computed; nothing in this package lets
// a gateway score a wake word or receive idle microphone audio.
//
// Feature behavior is negotiated by capability announcement in hello (see
// Capabilities), never by comparing agent versions. No "version >= X" helper
// belongs in this package; version minimums exist only in release manifests as
// installation-safety constraints.
//
// The reference document for this package is docs/protocol.md, which must be
// updated in the same change as any wire change here.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProtocolVersion is the wire protocol version announced in hello and welcome
// and advertised in the mDNS TXT record.
const ProtocolVersion = 1

// Errors returned when encoding or decoding an envelope.
var (
	// ErrEmptyMessageType is returned when an envelope carries no message type.
	ErrEmptyMessageType = errors.New("protocol: empty message type")
	// ErrNoPayload is returned when decoding the payload of an envelope that has none.
	ErrNoPayload = errors.New("protocol: envelope has no payload")
	// ErrMissingCorrelationID is returned for messages that must identify a turn or config revision.
	ErrMissingCorrelationID = errors.New("protocol: missing correlation id")
	// ErrNoRequiredPayload is returned for a message whose payload must be present.
	ErrNoRequiredPayload = errors.New("protocol: missing required payload")
)

// Envelope is the JSON control frame every non-audio message travels in.
type Envelope struct {
	Type    MessageType     `json:"type"`
	ID      string          `json:"id,omitempty"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Encode marshals a control frame. A nil payload produces an envelope with no
// payload field, which is correct for messages such as ping and audio.stop.
func Encode(msgType MessageType, id string, ts time.Time, payload any) ([]byte, error) {
	if msgType == "" {
		return nil, ErrEmptyMessageType
	}
	if requiresCorrelationID(msgType) && strings.TrimSpace(id) == "" {
		return nil, ErrMissingCorrelationID
	}
	if requiresPayload(msgType) && payload == nil {
		return nil, ErrNoRequiredPayload
	}
	if validatable, ok := payload.(interface{ Validate() error }); ok {
		if err := validatable.Validate(); err != nil {
			return nil, fmt.Errorf("protocol: validate %s payload: %w", msgType, err)
		}
	}

	env := Envelope{Type: msgType, ID: id, TS: ts}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("protocol: marshal %s payload: %w", msgType, err)
		}
		env.Payload = raw
	}

	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("protocol: marshal %s envelope: %w", msgType, err)
	}
	return data, nil
}

// Decode unmarshals a control frame. An unrecognized message type is not an
// error: the envelope is returned with Type.Known reporting false, so a peer
// speaking a newer protocol can be ignored message by message rather than
// dropping the connection.
func Decode(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("protocol: unmarshal envelope: %w", err)
	}
	if env.Type == "" {
		return Envelope{}, ErrEmptyMessageType
	}
	if requiresCorrelationID(env.Type) && strings.TrimSpace(env.ID) == "" {
		return Envelope{}, ErrMissingCorrelationID
	}
	if requiresPayload(env.Type) && len(env.Payload) == 0 {
		return Envelope{}, ErrNoRequiredPayload
	}
	return env, nil
}

// DecodePayload unmarshals the envelope payload into v.
func (e Envelope) DecodePayload(v any) error {
	if len(e.Payload) == 0 {
		return ErrNoPayload
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("protocol: unmarshal %s payload: %w", e.Type, err)
	}
	if validatable, ok := v.(interface{ Validate() error }); ok {
		if err := validatable.Validate(); err != nil {
			return fmt.Errorf("protocol: validate %s payload: %w", e.Type, err)
		}
	}
	return nil
}

func requiresCorrelationID(msgType MessageType) bool {
	return msgType == TypeTurnStart || msgType == TypeAudioStart || msgType == TypeAudioStop || msgType == TypeConfig
}

func requiresPayload(msgType MessageType) bool {
	switch msgType {
	case TypeWelcome, TypeConfig, TypeConfigResult, TypeLog, TypeAudioStop:
		return true
	default:
		return false
	}
}
