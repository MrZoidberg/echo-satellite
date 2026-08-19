package discovery

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

// stubBrowser is a hand-written Browser: the interface has one method and the
// tests need no call recording beyond a counter.
type stubBrowser struct {
	instances []Instance
	err       error
	calls     int
}

func (b *stubBrowser) Browse(context.Context) ([]Instance, error) {
	b.calls++
	return b.instances, b.err
}

func gateway(serverID, host string) Instance {
	return Instance{
		ServerID: serverID,
		Host:     host,
		Port:     DefaultPort,
		TXT:      TXTRecord{Protocol: protocol.ProtocolVersion, ServerID: serverID, TLS: true, Path: DefaultPath},
	}
}

func TestResolve_ExplicitURLWins(t *testing.T) {
	browser := &stubBrowser{instances: []Instance{gateway("other-gateway", "other.local.")}}
	r := NewResolver(browser, protocol.ProtocolVersion)
	paired := gateway("home-gateway", "home.local.")

	endpoint, err := r.Resolve(t.Context(), Config{
		Discovery: ModeMDNS,
		URL:       "wss://192.168.10.20:8770/device",
	}, &paired)

	require.NoError(t, err)
	assert.Equal(t, "wss://192.168.10.20:8770/device", endpoint)
	assert.Zero(t, browser.calls, "an explicit URL must not trigger a browse")
}

func TestResolve_ExplicitURLInvalid(t *testing.T) {
	r := NewResolver(&stubBrowser{}, protocol.ProtocolVersion)

	tests := map[string]string{
		"wrong scheme": "https://gw.local:8770/device",
		"no host":      "wss:///device",
		"unparsable":   "wss://%zz",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := r.Resolve(t.Context(), Config{URL: raw}, nil)
			assert.ErrorIs(t, err, ErrInvalidURL)
		})
	}
}

func TestResolve_PairedGatewayBeforeBrowse(t *testing.T) {
	browser := &stubBrowser{instances: []Instance{gateway("other-gateway", "other.local.")}}
	r := NewResolver(browser, protocol.ProtocolVersion)
	paired := gateway("home-gateway", "home.local.")

	endpoint, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, &paired)

	require.NoError(t, err)
	assert.Equal(t, "wss://home.local.:8770/device", endpoint)
	assert.Zero(t, browser.calls)
}

func TestResolve_PairedGatewayFoundAtNewAddress(t *testing.T) {
	// the paired gateway moved: its stored address is gone, but its server_id is
	// unchanged, so the browse must select it by identity rather than address
	moved := gateway("home-gateway", "")
	moved.Addrs = []netip.Addr{netip.MustParseAddr("192.168.10.55")}
	browser := &stubBrowser{instances: []Instance{
		gateway("guest-gateway", "guest.local."),
		moved,
	}}
	r := NewResolver(browser, protocol.ProtocolVersion)

	endpoint, err := r.Resolve(t.Context(), Config{
		Discovery:         ModeMDNS,
		PreferredServerID: "home-gateway",
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "wss://192.168.10.55:8770/device", endpoint)
	assert.Equal(t, 1, browser.calls)
}

func TestResolve_PreferredServerIDOverridesStalePairing(t *testing.T) {
	browser := &stubBrowser{instances: []Instance{gateway("new-gateway", "new.local.")}}
	r := NewResolver(browser, protocol.ProtocolVersion)
	paired := gateway("old-gateway", "old.local.")

	endpoint, err := r.Resolve(t.Context(), Config{
		Discovery:         ModeMDNS,
		PreferredServerID: "new-gateway",
	}, &paired)

	require.NoError(t, err)
	assert.Equal(t, "wss://new.local.:8770/device", endpoint)
}

func TestResolve_SkipsIncompatibleInstances(t *testing.T) {
	future := gateway("future-gateway", "future.local.")
	future.TXT.Protocol = protocol.ProtocolVersion + 1
	browser := &stubBrowser{instances: []Instance{future, gateway("home-gateway", "home.local.")}}
	r := NewResolver(browser, protocol.ProtocolVersion)

	endpoint, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, nil)

	require.NoError(t, err)
	assert.Equal(t, "wss://home.local.:8770/device", endpoint)
}

func TestResolve_IncompatiblePairedGatewayFallsBackToBrowse(t *testing.T) {
	paired := gateway("home-gateway", "home.local.")
	paired.TXT.Protocol = protocol.ProtocolVersion + 1
	browser := &stubBrowser{instances: []Instance{gateway("guest-gateway", "guest.local.")}}
	r := NewResolver(browser, protocol.ProtocolVersion)

	endpoint, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, &paired)

	require.NoError(t, err)
	assert.Equal(t, "wss://guest.local.:8770/device", endpoint)
}

func TestResolve_NoGateway(t *testing.T) {
	t.Run("browse finds nothing", func(t *testing.T) {
		r := NewResolver(&stubBrowser{}, protocol.ProtocolVersion)
		_, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, nil)
		assert.ErrorIs(t, err, ErrNoGateway)
	})

	t.Run("only incompatible instances", func(t *testing.T) {
		future := gateway("future-gateway", "future.local.")
		future.TXT.Protocol = 99
		r := NewResolver(&stubBrowser{instances: []Instance{future}}, protocol.ProtocolVersion)
		_, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, nil)
		assert.ErrorIs(t, err, ErrNoGateway)
	})

	t.Run("discovery disabled", func(t *testing.T) {
		browser := &stubBrowser{instances: []Instance{gateway("home-gateway", "home.local.")}}
		r := NewResolver(browser, protocol.ProtocolVersion)
		_, err := r.Resolve(t.Context(), Config{Discovery: ModeDisabled}, nil)
		require.ErrorIs(t, err, ErrNoGateway)
		assert.Zero(t, browser.calls, "disabled discovery must never browse")
	})

	t.Run("no browser configured", func(t *testing.T) {
		r := NewResolver(nil, protocol.ProtocolVersion)
		_, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, nil)
		assert.ErrorIs(t, err, ErrNoGateway)
	})
}

func TestResolve_BrowseError(t *testing.T) {
	sentinel := errors.New("multicast unavailable")
	r := NewResolver(&stubBrowser{err: sentinel}, protocol.ProtocolVersion)

	_, err := r.Resolve(t.Context(), Config{Discovery: ModeMDNS}, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}
