package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

// opts is the dotsim command line. It mirrors the simulated device described in
// docs/DESIGN.md 21: the same protocol as echod, with files and flags where the
// hardware would be.
type opts struct {
	Config            string  `long:"config" env:"DOTSIM_CONFIG" description:"path to the dotsim ini config file" no-ini:"true"`
	DeviceID          string  `long:"device-id" env:"DOTSIM_DEVICE_ID" default:"dotsim-1" description:"simulated device identity"`
	Discover          string  `long:"discover" env:"DOTSIM_DISCOVER" default:"mdns" choice:"mdns" choice:"disabled" description:"gateway discovery mode"`
	GatewayURL        string  `long:"gateway-url" env:"DOTSIM_GATEWAY_URL" description:"explicit gateway url; overrides discovery"`
	Trigger           string  `long:"trigger" env:"DOTSIM_TRIGGER" default:"wake" choice:"wake" choice:"button" description:"what starts the simulated turn"`
	WakeModel         string  `long:"wake-model" env:"DOTSIM_WAKE_MODEL" default:"okay_nabu" description:"wake model reported in turn.start"`
	WakeScore         float64 `long:"wake-score" env:"DOTSIM_WAKE_SCORE" default:"0.87" description:"wake score reported in turn.start"`
	VADScore          float64 `long:"vad-score" env:"DOTSIM_VAD_SCORE" default:"0.93" description:"local wake vad score reported in turn.start"`
	Mic               string  `long:"mic" env:"DOTSIM_MIC" description:"wav file streamed as command audio"`
	SpeakerOut        string  `long:"speaker-out" env:"DOTSIM_SPEAKER_OUT" description:"wav file playback audio is written to"`
	GatewayTokenFile  string  `long:"gateway-token-file" env:"DOTSIM_GATEWAY_TOKEN_FILE" description:"file containing the gateway bearer token"`
	TLSSkipVerify     bool    `long:"tls-skip-verify" env:"DOTSIM_TLS_SKIP_VERIFY" description:"disable TLS certificate verification; development only"`
	PreferredServerID string  `long:"preferred-server-id" env:"DOTSIM_PREFERRED_SERVER_ID" description:"gateway server ID to prefer during discovery"`
	StateDir          string  `long:"state-dir" env:"DOTSIM_STATE_DIR" default:".dotsim" description:"directory for persisted gateway and config state"`
	DiscoveryTimeout  int     `long:"discovery-timeout-ms" env:"DOTSIM_DISCOVERY_TIMEOUT_MS" default:"5000" description:"maximum mDNS resolution time in milliseconds"`
	Once              bool    `long:"once" env:"DOTSIM_ONCE" description:"exit after the first successfully transmitted fixture turn"`

	Dbg     bool `long:"dbg" env:"DEBUG" description:"debug logging"`
	Version bool `long:"version" short:"V" description:"show version and exit"`
}

// turnStart is the turn this configuration would open. Building it here keeps
// the simulator honest: a wake score it could not put on the wire is rejected
// at startup rather than silently clamped later.
func (o opts) turnStart() (protocol.TurnStart, error) {
	trigger := protocol.TurnTrigger(o.Trigger)
	if !trigger.Valid() {
		return protocol.TurnStart{}, fmt.Errorf("unknown trigger %q", o.Trigger)
	}

	turn := protocol.TurnStart{Trigger: trigger}
	if trigger == protocol.TriggerWake {
		if o.WakeModel == "" {
			return protocol.TurnStart{}, errors.New("a wake-triggered turn needs --wake-model")
		}
		if err := checkScore("wake-score", o.WakeScore); err != nil {
			return protocol.TurnStart{}, err
		}
		if err := checkScore("vad-score", o.VADScore); err != nil {
			return protocol.TurnStart{}, err
		}
		turn.Model, turn.WakeScore, turn.VADScore = o.WakeModel, o.WakeScore, o.VADScore
	}
	return turn, nil
}

// discoveryConfig converts the options into the gateway resolution settings.
func (o opts) discoveryConfig() discovery.Config {
	return discovery.Config{Discovery: o.Discover, URL: o.GatewayURL, PreferredServerID: o.PreferredServerID}
}

// validate checks the whole configuration, including files that must exist.
func (o opts) validate() error {
	if _, err := o.turnStart(); err != nil {
		return err
	}
	if o.Mic != "" {
		if _, err := os.Stat(o.Mic); err != nil {
			return fmt.Errorf("mic file: %w", err)
		}
	}
	if o.DiscoveryTimeout <= 0 {
		return errors.New("--discovery-timeout-ms must be positive")
	}
	return nil
}

func checkScore(name string, score float64) error {
	if score < 0 || score > 1 {
		return fmt.Errorf("--%s must be between 0 and 1, got %v", name, score)
	}
	return nil
}

func parseArgs(args []string) (opts, error) {
	probe, err := parseInto(args, nil)
	if err != nil {
		return opts{}, err
	}
	if probe.Config == "" {
		return probe, nil
	}
	if _, err := os.Stat(probe.Config); err != nil {
		return opts{}, fmt.Errorf("read config %q: %w", probe.Config, err)
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
		return opts{}, err //nolint:wrapcheck // callers inspect the go-flags error type directly
	}
	return o, nil
}

// isHelpRequest reports whether the parse error is go-flags printing help.
func isHelpRequest(err error) bool {
	var flagsErr *flags.Error
	return errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp
}
