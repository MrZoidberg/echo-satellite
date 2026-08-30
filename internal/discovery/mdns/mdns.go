// Package mdns implements the discovery DNS-SD interfaces with zeroconf.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/grandcat/zeroconf"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
)

const browseWindow = 3 * time.Second

// Client implements discovery.Advertiser and discovery.Browser. It exposes no
// library-specific types to its callers.
type Client struct{}

// New returns a DNS-SD client.
func New() *Client { return &Client{} }

// Advertise publishes the gateway until ctx is canceled.
func (*Client) Advertise(ctx context.Context, inst discovery.Instance) error {
	if err := inst.Validate(); err != nil {
		return fmt.Errorf("validate mDNS advertisement: %w", err)
	}
	if inst.Port == 0 || !inst.TXT.TLS || inst.TXT.Path != discovery.DefaultPath {
		return errors.New("mDNS advertisement must use TLS and the device endpoint path")
	}
	server, err := register(inst)
	if err != nil {
		return fmt.Errorf("register mDNS service: %w", err)
	}
	defer server.Shutdown()
	<-ctx.Done()
	return fmt.Errorf("mDNS advertisement ended: %w", ctx.Err())
}

// Browse collects compatible DNS-SD entries for a bounded interval. An
// already-canceled context returns promptly without opening multicast sockets.
func (*Client) Browse(ctx context.Context) ([]discovery.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mDNS browse context: %w", err)
	}
	resolver, err := newResolver()
	if err != nil {
		return nil, fmt.Errorf("create mDNS resolver: %w", err)
	}
	deadline, cancel := context.WithTimeout(ctx, browseWindow)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry)
	if err := resolver.Browse(deadline, discovery.ServiceType, discovery.Domain, entries); err != nil {
		return nil, fmt.Errorf("browse mDNS service: %w", err)
	}

	instances := make(map[string]discovery.Instance)
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				return sorted(instances), nil
			}
			if inst, valid := instanceFromEntry(entry); valid {
				if existing, found := instances[inst.ServerID]; found {
					if endpointKey(existing) == endpointKey(inst) {
						inst.Addrs = appendUniqueAddrs(existing.Addrs, inst.Addrs)
					} else if endpointKey(existing) < endpointKey(inst) {
						continue
					}
				}
				instances[inst.ServerID] = inst
			}
		case <-deadline.Done():
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("mDNS browse context: %w", err)
			}
			return sorted(instances), nil
		}
	}
}

type resolver interface {
	Browse(context.Context, string, string, chan<- *zeroconf.ServiceEntry) error
}

type server interface{ Shutdown() }

var newResolver = func() (resolver, error) { return zeroconf.NewResolver() }

var register = func(inst discovery.Instance) (server, error) {
	ips := make([]string, 0, len(inst.Addrs))
	for _, addr := range inst.Addrs {
		ips = append(ips, addr.String())
	}
	return zeroconf.RegisterProxy(
		discovery.InstanceName(inst.ServerID), discovery.ServiceType, discovery.Domain,
		inst.Port, inst.Host, ips, inst.TXT.Encode(), nil,
	)
}

func instanceFromEntry(entry *zeroconf.ServiceEntry) (discovery.Instance, bool) {
	if entry == nil || entry.Port <= 0 || entry.Port > 65535 {
		return discovery.Instance{}, false
	}
	txt, err := discovery.ParseTXT(entry.Text)
	if err != nil {
		return discovery.Instance{}, false
	}
	inst := discovery.Instance{ServerID: txt.ServerID, Host: entry.HostName, Port: entry.Port, TXT: txt}
	inst.Addrs = appendAddrs(inst.Addrs, entry.AddrIPv4)
	inst.Addrs = appendAddrs(inst.Addrs, entry.AddrIPv6)
	if err := inst.Validate(); err != nil {
		return discovery.Instance{}, false
	}
	return inst, true
}

func appendAddrs(dst []netip.Addr, ips []net.IP) []netip.Addr {
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			dst = append(dst, addr.Unmap())
		}
	}
	return dst
}

func appendUniqueAddrs(dst, addrs []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(dst)+len(addrs))
	for _, addr := range dst {
		seen[addr] = struct{}{}
	}
	for _, addr := range addrs {
		if _, found := seen[addr]; found {
			continue
		}
		seen[addr] = struct{}{}
		dst = append(dst, addr)
	}
	return dst
}

func endpointKey(inst discovery.Instance) string {
	return inst.Host + "\x00" + fmt.Sprintf("%05d", inst.Port)
}

func sorted(instances map[string]discovery.Instance) []discovery.Instance {
	keys := make([]string, 0, len(instances))
	for key := range instances {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]discovery.Instance, 0, len(keys))
	for _, key := range keys {
		result = append(result, instances[key])
	}
	return result
}

var _ discovery.Advertiser = (*Client)(nil)
var _ discovery.Browser = (*Client)(nil)
