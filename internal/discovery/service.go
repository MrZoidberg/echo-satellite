// Package discovery defines the local DNS-SD contract between gateways and
// satellites, and the order in which a satellite picks the gateway to connect
// to.
//
// mDNS locates endpoints; it is not authentication. TXT records carry
// discovery metadata only and never credentials, and a discovered gateway is
// still required to pass TLS and device authentication before it is trusted.
//
// The Advertiser and Browser interfaces keep resolution testable without a
// multicast network. Their concrete DNS-SD implementation lives in mdns.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
)

// DNS-SD identifiers for the satellite protocol.
const (
	// ServiceType is the service a gateway advertises.
	ServiceType = "_echo-satellite._tcp"
	// DeviceServiceType is the optional service a device advertises for
	// provisioning and diagnostics. It carries non-sensitive metadata only.
	DeviceServiceType = "_echo-satellite-device._tcp"
	// Domain is the mDNS domain both services live in.
	Domain = "local."
	// DefaultPort is the gateway device endpoint port.
	DefaultPort = 8770
	// DefaultPath is the device endpoint path used when TXT omits one.
	DefaultPath = "/device"
)

// InstanceName returns the DNS-SD instance name a gateway advertises itself
// under, for example "echo-satellite-home-gateway".
func InstanceName(serverID string) string { return "echo-satellite-" + serverID }

// Instance is one discovered gateway.
type Instance struct {
	ServerID string
	Host     string
	Port     int
	Addrs    []netip.Addr
	TXT      TXTRecord
}

// ErrNoEndpoint is returned when an instance carries neither a host name nor an
// address, so no endpoint URL can be built from it.
var ErrNoEndpoint = errors.New("discovery: instance has no host or address")

// EndpointURL builds the device WebSocket URL for the instance. The host name
// is preferred over a resolved address so a gateway keeps working when its IP
// address changes.
func (i Instance) EndpointURL() (string, error) {
	host := i.Host
	if host == "" && len(i.Addrs) > 0 {
		host = i.Addrs[0].String()
	}
	if host == "" {
		return "", ErrNoEndpoint
	}

	port := i.Port
	if port == 0 {
		port = DefaultPort
	}

	scheme := "ws"
	if i.TXT.TLS {
		scheme = "wss"
	}

	path := i.TXT.Path
	if path == "" {
		path = DefaultPath
	}

	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	return u.String(), nil
}

// Compatible reports whether the instance speaks a protocol version this build
// can talk to.
func (i Instance) Compatible(protocolVersion int) bool {
	return i.TXT.Protocol == protocolVersion
}

// Advertiser publishes a gateway instance on the local network.
//
// The interface keeps composition roots and their tests independent from the
// selected DNS-SD library.
type Advertiser interface {
	Advertise(ctx context.Context, inst Instance) error
}

// Browser finds gateway instances on the local network.
type Browser interface {
	Browse(ctx context.Context) ([]Instance, error)
}

// Validate checks that an instance can be advertised or connected to.
func (i Instance) Validate() error {
	if i.ServerID == "" {
		return fmt.Errorf("discovery: instance %q: %w", i.Host, ErrMissingServerID)
	}
	if i.Port < 0 || i.Port > 65535 {
		return fmt.Errorf("discovery: instance %q has invalid port %d", i.Host, i.Port)
	}
	if _, err := i.EndpointURL(); err != nil {
		return err
	}
	if err := i.TXT.Validate(); err != nil {
		return err
	}
	if i.ServerID != i.TXT.ServerID {
		return fmt.Errorf("discovery: instance server_id %q does not match TXT server_id %q", i.ServerID, i.TXT.ServerID)
	}
	return nil
}
