// Command dotsim is a simulated Echo Dot. It speaks the same protocol as echod
// with a WAV fixture and local files in place of hardware.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/client"
	deviceconfig "github.com/MrZoidberg/echo-satellite/internal/device/config"
	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/discovery/mdns"
	"github.com/MrZoidberg/echo-satellite/internal/logging"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

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
	closeLog, err := logging.Configure(logging.Options{Format: o.LogFormat, File: o.LogFile, MaxBytes: o.LogMaxBytes, Debug: o.Dbg})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dotsim: %v\n", err)
		os.Exit(1)
	}
	runErr := run(o)
	closeErr := closeLog()
	if runErr != nil || closeErr != nil {
		slog.Error("dotsim failed", "error", errors.Join(runErr, closeErr))
		os.Exit(1)
	}
}

func run(o opts) error {
	if err := o.validate(); err != nil {
		return err
	}
	if o.Mic == "" {
		return errors.New("--mic is required when running dotsim")
	}
	if o.GatewayTokenFile == "" {
		return errors.New("--gateway-token-file is required when running dotsim")
	}
	if err := initSimDeploymentState(o.StateDir); err != nil {
		return err
	}
	bootstrap := deviceconfig.Bootstrap()
	state := newSimConfig(deviceconfig.Store{Path: filepath.Join(o.StateDir, "config.json")}, bootstrap)
	if err := state.load(); err != nil {
		slog.Warn("using local bootstrap configuration", "error", err)
	}
	turns, err := newWAVTurns(o, state)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controls := simUpdateControls{interruptDownload: o.UpdateFailure == "interrupted-download", invalidSignature: o.UpdateFailure == "invalid-signature", digestMismatch: o.UpdateFailure == "digest-mismatch", insufficientSpace: o.UpdateFailure == "insufficient-space", restartFailure: o.UpdateFailure == "restart-failure", reconnectFailure: o.UpdateFailure == "reconnect-failure"}
	if o.UpdatePublicKey != "" {
		controls.publicKey, err = release.ParsePublicKey(o.UpdatePublicKey)
		if err != nil {
			return fmt.Errorf("parse update public key: %w", err)
		}
	} else {
		controls.publicKey, _ = release.EmbeddedPublicKey()
	}
	slog.Info("dotsim configuration", "revision", revision, "device_id", o.DeviceID, "protocol", protocol.ProtocolVersion, "discovery", o.Discover, "gateway_endpoint", safeGatewayEndpoint(o.GatewayURL))
	for {
		sessionCtx, stopSession := context.WithCancel(ctx)
		restarted := make(chan struct{}, 1)
		manager := newSimDeploymentManager(o.StateDir, controls)
		manager.restart = func() {
			select {
			case restarted <- struct{}{}:
			default:
			}
			stopSession()
		}
		options := client.Options{Discovery: o.discoveryConfig(), HelloSource: func() protocol.Hello {
			metadata := manager.pendingMetadata()
			return protocol.Hello{DeviceID: o.DeviceID, AgentVersion: revision, Protocol: protocol.ProtocolVersion,
				Capabilities: protocol.NewCapabilities(protocol.CapWakeLocal, protocol.CapAudioCapture, protocol.CapCommandEndpointingLocal, protocol.CapUpdateSingle),
				WakeConfig:   wakeSummary(state.current()), UpdateState: protocol.PhaseIdle, InstalledVersion: metadata.Version,
				InstalledBuildID: metadata.BuildID, PendingDeploymentID: metadata.PendingDeploymentID, ConfigVersion: state.current().Version}
		}, Dialer: client.WSSDialer{}, Resolver: timedResolver{resolver: discovery.NewResolver(mdns.New(), protocol.ProtocolVersion), timeout: time.Duration(o.DiscoveryTimeout) * time.Millisecond}, Pairings: discovery.PairingStore{Path: filepath.Join(o.StateDir, "paired-gateway.json")}, Config: state, TurnSource: turns, TokenPath: o.GatewayTokenFile, SkipTLSVerify: o.TLSSkipVerify, Logger: slog.Default(), Update: manager}
		if o.Once {
			options.TurnSent = cancel
		}
		session, sessionErr := client.New(options)
		if sessionErr != nil {
			return fmt.Errorf("create gateway client: %w", sessionErr)
		}
		sessionErr = session.Run(sessionCtx)
		stopSession()
		select {
		case <-restarted:
			if controls.reconnectFailure {
				return nil
			}
			continue
		default:
		}
		if o.Once && errors.Is(sessionErr, context.Canceled) {
			return nil
		}
		return fmt.Errorf("run gateway client: %w", sessionErr)
	}
}

func safeGatewayEndpoint(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "configured"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func wakeSummary(settings deviceconfig.Settings) protocol.WakeConfig {
	return protocol.WakeConfig{Engine: settings.Wake.Engine, Models: []string{settings.Wake.Model}, WakeThreshold: settings.Wake.Threshold, VADThreshold: settings.Wake.VAD.Threshold, PreRollMS: settings.Wake.PreRollMS}
}
