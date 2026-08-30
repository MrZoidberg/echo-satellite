package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Discovery modes accepted in Config.
const (
	// ModeMDNS browses the local network when no better candidate exists.
	ModeMDNS = "mdns"
	// ModeDisabled never browses. Only an explicit URL or a paired gateway is used.
	ModeDisabled = "disabled"
)

// Errors returned by Resolve.
var (
	// ErrNoGateway is returned when no gateway endpoint could be determined.
	ErrNoGateway = errors.New("discovery: no gateway found")
	// ErrInvalidURL is returned when the explicitly configured URL is unusable.
	ErrInvalidURL = errors.New("discovery: invalid gateway url")
)

// Config is the satellite side of the gateway settings in docs/DESIGN.md 8.4.
type Config struct {
	// Discovery is ModeMDNS or ModeDisabled. An empty value means ModeMDNS.
	Discovery string
	// URL, when set, is the gateway endpoint and overrides discovery entirely.
	URL string
	// PreferredServerID is the server_id to prefer among discovered gateways.
	// A previously paired gateway is matched by this identity, not by address,
	// so it keeps being preferred after its IP address changes.
	PreferredServerID string
}

// Resolver decides which gateway endpoint a satellite should connect to.
//
// It implements the resolution order in docs/DESIGN.md 8.5: an explicitly
// configured URL always wins, then the previously paired gateway, then an mDNS
// browse. Reachability is not probed here — the caller connects, and on failure
// calls Resolve again without the paired candidate so the browse path is used.
type Resolver struct {
	browser  Browser
	protocol int
}

// NewResolver builds a resolver over the given browser. A nil browser is
// allowed and simply makes the browse step yield nothing, which is the correct
// behavior for a device configured with an explicit URL.
func NewResolver(browser Browser, protocolVersion int) *Resolver {
	return &Resolver{browser: browser, protocol: protocolVersion}
}

// Resolve returns the endpoint URL to connect to. lastPaired may be nil.
func (r *Resolver) Resolve(ctx context.Context, cfg Config, lastPaired *Instance) (string, error) {
	if cfg.URL != "" {
		if err := validateEndpointURL(cfg.URL); err != nil {
			return "", err
		}
		return cfg.URL, nil
	}

	if endpoint, ok := r.fromPaired(cfg, lastPaired); ok {
		return endpoint, nil
	}

	if cfg.Discovery == ModeDisabled {
		return "", fmt.Errorf("%w: discovery disabled and no configured or paired gateway", ErrNoGateway)
	}
	if r.browser == nil {
		return "", fmt.Errorf("%w: no browser configured", ErrNoGateway)
	}

	instances, err := r.browser.Browse(ctx)
	if err != nil {
		return "", fmt.Errorf("discovery: browse %s: %w", ServiceType, err)
	}

	inst, ok := r.pick(instances, cfg.PreferredServerID)
	if !ok {
		return "", fmt.Errorf("%w: browsed %d instance(s), none compatible with protocol %d",
			ErrNoGateway, len(instances), r.protocol)
	}

	endpoint, err := inst.EndpointURL()
	if err != nil {
		return "", err
	}
	return endpoint, nil
}

// fromPaired returns the endpoint of the previously paired gateway when it is
// still usable. A preferred server_id that names a different gateway wins over
// the pairing.
func (r *Resolver) fromPaired(cfg Config, lastPaired *Instance) (string, bool) {
	if lastPaired == nil {
		return "", false
	}
	if cfg.PreferredServerID != "" && lastPaired.ServerID != cfg.PreferredServerID {
		return "", false
	}
	if !lastPaired.Compatible(r.protocol) {
		return "", false
	}
	endpoint, err := lastPaired.EndpointURL()
	if err != nil {
		return "", false
	}
	return endpoint, true
}

// pick chooses among browsed instances: the preferred server_id first, then a
// deterministic protocol-compatible candidate.
func (r *Resolver) pick(instances []Instance, preferredServerID string) (Instance, bool) {
	candidates := make([]Instance, 0, len(instances))
	for _, inst := range instances {
		if !inst.Compatible(r.protocol) || inst.ServerID == "" {
			continue
		}
		candidates = append(candidates, inst)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return instanceOrderKey(candidates[i]) < instanceOrderKey(candidates[j])
	})
	for _, inst := range candidates {
		if preferredServerID == "" || inst.ServerID == preferredServerID {
			return inst, true
		}
	}
	if len(candidates) == 0 {
		return Instance{}, false
	}
	return candidates[0], true
}

func instanceOrderKey(inst Instance) string {
	return inst.ServerID + "\x00" + inst.Host + "\x00" + fmt.Sprintf("%05d", inst.Port)
}

func validateEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrInvalidURL, raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%w %q: no host", ErrInvalidURL, raw)
	}
	if !strings.EqualFold(u.Scheme, "ws") && !strings.EqualFold(u.Scheme, "wss") {
		return fmt.Errorf("%w %q: scheme must be ws or wss", ErrInvalidURL, raw)
	}
	return nil
}
