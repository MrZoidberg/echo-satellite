// Command dotsim is a simulated Echo Dot. It speaks the same protocol as echod
// with files and flags in place of hardware, so gateway and fleet logic can be
// exercised without a real device.
//
// Milestone 0 delivers the entrypoint and configuration validation only: this
// binary reports the turn it would open and exits. Connecting, streaming audio
// and simulating update states land in Milestone 2.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

// revision is set at link time by the build.
var revision = "unknown"

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		if isHelpRequest(err) {
			return
		}
		fmt.Fprintf(os.Stderr, "dotsim: %v\n", err)
		os.Exit(1)
	}

	if o.Version {
		fmt.Printf("version: %s\n", revision)
		return
	}

	setupLog(o.Dbg)
	if err := run(o); err != nil {
		slog.Error("dotsim failed", "error", err)
		os.Exit(1)
	}
}

func setupLog(dbg bool) {
	level := slog.LevelInfo
	if dbg {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func run(o opts) error {
	if err := o.validate(); err != nil {
		return err
	}

	turn, err := o.turnStart()
	if err != nil {
		return err
	}
	cfg := o.discoveryConfig()

	slog.Info("dotsim configuration",
		"revision", revision, "device_id", o.DeviceID, "protocol", protocol.ProtocolVersion,
		"discovery", cfg.Discovery, "gateway_url", cfg.URL)
	slog.Info("simulated turn",
		"trigger", turn.Trigger, "model", turn.Model, "wake_score", turn.WakeScore, "vad_score", turn.VADScore,
		"mic", o.Mic, "speaker_out", o.SpeakerOut)

	slog.Warn("no simulator behavior is implemented yet",
		"detail", "connecting, audio streaming and update simulation land in milestone 2")
	return nil
}
