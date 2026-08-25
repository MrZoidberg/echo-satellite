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
	Buttons buttonsCommand `command:"buttons" description:"diagnose device buttons"`
	Wake    wakeCommand    `command:"wake" description:"manage local wake models"`
	Bench   benchCommand   `command:"bench" description:"benchmark on-device mel, embedding, classifier and VAD inference"`
	Status  statusCommand  `command:"status" description:"report device health and wake diagnostics"`
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
	Current   uint8   `long:"current" default:"0" description:"LED current attenuation index from 0 (full) to 3 (quarter)"`
	Clear     bool    `long:"clear" description:"clear the ring after the test"`
}

type buttonsCommand struct {
	Test buttonsTestCommand `command:"test" description:"print recognized button presses"`
}

type buttonsTestCommand struct {
	InputDir    string  `long:"input-dir" default:"/dev/input" description:"input event device directory"`
	SysClassDir string  `long:"sys-class-dir" default:"/sys/class/input" description:"input sysfs class directory"`
	FromFile    string  `long:"from-file" description:"replay recorded input events instead of live devices"`
	Seconds     float64 `long:"seconds" default:"30" description:"seconds to watch for events"`
}

type wakeCommand struct {
	List    wakeListCommand    `command:"list" description:"list installed wake models"`
	Install wakeInstallCommand `command:"install" description:"verify and install a local wake model"`
	Test    wakeTestCommand    `command:"test" description:"run wake detection against a WAV, raw PCM, or live microphone"`
	VADTest wakeVADTestCommand `command:"vad-test" description:"run wake VAD against a WAV, raw PCM, or live microphone"`
}

type wakeInputOptions struct {
	FromFile   string  `long:"from-file" description:"replay a WAV or raw device PCM instead of live ALSA capture"`
	Threshold  float64 `long:"threshold" default:"0.5" description:"score threshold from 0 to 1"`
	PrintSteps bool    `long:"print-steps" description:"print every 80ms score"`
	Seconds    float64 `long:"seconds" default:"5" description:"maximum live capture duration; zero reads a file to EOF"`
}

type wakeTestCommand struct {
	wakeInputOptions
	Model        string  `long:"model" default:"okay_nabu" description:"installed wake model ID"`
	ModelDir     string  `long:"model-dir" default:"/data/local/etc/echo-satellite/wake-models" description:"wake model store directory"`
	VADThreshold float64 `long:"vad-threshold" default:"0.5" description:"wake VAD threshold from 0 to 1"`
	NoVAD        bool    `long:"no-vad" description:"disable the local wake VAD gate"`
	PreRollMS    int     `long:"preroll-ms" default:"250" description:"accepted-event pre-roll duration in milliseconds"`
	SavePreRoll  string  `long:"save-preroll" description:"opt in to storing accepted raw pre-roll WAV files in this directory"`
}

type wakeVADTestCommand struct{ wakeInputOptions }

type statusCommand struct {
	JSON     bool   `long:"json" description:"print machine-readable JSON"`
	ModelDir string `long:"model-dir" default:"/data/local/etc/echo-satellite/wake-models" description:"wake model store directory"`
	Model    string `long:"model" default:"okay_nabu" description:"configured active wake model ID"`
	Root     string `long:"root" default:"/" description:"filesystem root used for hardware and identity probes"`
}

type wakeListCommand struct {
	ModelDir string `long:"model-dir" default:"/data/local/etc/echo-satellite/wake-models" description:"wake model store directory"`
}

type wakeInstallCommand struct {
	Args struct {
		ID string `positional-arg-name:"id" required:"true"`
	} `positional-args:"true"`
	From      string `long:"from" required:"true" description:"local TFLite classifier path"`
	Metadata  string `long:"metadata" description:"sidecar JSON path; defaults beside --from"`
	SHA256    string `long:"sha256" description:"expected SHA-256; defaults to the sidecar digest"`
	ModelDir  string `long:"model-dir" default:"/data/local/etc/echo-satellite/wake-models" description:"wake model store directory"`
	Overwrite bool   `long:"overwrite" description:"replace an installed model with the same id"`
}

// benchCommand runs the mel, embedding and classifier models plus the VAD over N synthetic or
// fixture-replayed 80 ms steps and reports per-stage timing, CPU and RSS. This is Task 17's
// on-device inference budget measurement, done ahead of building the wake pipeline on it.
type benchCommand struct {
	Model    string `long:"model" required:"true" description:"wake classifier model name (without .tflite) under --model-dir"`
	ModelDir string `long:"model-dir" default:".assets/wake-models" description:"directory containing melspectrogram.tflite, embedding_model.tflite and the classifier model"`
	Steps    int    `long:"steps" default:"500" description:"number of 80ms wake steps to benchmark"`
	FromFile string `long:"from-file" description:"16 kHz mono s16 PCM (raw or WAV) fixture to replay instead of a synthetic waveform"`
	JSON     bool   `long:"json" description:"print a machine-readable JSON report instead of text"`
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
