package main

import (
	"errors"

	"github.com/jessevdk/go-flags"
)

// opts is the echoctl command tree. Command groups for microphone, wake and
// device diagnostics arrive with their milestones; only the commands whose
// subsystem exists are exposed.
type opts struct {
	Version versionCommand `command:"version" description:"show version"`
	Release releaseCommand `command:"release" description:"inspect and verify release artifacts"`
	Mic     micCommand     `command:"mic" description:"diagnose microphone capture"`
	Speaker speakerCommand `command:"speaker" description:"diagnose speaker playback"`
	LED     ledCommand     `command:"led" description:"diagnose the LED ring"`
}

type versionCommand struct{}

type releaseCommand struct {
	Verify verifyCommand `command:"verify" description:"verify a release bundle: manifest, artifact digest and signature"`
}

type micCommand struct {
	Record micRecordCommand `command:"record" description:"record microphone PCM to WAV"`
}

type micRecordCommand struct {
	Seconds     float64 `long:"seconds" default:"5" description:"recording duration in seconds"`
	Out         string  `long:"out" required:"true" description:"output WAV path"`
	Channels    string  `long:"channels" default:"mic0" description:"mic0..mic6, all, or a comma-separated list"`
	FromFile    string  `long:"from-file" description:"replay raw device PCM instead of ALSA"`
	Card        int     `long:"card" default:"0" description:"ALSA card number"`
	Device      int     `long:"device" default:"24" description:"ALSA capture device number"`
	PrintLevels bool    `long:"print-levels" description:"print peak and RMS dBFS per selected channel"`
}

type speakerCommand struct {
	Test speakerTestCommand `command:"test" description:"play a WAV or generated test tone"`
}

type speakerTestCommand struct {
	In        string  `long:"in" description:"input WAV; defaults to a generated 1 kHz tone"`
	Seconds   float64 `long:"seconds" default:"1" description:"generated tone or playback duration in seconds"`
	ToFile    string  `long:"to-file" description:"write playback PCM to a WAV instead of ALSA"`
	Resampler string  `long:"resampler" choice:"sinc" choice:"linear" choice:"hold" default:"sinc"`
	Volume    float64 `long:"volume" default:"1" description:"linear volume gain from 0 to 1"`
	NoAmp     bool    `long:"no-amp" description:"do not enable the speaker amplifier"`
	Card      int     `long:"card" default:"0" description:"ALSA card number"`
	Device    int     `long:"device" default:"23" description:"ALSA playback device number"`
}

type ledCommand struct {
	Test ledTestCommand `command:"test" description:"render semantic LED states"`
}

type ledTestCommand struct {
	Root      string  `long:"root" default:"/sys/bus/i2c/devices/0-003f" description:"LED controller sysfs root"`
	State     string  `long:"state" default:"idle" description:"semantic device state"`
	AllStates bool    `long:"all-states" description:"render every semantic device state in order"`
	Seconds   float64 `long:"seconds" default:"1" description:"seconds to render each state"`
	Current   uint8   `long:"current" default:"255" description:"global LED current from 0 to 255"`
	Clear     bool    `long:"clear" description:"clear the ring after the test"`
}

// verifyCommand checks a release bundle exactly the way a gateway or a device
// checks one before staging it.
type verifyCommand struct {
	Manifest      string `long:"manifest" required:"true" description:"path to manifest.json"`
	Artifact      string `long:"artifact" description:"path to the release artifact; when set, size and sha256 are checked"`
	Sig           string `long:"sig" description:"path to manifest.sig"`
	PubKey        string `long:"pubkey" description:"path to the base64 release public key; defaults to the key built into this binary"`
	AllowUnsigned bool   `long:"allow-unsigned-dev-builds" description:"accept a bundle with no signature; development only"`
}

func newParser(o *opts) *flags.Parser { return flags.NewParser(o, flags.Default) }

// parseArgs parses the command line without executing anything. It exists so
// the flag wiring is testable on its own.
func parseArgs(args []string) (opts, string, error) {
	var o opts
	p := newParser(&o)
	if _, err := p.ParseArgs(args); err != nil {
		return opts{}, "", err //nolint:wrapcheck // callers inspect the go-flags error type directly
	}
	if p.Active == nil {
		return o, "", nil
	}
	name := p.Active.Name
	if p.Active.Active != nil {
		name += " " + p.Active.Active.Name
	}
	return o, name, nil
}

// isHelpRequest reports whether the parse error is go-flags printing help.
func isHelpRequest(err error) bool {
	var flagsErr *flags.Error
	return errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp
}
