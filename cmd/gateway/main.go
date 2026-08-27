// Command gateway is the central orchestration service satellites connect to.
//
// Milestone 0 delivers the entrypoint and the shared contracts only: this
// binary parses its configuration, reports the mDNS record it would advertise
// and the release trust policy it runs under, and waits for a signal. The
// device WebSocket endpoint, mDNS advertisement and Update Manager land in
// Milestones 2 and 4.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

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
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
		os.Exit(1)
	}

	if o.Version {
		fmt.Printf("version: %s\n", revision)
		return
	}

	setupLog(o.Dbg)
	if err := run(o); err != nil {
		slog.Error("gateway failed", "error", err)
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

	slog.Info("gateway starting", "revision", revision, "listen", o.Listen, "protocol", protocol.ProtocolVersion)

	inst, err := o.advertisement()
	if err != nil {
		return fmt.Errorf("build mdns advertisement: %w", err)
	}
	endpoint, err := inst.EndpointURL()
	if err != nil {
		return fmt.Errorf("build device endpoint: %w", err)
	}
	slog.Info("mdns advertisement", "instance", logValue(inst.ServerID), "endpoint", logValue(endpoint), "txt", logValues(inst.TXT.Encode()))

	// the escape hatch must be visible whenever it is on, not only at install time
	for _, note := range o.trustPolicy().StatusNotes() {
		slog.Warn("release trust", "note", logValue(note))
	}

	slog.Warn("no gateway subsystem is implemented yet",
		"detail", "device endpoint, mdns advertisement and update manager land in milestones 2 and 4")

	<-ctx.Done()
	slog.Info("gateway stopped")
	return nil
}

// logValue prevents values received from configuration or discovery from
// injecting a separate record into text logs.
func logValue(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func logValues(values []string) []string {
	sanitized := make([]string, len(values))
	for i, value := range values {
		sanitized[i] = logValue(value)
	}
	return sanitized
}
