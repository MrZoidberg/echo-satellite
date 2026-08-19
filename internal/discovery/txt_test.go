package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestTXTRecord_EncodeParseRoundTrip(t *testing.T) {
	rec := TXTRecord{Protocol: protocol.ProtocolVersion, ServerID: "home-gateway", TLS: true, Path: DefaultPath}

	entries := rec.Encode()
	assert.Equal(t, []string{
		"protocol=1",
		"server_id=home-gateway",
		"tls=1",
		"path=/device",
	}, entries)

	got, err := ParseTXT(entries)
	require.NoError(t, err)
	assert.Equal(t, rec, got)
}

func TestTXTRecord_EncodeDefaults(t *testing.T) {
	entries := TXTRecord{Protocol: 1, ServerID: "gw"}.Encode()
	assert.Contains(t, entries, "tls=0")
	assert.Contains(t, entries, "path="+DefaultPath)
}

func TestParseTXT_RejectsSecretLikeKeys(t *testing.T) {
	for _, key := range []string{"token", "api_key", "shared_secret", "password", "psk", "authz", "credential"} {
		t.Run(key, func(t *testing.T) {
			_, err := ParseTXT([]string{"protocol=1", "server_id=gw", key + "=hunter2"})
			assert.ErrorIs(t, err, ErrSecretInTXT)
		})
	}
}

func TestParseTXT_IgnoresUnknownKeys(t *testing.T) {
	rec, err := ParseTXT([]string{"protocol=1", "server_id=gw", "region=kitchen", "bare-flag"})
	require.NoError(t, err)
	assert.Equal(t, "gw", rec.ServerID)
	assert.Equal(t, 1, rec.Protocol)
}

func TestParseTXT_TLSVariants(t *testing.T) {
	tests := map[string]bool{"tls=1": true, "tls=true": true, "tls=TRUE": true, "tls=0": false, "tls=no": false}
	for entry, want := range tests {
		t.Run(entry, func(t *testing.T) {
			rec, err := ParseTXT([]string{"protocol=1", "server_id=gw", entry})
			require.NoError(t, err)
			assert.Equal(t, want, rec.TLS)
		})
	}
}

func TestParseTXT_InvalidProtocol(t *testing.T) {
	_, err := ParseTXT([]string{"protocol=one", "server_id=gw"})
	assert.ErrorIs(t, err, ErrInvalidProtocol)
}

func TestTXTRecord_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, TXTRecord{Protocol: 1, ServerID: "gw", Path: "/device"}.Validate())
	})

	t.Run("missing server id", func(t *testing.T) {
		assert.ErrorIs(t, TXTRecord{Protocol: 1}.Validate(), ErrMissingServerID)
	})

	t.Run("no protocol", func(t *testing.T) {
		assert.ErrorIs(t, TXTRecord{ServerID: "gw"}.Validate(), ErrInvalidProtocol)
	})

	t.Run("relative path", func(t *testing.T) {
		require.Error(t, TXTRecord{Protocol: 1, ServerID: "gw", Path: "device"}.Validate())
	})
}
