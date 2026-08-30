package mdns

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/grandcat/zeroconf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

type stubResolver struct {
	entries []*zeroconf.ServiceEntry
	err     error
	started chan<- struct{}
	block   bool
}

func (r stubResolver) Browse(_ context.Context, _, _ string, entries chan<- *zeroconf.ServiceEntry) error {
	if r.err != nil {
		return r.err
	}
	if r.started != nil {
		r.started <- struct{}{}
	}
	if r.block {
		return nil
	}
	go func() {
		defer close(entries)
		for _, entry := range r.entries {
			entries <- entry
		}
	}()
	return nil
}

type stubServer struct{ stopped bool }

func (s *stubServer) Shutdown() { s.stopped = true }

func TestBrowseConvertsFiltersDeduplicatesAndSorts(t *testing.T) {
	restore := swapResolver(t, stubResolver{entries: []*zeroconf.ServiceEntry{
		entry("home", "gateway.local.", "192.168.1.3", "fe80::1"),
		entry("guest", "guest.local.", "192.168.1.4"),
		entry("home", "gateway.local.", "192.168.1.3"),
		entry("home", "old-gateway.local.", "192.168.1.5"),
		{HostName: "bad.local.", Port: 8770, Text: []string{"protocol=1", "server_id=bad", "token=leak"}},
		{HostName: "bad.local.", Port: 0, Text: []string{"protocol=1", "server_id=bad"}},
	}})
	defer restore()

	instances, err := New().Browse(t.Context())
	require.NoError(t, err)
	require.Len(t, instances, 2)
	assert.Equal(t, "guest", instances[0].ServerID)
	assert.Equal(t, "home", instances[1].ServerID)
	assert.Len(t, instances[1].Addrs, 2)
	assert.Equal(t, "192.168.1.3", instances[1].Addrs[0].String())
	assert.Equal(t, "fe80::1", instances[1].Addrs[1].String())
}

func TestBrowseCanceledPromptly(t *testing.T) {
	t.Run("before socket setup", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		instances, err := New().Browse(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, instances)
	})
	t.Run("during browse", func(t *testing.T) {
		started := make(chan struct{}, 1)
		restore := swapResolver(t, stubResolver{started: started, block: true})
		defer restore()
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := New().Browse(ctx)
			result <- err
		}()
		<-started
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
	})
}

func TestBrowseWrapsResolverError(t *testing.T) {
	restore := swapResolver(t, stubResolver{err: errors.New("multicast unavailable")})
	defer restore()
	_, err := New().Browse(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "multicast unavailable")
}

func TestAdvertiseUsesOnlyDiscoveryMetadataAndStops(t *testing.T) {
	old := register
	defer func() { register = old }()
	var got discovery.Instance
	stub := &stubServer{}
	register = func(inst discovery.Instance) (server, error) {
		got = inst
		return stub, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := New().Advertise(ctx, testInstance())
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, stub.stopped)
	assert.Equal(t, []string{"protocol=1", "server_id=home", "tls=1", "path=/device"}, got.TXT.Encode())
}

func TestAdvertiseRejectsInsecureOrNonDeviceEndpoint(t *testing.T) {
	tests := map[string]discovery.Instance{
		"plaintext":  func() discovery.Instance { inst := testInstance(); inst.TXT.TLS = false; return inst }(),
		"wrong path": func() discovery.Instance { inst := testInstance(); inst.TXT.Path = "/other"; return inst }(),
	}
	for name, inst := range tests {
		t.Run(name, func(t *testing.T) {
			err := New().Advertise(t.Context(), inst)
			require.Error(t, err)
		})
	}
}

func entry(serverID, host string, ips ...string) *zeroconf.ServiceEntry {
	entry := &zeroconf.ServiceEntry{HostName: host, Port: 8770, Text: []string{
		"protocol=1", "server_id=" + serverID, "tls=1", "path=/device",
	}}
	for _, raw := range ips {
		ip := net.ParseIP(raw)
		if ip.To4() != nil {
			entry.AddrIPv4 = append(entry.AddrIPv4, ip)
		} else {
			entry.AddrIPv6 = append(entry.AddrIPv6, ip)
		}
	}
	return entry
}

func testInstance() discovery.Instance {
	return discovery.Instance{ServerID: "home", Host: "gateway.local.", Port: 8770,
		TXT: discovery.TXTRecord{Protocol: protocol.ProtocolVersion, ServerID: "home", TLS: true, Path: discovery.DefaultPath}}
}

func swapResolver(t *testing.T, value resolver) func() {
	t.Helper()
	old := newResolver
	newResolver = func() (resolver, error) { return value, nil }
	return func() { newResolver = old }
}
