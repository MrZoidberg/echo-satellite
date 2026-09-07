// Package mdns implements the discovery DNS-SD interfaces with zeroconf.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/betamos/zeroconf"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
)

const browseWindow = 3 * time.Second

// Client implements discovery.Advertiser and discovery.Browser. It exposes no
// library-specific types to its callers.
type Client struct {
	deviceBrowse bool
}

// New returns a DNS-SD client.
func New() *Client { return &Client{} }

// NewDevice returns a client whose browser is confined to FireOS infrastructure
// Wi-Fi. Host tools retain zeroconf's normal all-interface behavior through New.
func NewDevice() *Client { return &Client{deviceBrowse: true} }

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
func (c *Client) Browse(ctx context.Context) ([]discovery.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mDNS browse context: %w", err)
	}
	resolverFactory := newResolver
	if c.deviceBrowse {
		resolverFactory = newDeviceResolver
	}
	resolver, err := resolverFactory()
	if err != nil {
		return nil, fmt.Errorf("create mDNS resolver: %w", err)
	}
	deadline, cancel := context.WithTimeout(ctx, browseWindow)
	defer cancel()
	entries := make(chan *serviceEntry)
	errCh := make(chan error, 1)
	go func() {
		errCh <- resolver.Browse(deadline, discovery.ServiceType, discovery.Domain, entries)
	}()

	instances := make(map[string]discovery.Instance)
	entriesByName := make(map[string]discovery.Instance)
	seen := 0
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				entries = nil
				continue
			}
			seen++
			if entry != nil && entry.Removed {
				serverID := entriesByName[entry.Name].ServerID
				delete(entriesByName, entry.Name)
				rebuildInstances(instances, entriesByName)
				slog.Debug("removed mDNS gateway", "instance", entry.Name, "server_id", serverID)
				continue
			}
			inst, valid := instanceFromEntry(entry)
			if !valid {
				slog.Debug("ignored incompatible mDNS response", "host", entryHost(entry), "port", entryPort(entry), "address_count", entryAddressCount(entry))
				continue
			}
			slog.Debug("accepted mDNS gateway", "server_id", inst.ServerID, "host", inst.Host, "port", inst.Port, "address_count", len(inst.Addrs), "preferred_address", firstAddress(inst.Addrs))
			entriesByName[entryKey(entry, inst)] = inst
			rebuildInstances(instances, entriesByName)
		case err := <-errCh:
			if err != nil {
				return nil, fmt.Errorf("browse mDNS service: %w", err)
			}
			slog.Debug("mDNS browse completed", "responses", seen, "compatible_gateways", len(instances))
			return sorted(instances), nil
		case <-deadline.Done():
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("mDNS browse context: %w", err)
			}
			slog.Debug("mDNS browse window ended", "responses", seen, "compatible_gateways", len(instances))
			return sorted(instances), nil
		}
	}
}

func entryKey(entry *serviceEntry, inst discovery.Instance) string {
	if entry.Name != "" {
		return entry.Name
	}
	addresses := make([]string, 0, len(inst.Addrs))
	for _, addr := range inst.Addrs {
		addresses = append(addresses, addr.String())
	}
	return inst.ServerID + "\x00" + endpointKey(inst) + "\x00" + strings.Join(addresses, ",")
}

func rebuildInstances(instances, entriesByName map[string]discovery.Instance) {
	clear(instances)
	for _, inst := range entriesByName {
		mergeInstance(instances, inst)
	}
}

func mergeInstance(instances map[string]discovery.Instance, inst discovery.Instance) {
	existing, found := instances[inst.ServerID]
	if !found {
		instances[inst.ServerID] = inst
		return
	}
	if endpointKey(existing) == endpointKey(inst) {
		inst.Addrs = appendUniqueAddrs(existing.Addrs, inst.Addrs)
		instances[inst.ServerID] = inst
		return
	}
	if endpointKey(existing) > endpointKey(inst) {
		instances[inst.ServerID] = inst
	}
}

func firstAddress(addrs []netip.Addr) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0].String()
}

type resolver interface {
	Browse(context.Context, string, string, chan<- *serviceEntry) error
}

type serviceEntry struct {
	Name     string
	Removed  bool
	HostName string
	Port     int
	Text     []string
	AddrIPv4 []net.IP
	AddrIPv6 []net.IP
}

type forkResolver struct{ interfaces []net.Interface }

func (r forkResolver) Browse(ctx context.Context, service, domain string, entries chan<- *serviceEntry) error {
	interfaceNames := make([]string, 0, len(r.interfaces))
	for _, iface := range r.interfaces {
		interfaceNames = append(interfaceNames, iface.Name)
	}
	slog.Debug("starting mDNS browse", "service", service, "domain", domain, "interfaces", interfaceNames)
	typeName := zeroconf.NewType(service + "." + strings.TrimSuffix(domain, "."))
	client := zeroconf.New()
	if len(r.interfaces) > 0 {
		client.Interfaces(func() ([]net.Interface, error) { return r.interfaces, nil })
	}
	client.Browse(func(event zeroconf.Event) {
		if event.Service == nil {
			return
		}
		slog.Debug("received raw mDNS service", "host", event.Hostname, "port", event.Port, "address_count", len(event.Addrs), "txt_field_count", len(event.Text))
		entry := &serviceEntry{Name: event.Name, Removed: event.Op == zeroconf.OpRemoved, HostName: event.Hostname, Port: int(event.Port), Text: append([]string(nil), event.Text...)}
		for _, addr := range event.Addrs {
			if addr.Is4() {
				entry.AddrIPv4 = append(entry.AddrIPv4, net.IP(addr.AsSlice()))
			} else if addr.Is6() {
				entry.AddrIPv6 = append(entry.AddrIPv6, net.IP(addr.AsSlice()))
			}
		}
		entry.AddrIPv4 = preferInterfaceSubnets(entry.AddrIPv4, r.interfaces)
		entry.AddrIPv6 = preferInterfaceSubnets(entry.AddrIPv6, r.interfaces)
		select {
		case entries <- entry:
		case <-ctx.Done():
		}
	}, typeName)
	opened, err := client.Open()
	if err != nil {
		return fmt.Errorf("open mDNS client: %w", err)
	}
	defer opened.Close()
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return fmt.Errorf("mDNS browse context: %w", ctx.Err())
}

func preferInterfaceSubnets(addrs []net.IP, interfaces []net.Interface) []net.IP {
	prefixes := make([]*net.IPNet, 0)
	for _, iface := range interfaces {
		ifaceAddrs, err := addressesForInterface(iface)
		if err != nil {
			continue
		}
		for _, raw := range ifaceAddrs {
			_, prefix, parseErr := net.ParseCIDR(raw.String())
			if parseErr == nil {
				prefixes = append(prefixes, prefix)
			}
		}
	}
	preferred := make([]net.IP, 0, len(addrs))
	other := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		matched := false
		for _, prefix := range prefixes {
			if prefix.Contains(addr) {
				matched = true
				break
			}
		}
		if matched {
			preferred = append(preferred, addr)
		} else {
			other = append(other, addr)
		}
	}
	return append(preferred, other...)
}

func entryHost(entry *serviceEntry) string {
	if entry == nil {
		return ""
	}
	return entry.HostName
}

func entryPort(entry *serviceEntry) int {
	if entry == nil {
		return 0
	}
	return entry.Port
}

func entryAddressCount(entry *serviceEntry) int {
	if entry == nil {
		return 0
	}
	return len(entry.AddrIPv4) + len(entry.AddrIPv6)
}

type server interface{ Shutdown() }

var interfaceByName = net.InterfaceByName

var newResolver = func() (resolver, error) {
	return createResolver(nil)
}

var newDeviceResolver = func() (resolver, error) {
	return createResolver(preferredInterfaces())
}

var createResolver = func(interfaces []net.Interface) (resolver, error) {
	return forkResolver{interfaces: interfaces}, nil
}

// preferredInterface limits FireOS discovery to its infrastructure Wi-Fi
// adapter. The Dot's simultaneously-up p2p0 adapter rejects multicast sends.
// Hosts without a usable wlan0 retain the library's normal interface selection.
func preferredInterfaces() []net.Interface {
	iface, err := interfaceByName("wlan0")
	if err != nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
		return nil
	}
	return []net.Interface{*iface}
}

var register = func(inst discovery.Instance) (server, error) {
	targets, err := advertisementTargets(inst.Addrs)
	if err != nil {
		return nil, fmt.Errorf("select mDNS advertisement target: %w", err)
	}
	servers := make([]server, 0, len(targets))
	for _, target := range targets {
		published, publishErr := registerProxy(
			discovery.InstanceName(inst.ServerID), discovery.ServiceType, discovery.Domain,
			inst.Port, proxyHost(inst.Host), target.ips, inst.TXT.Encode(), []net.Interface{target.iface},
		)
		if publishErr != nil {
			for _, active := range servers {
				active.Shutdown()
			}
			return nil, fmt.Errorf("publish mDNS service on interface %s: %w", target.iface.Name, publishErr)
		}
		servers = append(servers, published)
	}
	return multipleServers(servers), nil
}

var registerProxy = func(instance string, service string, domain string, port int, host string, ips []string, text []string, ifaces []net.Interface) (server, error) {
	addrs := make([]netip.Addr, 0, len(ips))
	for _, raw := range ips {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, fmt.Errorf("parse advertisement address %q: %w", raw, err)
		}
		addrs = append(addrs, addr)
	}
	client := zeroconf.New().Interfaces(func() ([]net.Interface, error) { return ifaces, nil }).Publish(&zeroconf.Service{
		Type: zeroconf.NewType(service + "." + strings.TrimSuffix(domain, ".")), Name: instance,
		Port: uint16(port), Hostname: forkHostname(host), Addrs: addrs, Text: text,
	})
	opened, err := client.Open()
	if err != nil {
		return nil, fmt.Errorf("open mDNS client: %w", err)
	}
	return openedServer{client: opened}, nil
}

type openedServer struct{ client *zeroconf.Client }

type multipleServers []server

func forkHostname(host string) string { return host + ".local" }

func (s openedServer) Shutdown() { _ = s.client.Close() }

func (s multipleServers) Shutdown() {
	for _, active := range s {
		active.Shutdown()
	}
}

var listInterfaces = net.Interfaces

var addressesForInterface = func(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

type advertisementTarget struct {
	iface net.Interface
	ips   []string
}

// advertisementTargets selects every usable multicast interface and confines
// each proxy record to addresses assigned to that specific interface.
func advertisementTargets(configured []netip.Addr) ([]advertisementTarget, error) {
	interfaces, err := listInterfaces()
	if err != nil {
		return nil, err
	}
	targets := make([]advertisementTarget, 0, len(interfaces))
	assigned := make(map[netip.Addr]int)
	for _, iface := range interfaces {
		if !usableMulticastInterface(iface) {
			continue
		}
		interfaceAddrs, addrErr := interfaceAdvertiseAddresses(iface)
		if addrErr != nil {
			continue
		}
		if len(interfaceAddrs) == 0 {
			continue
		}
		target := advertisementTarget{iface: iface, ips: make([]string, 0, len(interfaceAddrs))}
		for _, addr := range interfaceAddrs {
			assigned[addr] = len(targets)
			target.ips = append(target.ips, addr.String())
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, errors.New("no usable multicast interface for advertisement")
	}
	if len(configured) == 0 {
		return targets, nil
	}
	configuredTargets := make([]advertisementTarget, len(targets))
	for _, addr := range configured {
		index, found := assigned[addr.Unmap()]
		if !usableAddress(addr) || !found {
			return nil, fmt.Errorf("configured advertisement address %s is not assigned to a selected multicast interface", addr)
		}
		if configuredTargets[index].iface.Name == "" {
			configuredTargets[index].iface = targets[index].iface
		}
		configuredTargets[index].ips = append(configuredTargets[index].ips, addr.String())
	}
	result := make([]advertisementTarget, 0, len(configuredTargets))
	for _, target := range configuredTargets {
		if len(target.ips) > 0 {
			result = append(result, target)
		}
	}
	return result, nil
}

func usableMulticastInterface(iface net.Interface) bool {
	return iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagMulticast != 0 && iface.Flags&net.FlagLoopback == 0
}

func interfaceAdvertiseAddresses(iface net.Interface) ([]netip.Addr, error) {
	ifaceAddrs, err := addressesForInterface(iface)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.Addr, 0, len(ifaceAddrs))
	for _, ifaceAddr := range ifaceAddrs {
		ip, _, parseErr := net.ParseCIDR(ifaceAddr.String())
		if parseErr != nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		if ok && usableAddress(addr.Unmap()) {
			addrs = append(addrs, addr.Unmap())
		}
	}
	return addrs, nil
}

func usableAddress(addr netip.Addr) bool {
	return addr.IsValid() && !addr.IsLoopback() && !addr.IsUnspecified() && !addr.IsLinkLocalUnicast()
}

// proxyHost returns the bare label RegisterProxy requires. That library adds
// the DNS-SD domain itself; passing a .local host makes it publish .local.local.
func proxyHost(host string) string {
	host = strings.TrimSuffix(host, ".")
	if len(host) >= len(".local") && strings.EqualFold(host[len(host)-len(".local"):], ".local") {
		return host[:len(host)-len(".local")]
	}
	return host
}

func instanceFromEntry(entry *serviceEntry) (discovery.Instance, bool) {
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
		if _, found := seen[addr]; !found {
			seen[addr] = struct{}{}
			dst = append(dst, addr)
		}
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
