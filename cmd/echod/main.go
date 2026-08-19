// Command echod is the device agent that runs on a rooted Echo Dot.
//
// Milestone 0 delivers the entrypoint and the shared contracts only: this
// binary parses its configuration, reports what it would announce, and waits
// for a signal. Microphone capture, the local wake stack and the gateway
// connection land in Milestones 1 and 2.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
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
		fmt.Fprintf(os.Stderr, "echod: %v\n", err)
		os.Exit(1)
	}

	if o.Version {
		fmt.Printf("version: %s\n", revision)
		return
	}

	setupLog(o.Dbg)
	if err := run(o); err != nil {
		slog.Error("echod failed", "error", err)
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("echod starting", "revision", revision, "device_id", o.DeviceID, "protocol", protocol.ProtocolVersion)
	slog.Info("announced capabilities", "capabilities", announcedCapabilities())
	logGatewayTarget(ctx, o)

	slog.Warn("no device subsystem is implemented yet",
		"detail", "microphone, local wake stack and gateway transport land in milestones 1 and 2")

	<-ctx.Done()
	slog.Info("echod stopped")
	return nil
}

// announcedCapabilities is what this build would send in hello. Wake detection
// is always local, so every build announces it.
func announcedCapabilities() protocol.Capabilities {
	return protocol.NewCapabilities(
		protocol.CapWakeLocal,
		protocol.CapAudioCapture,
		protocol.CapAudioPlayback,
		protocol.CapButton,
		protocol.CapLED,
		protocol.CapMute,
		protocol.CapUpdateAB,
	)
}

// logGatewayTarget reports which gateway this configuration resolves to. No
// browser is wired yet, so only an explicit url or a paired gateway resolves;
// mDNS browsing lands in Milestone 2.
func logGatewayTarget(ctx context.Context, o opts) {
	cfg := o.discoveryConfig()
	slog.Info("gateway configuration",
		"discovery", cfg.Discovery, "url", cfg.URL, "preferred_server_id", cfg.PreferredServerID)

	endpoint, err := discovery.NewResolver(nil, protocol.ProtocolVersion).Resolve(ctx, cfg, nil)
	switch {
	case errors.Is(err, discovery.ErrNoGateway):
		slog.Info("no gateway endpoint resolved", "detail", "mdns browsing lands in milestone 2")
	case err != nil:
		slog.Error("gateway configuration is unusable", "error", err)
	default:
		slog.Info("gateway endpoint resolved", "endpoint", endpoint)
	}
}
