package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/discovery"
)

const (
	defaultStatsInterval = 30 * time.Second
	defaultLogMaxBytes   = 10 * 1024 * 1024
)

type configurableBool string

func (b *configurableBool) UnmarshalFlag(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("parse boolean %q: %w", value, err)
	}
	*b = configurableBool(strconv.FormatBool(parsed))
	return nil
}

func (b configurableBool) Bool() bool { return b == "true" }

// opts is the echod command line. Precedence is CLI flag, then environment
// variable, then config file, then the built-in default.
type opts struct {
	Config            string           `long:"config" env:"ECHOD_CONFIG" description:"path to the echod ini config file" no-ini:"true"`
	DeviceID          string           `long:"device-id" env:"ECHOD_DEVICE_ID" description:"device identity announced in hello"`
	Discovery         string           `long:"discovery" env:"ECHOD_DISCOVERY" default:"mdns" choice:"mdns" choice:"disabled" description:"gateway discovery mode"`
	GatewayURL        string           `long:"gateway-url" env:"ECHOD_GATEWAY_URL" description:"explicit gateway url; overrides discovery"`
	PreferredServerID string           `long:"preferred-server-id" env:"ECHOD_PREFERRED_SERVER_ID" description:"gateway server_id to prefer"`
	WakeOnly          bool             `long:"wake-only" env:"ECHOD_WAKE_ONLY" description:"run the device-local wake pipeline without gateway traffic"`
	TestStartAudio    string           `long:"test-start-audio" env:"ECHOD_TEST_START_AUDIO" default:"/data/local/etc/echo-satellite/starting_test.wav" description:"16 kHz mono WAV played before live wake-only diagnostics"`
	WakeModel         string           `long:"wake-model" env:"ECHOD_WAKE_MODEL" default:"okay_nabu" description:"installed wake model id"`
	WakeModelDir      string           `long:"wake-model-dir" env:"ECHOD_WAKE_MODEL_DIR" default:"/data/local/etc/echo-satellite/wake-models" description:"installed wake model directory"`
	WakeThreshold     float64          `long:"wake-threshold" env:"ECHOD_WAKE_THRESHOLD" default:"0.50" description:"wake acceptance threshold from 0 to 1"`
	VADThreshold      float64          `long:"vad-threshold" env:"ECHOD_VAD_THRESHOLD" default:"0.50" description:"wake VAD threshold from 0 to 1"`
	VADEnabled        configurableBool `long:"vad-enabled" env:"ECHOD_VAD_ENABLED" default:"true" optional:"true" optional-value:"true" description:"require local VAD evidence for wake acceptance"`
	VADLookbackMS     int              `long:"vad-lookback-ms" env:"ECHOD_VAD_LOOKBACK_MS" default:"1200" description:"bounded recent VAD evidence window in milliseconds"`
	PreRollMS         int              `long:"preroll-ms" env:"ECHOD_PREROLL_MS" default:"600" description:"pre-trigger audio retained in milliseconds"`
	MinWakeIntervalMS int              `long:"min-wake-interval-ms" env:"ECHOD_MIN_WAKE_INTERVAL_MS" default:"2000" description:"minimum interval between accepted wakes in milliseconds"`
	MicChannels       string           `long:"mic-channels" env:"ECHOD_MIC_CHANNELS" default:"0" description:"comma-separated physical microphone channel indices"`
	MicFromFile       string           `long:"mic-from-file" env:"ECHOD_MIC_FROM_FILE" description:"replay paced WAV or raw Dot microphone PCM instead of ALSA"`
	LEDRoot           string           `long:"led-root" env:"ECHOD_LED_ROOT" default:"/sys/bus/i2c/devices/0-003f" description:"LED controller sysfs root"`
	StatsInterval     time.Duration    `long:"stats-interval" env:"ECHOD_STATS_INTERVAL" default:"30s" description:"periodic wake statistics interval"`
	AlwaysScoreWake   configurableBool `long:"always-score-wake" env:"ECHOD_ALWAYS_SCORE_WAKE" default:"true" optional:"true" optional-value:"true" description:"score wake on every step instead of pre-gating on instantaneous VAD"`
	LogFile           string           `long:"log-file" env:"ECHOD_LOG_FILE" description:"bounded rotating structured log file"`
	LogMaxBytes       int64            `long:"log-max-bytes" env:"ECHOD_LOG_MAX_BYTES" default:"10485760" description:"total byte cap across rotating logs"`
	Dbg               bool             `long:"dbg" env:"DEBUG" description:"debug logging" no-ini:"true"`
	Version           bool             `long:"version" short:"V" description:"show version and exit" no-ini:"true"`
}

func (o opts) wakeConfig() wake.Config {
	cfg := wake.Defaults()
	cfg.Model = o.WakeModel
	cfg.Threshold = o.WakeThreshold
	cfg.VAD.Enabled = o.VADEnabled.Bool()
	cfg.VAD.Threshold = o.VADThreshold
	cfg.VAD.LookbackMS = o.VADLookbackMS
	cfg.PreRollMS = o.PreRollMS
	cfg.MinIntervalMS = o.MinWakeIntervalMS
	cfg.AlwaysScoreWake = o.AlwaysScoreWake.Bool()
	return cfg
}

func (o opts) micChannelList() ([]int, error) {
	parts := strings.Split(o.MicChannels, ",")
	channels := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		channel, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || channel < 0 || channel >= 7 {
			return nil, fmt.Errorf("mic channel %q must be a physical channel from 0 to 6", part)
		}
		if _, duplicate := seen[channel]; duplicate {
			return nil, fmt.Errorf("mic channel %d is repeated", channel)
		}
		seen[channel] = struct{}{}
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return nil, errors.New("at least one microphone channel is required")
	}
	return channels, nil
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
		return validateOpts(probe)
	}

	if _, statErr := os.Stat(probe.Config); statErr != nil {
		return opts{}, fmt.Errorf("read config %q: %w", probe.Config, statErr)
	}
	o, err := parseInto(args, &probe.Config)
	if err != nil {
		return opts{}, err
	}
	return validateOpts(o)
}

func validateOpts(o opts) (opts, error) {
	if err := o.wakeConfig().Validate(); err != nil {
		return opts{}, fmt.Errorf("validate wake configuration: %w", err)
	}
	if _, err := o.micChannelList(); err != nil {
		return opts{}, err
	}
	if o.StatsInterval <= 0 {
		return opts{}, errors.New("stats interval must be positive")
	}
	if o.LogMaxBytes <= 0 {
		return opts{}, errors.New("log max bytes must be positive")
	}
	return o, nil
}

func parseInto(args []string, iniFile *string) (opts, error) {
	var o opts
	p := flags.NewParser(&o, flags.Default)

	if iniFile != nil {
		if err := flags.NewIniParser(p).ParseFile(*iniFile); err != nil {
			return opts{}, fmt.Errorf("parse config %q: %w", *iniFile, err)
		}
		if err := applyEnvironment(&o); err != nil {
			return opts{}, err
		}
	}
	if _, err := p.ParseArgs(args); err != nil {
		return opts{}, fmt.Errorf("parse arguments: %w", err)
	}
	return o, nil
}

// applyEnvironment restores the documented environment-over-ini precedence.
// go-flags treats ini values as already supplied and otherwise will not replace
// them from env tags when ParseArgs runs.
func applyEnvironment(o *opts) error {
	value := reflect.ValueOf(o).Elem()
	typeOfOpts := value.Type()
	for index := range value.NumField() {
		name := typeOfOpts.Field(index).Tag.Get("env")
		raw, found := os.LookupEnv(name)
		if name == "" || !found {
			continue
		}
		field := value.Field(index)
		var err error
		if field.CanAddr() && field.Addr().Type() == reflect.TypeFor[*configurableBool]() {
			err = field.Addr().Interface().(*configurableBool).UnmarshalFlag(raw)
			if err != nil {
				return fmt.Errorf("parse environment %s: %w", name, err)
			}
			continue
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString(raw)
		case reflect.Bool:
			var parsed bool
			parsed, err = strconv.ParseBool(raw)
			field.SetBool(parsed)
		case reflect.Int, reflect.Int64:
			var parsed int64
			if field.Type() == reflect.TypeFor[time.Duration]() {
				var duration time.Duration
				duration, err = time.ParseDuration(raw)
				parsed = int64(duration)
			} else {
				parsed, err = strconv.ParseInt(raw, 10, field.Type().Bits())
			}
			field.SetInt(parsed)
		case reflect.Float64:
			var parsed float64
			parsed, err = strconv.ParseFloat(raw, 64)
			field.SetFloat(parsed)
		default:
			err = fmt.Errorf("unsupported option type %s", field.Type())
		}
		if err != nil {
			return fmt.Errorf("parse environment %s: %w", name, err)
		}
	}
	return nil
}

// isHelpRequest reports whether the parse error is go-flags printing help.
func isHelpRequest(err error) bool {
	var flagsErr *flags.Error
	return errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp
}
