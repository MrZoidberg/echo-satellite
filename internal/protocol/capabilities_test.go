package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCapabilities_SortsAndDeduplicates(t *testing.T) {
	caps := NewCapabilities(CapUpdateAB, CapWakeLocal, CapUpdateAB, CapAudioCapture)
	assert.Equal(t, Capabilities{CapAudioCapture, CapUpdateAB, CapWakeLocal}, caps)

	same := NewCapabilities(CapWakeLocal, CapAudioCapture, CapUpdateAB)
	first, err := json.Marshal(caps)
	require.NoError(t, err)
	second, err := json.Marshal(same)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "same feature set must produce identical wire bytes")
}

func TestNewCapabilities_Empty(t *testing.T) {
	caps := NewCapabilities()
	assert.Empty(t, caps)

	data, err := json.Marshal(caps)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(data))
}

func TestCapabilities_Has(t *testing.T) {
	caps := NewCapabilities(CapWakeLocal, CapAudioCapture, CapLED)
	assert.True(t, caps.Has(CapWakeLocal))
	assert.True(t, caps.Has(CapLED))
	assert.False(t, caps.Has(CapUpdateAB))
	assert.False(t, Capabilities(nil).Has(CapWakeLocal))
}

func TestCapabilities_JSONRoundTrip(t *testing.T) {
	caps := NewCapabilities(CapWakeLocal, CapUpdateAB)
	data, err := json.Marshal(caps)
	require.NoError(t, err)
	assert.JSONEq(t, `["update.ab","wake.local"]`, string(data))

	var got Capabilities
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, caps, got)
}
