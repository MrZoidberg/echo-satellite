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
}

type versionCommand struct{}

type releaseCommand struct {
	Verify verifyCommand `command:"verify" description:"verify a release bundle: manifest, artifact digest and signature"`
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
