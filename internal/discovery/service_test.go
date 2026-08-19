package discovery

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestInstanceName(t *testing.T) {
	assert.Equal(t, "echo-satellite-home-gateway", InstanceName("home-gateway"))
}

func TestInstance_EndpointURL(t *testing.T) {
	tests := []struct {
		name string
		inst Instance
		want string
	}{
		{
			name: "host and tls",
			inst: Instance{Host: "echo-gateway.local.", Port: 8770, TXT: TXTRecord{TLS: true, Path: "/device"}},
			want: "wss://echo-gateway.local.:8770/device",
		},
		{
			name: "plaintext",
			inst: Instance{Host: "gw.local.", Port: 9000, TXT: TXTRecord{Path: "/device"}},
			want: "ws://gw.local.:9000/device",
		},
		{
			name: "defaults for port and path",
			inst: Instance{Host: "gw.local.", TXT: TXTRecord{TLS: true}},
			want: "wss://gw.local.:8770/device",
		},
		{
			name: "falls back to address",
			inst: Instance{Addrs: []netip.Addr{netip.MustParseAddr("192.168.10.20")}, TXT: TXTRecord{TLS: true}},
			want: "wss://192.168.10.20:8770/device",
		},
		{
			name: "ipv6 address is bracketed",
			inst: Instance{Addrs: []netip.Addr{netip.MustParseAddr("fe80::1")}, TXT: TXTRecord{TLS: true}},
			want: "wss://[fe80::1]:8770/device",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.inst.EndpointURL()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstance_EndpointURL_NoHost(t *testing.T) {
	_, err := Instance{ServerID: "gw"}.EndpointURL()
	assert.ErrorIs(t, err, ErrNoEndpoint)
}

func TestInstance_Compatible(t *testing.T) {
	inst := Instance{TXT: TXTRecord{Protocol: protocol.ProtocolVersion}}
	assert.True(t, inst.Compatible(protocol.ProtocolVersion))
	assert.False(t, inst.Compatible(protocol.ProtocolVersion+1))
}

func TestInstance_Validate(t *testing.T) {
	valid := Instance{
		ServerID: "home-gateway",
		Host:     "echo-gateway.local.",
		Port:     DefaultPort,
		TXT:      TXTRecord{Protocol: protocol.ProtocolVersion, ServerID: "home-gateway", TLS: true, Path: DefaultPath},
	}
	require.NoError(t, valid.Validate())

	t.Run("missing server id", func(t *testing.T) {
		inst := valid
		inst.ServerID = ""
		assert.ErrorIs(t, inst.Validate(), ErrMissingServerID)
	})

	t.Run("no endpoint", func(t *testing.T) {
		inst := valid
		inst.Host = ""
		assert.ErrorIs(t, inst.Validate(), ErrNoEndpoint)
	})

	t.Run("bad txt", func(t *testing.T) {
		inst := valid
		inst.TXT.Protocol = 0
		assert.ErrorIs(t, inst.Validate(), ErrInvalidProtocol)
	})
}
