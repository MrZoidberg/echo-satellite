package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

// opts is the gateway command line.
type opts struct {
	Config          string `long:"config" env:"GATEWAY_CONFIG" description:"path to the gateway ini config file" no-ini:"true"`
	Listen          string `long:"listen" env:"GATEWAY_LISTEN" default:":8770" description:"device endpoint listen address"`
	ServerID        string `long:"server-id" env:"GATEWAY_SERVER_ID" default:"echo-gateway" description:"stable gateway identity advertised over mdns"`
	Hostname        string `long:"hostname" env:"GATEWAY_HOSTNAME" default:"echo-gateway.local." description:"host name advertised over mdns"`
	Path            string `long:"device-path" env:"GATEWAY_DEVICE_PATH" default:"/device" description:"device endpoint path"`
	TLSCert         string `long:"tls-cert" env:"GATEWAY_TLS_CERT" description:"TLS certificate PEM for the device endpoint"`
	TLSKey          string `long:"tls-key" env:"GATEWAY_TLS_KEY" description:"TLS private-key PEM for the device endpoint"`
	DeviceTokenFile string `long:"device-token-file" env:"GATEWAY_DEVICE_TOKEN_FILE" description:"file containing the device bearer token"`
	DeviceConfig    string `long:"device-config" env:"GATEWAY_DEVICE_CONFIG" description:"strict TOML device configuration profile"`
	NoMDNS          bool   `long:"no-mdns" env:"GATEWAY_NO_MDNS" description:"disable gateway mDNS advertisement"`
	DiagnosticWAV   string `long:"diagnostic-wav-dir" env:"GATEWAY_DIAGNOSTIC_WAV_DIR" description:"optional directory for completed diagnostic turn WAV files"`

	AllowUnsignedDevBuilds bool `long:"allow-unsigned-dev-builds" env:"GATEWAY_ALLOW_UNSIGNED_DEV_BUILDS" description:"accept releases with no signature; development only"`

	Dbg     bool `long:"dbg" env:"DEBUG" description:"debug logging" no-ini:"true"`
	Version bool `long:"version" short:"V" description:"show version and exit" no-ini:"true"`
}

// advertisement builds the mDNS instance this gateway would publish. TXT
// carries discovery metadata only; nothing secret is ever advertised.
func (o opts) advertisement() (discovery.Instance, error) {
	port, err := listenPort(o.Listen)
	if err != nil {
		return discovery.Instance{}, err
	}

	inst := discovery.Instance{
		ServerID: o.ServerID,
		Host:     o.Hostname,
		Port:     port,
		TXT: discovery.TXTRecord{
			Protocol: protocol.ProtocolVersion,
			ServerID: o.ServerID,
			TLS:      true,
			Path:     o.Path,
		},
	}
	if err := inst.Validate(); err != nil {
		return discovery.Instance{}, fmt.Errorf("mdns advertisement for %q: %w", o.ServerID, err)
	}
	return inst, nil
}

// trustPolicy is the release trust configuration this gateway runs under.
func (o opts) trustPolicy() release.TrustPolicy {
	return release.TrustPolicy{AllowUnsignedDevBuilds: o.AllowUnsignedDevBuilds}
}

// listenPort extracts the port from a listen address such as ":8770".
func listenPort(listen string) (int, error) {
	_, portStr, found := strings.Cut(listen, ":")
	if !found {
		return 0, fmt.Errorf("listen address %q has no port", listen)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("listen address %q has an invalid port", listen)
	}
	return port, nil
}

// parseArgs reads the command line, applying a config file underneath it when
// --config is given.
func parseArgs(args []string) (opts, error) {
	probe, err := parseInto(args, nil)
	if err != nil {
		return opts{}, err
	}
	if probe.Config == "" {
		return probe, nil
	}

	if _, statErr := os.Stat(probe.Config); statErr != nil {
		return opts{}, fmt.Errorf("read config %q: %w", probe.Config, statErr)
	}
	return parseInto(args, &probe.Config)
}

func parseInto(args []string, iniFile *string) (opts, error) {
	var o opts
	p := flags.NewParser(&o, flags.Default)

	if iniFile != nil {
		if err := flags.NewIniParser(p).ParseFile(*iniFile); err != nil {
			return opts{}, fmt.Errorf("parse config %q: %w", *iniFile, err)
		}
	}
	if _, err := p.ParseArgs(args); err != nil {
		return opts{}, fmt.Errorf("parse arguments: %w", err)
	}
	return o, nil
}

// isHelpRequest reports whether the parse error is go-flags printing help.
func isHelpRequest(err error) bool {
	var flagsErr *flags.Error
	return errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp
}
