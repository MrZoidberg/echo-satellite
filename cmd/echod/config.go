package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
)

// opts is the echod command line. Precedence is CLI flag, then environment
// variable, then config file, then the built-in default.
type opts struct {
	Config            string `long:"config" env:"ECHOD_CONFIG" description:"path to the echod ini config file" no-ini:"true"`
	DeviceID          string `long:"device-id" env:"ECHOD_DEVICE_ID" description:"device identity announced in hello"`
	Discovery         string `long:"discovery" env:"ECHOD_DISCOVERY" default:"mdns" choice:"mdns" choice:"disabled" description:"gateway discovery mode"`
	GatewayURL        string `long:"gateway-url" env:"ECHOD_GATEWAY_URL" description:"explicit gateway url; overrides discovery"`
	PreferredServerID string `long:"preferred-server-id" env:"ECHOD_PREFERRED_SERVER_ID" description:"gateway server_id to prefer"`
	Dbg               bool   `long:"dbg" env:"DEBUG" description:"debug logging" no-ini:"true"`
	Version           bool   `long:"version" short:"V" description:"show version and exit" no-ini:"true"`
}

// discoveryConfig converts the options into the gateway resolution settings.
func (o opts) discoveryConfig() discovery.Config {
	return discovery.Config{
		Discovery:         o.Discovery,
		URL:               o.GatewayURL,
		PreferredServerID: o.PreferredServerID,
	}
}

// parseArgs reads the command line. When --config names a file, the file is
// loaded first and the command line is then re-applied on top of it, so an
// explicit flag always wins over a config value.
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
