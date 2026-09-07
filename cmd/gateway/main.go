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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/discovery/mdns"
	gatewayconfig "github.com/MrZoidberg/echo-satellite/internal/gateway/config"
	"github.com/MrZoidberg/echo-satellite/internal/gateway/devices"
	"github.com/MrZoidberg/echo-satellite/internal/gateway/turns"
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

func loadToken(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: gateway operator explicitly supplies the credential path.
	if err != nil {
		return nil, fmt.Errorf("read device token file: %w", err)
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 || len(data) > 4096 {
		return nil, errors.New("read device token file: token must contain 1 through 4096 bytes")
	}
	return data, nil
}

func loadProfile(path string) (gatewayconfig.Snapshot, error) {
	file, err := os.Open(path) //nolint:gosec // G304: gateway operator explicitly supplies the profile path.
	if err != nil {
		return gatewayconfig.Snapshot{}, fmt.Errorf("open device configuration: %w", err)
	}
	defer func() { _ = file.Close() }()
	value, err := gatewayconfig.Load(file)
	if err != nil {
		return gatewayconfig.Snapshot{}, fmt.Errorf("load device configuration: %w", err)
	}
	return value, nil
}

func reloadProfile(store *gatewayconfig.Store, path string, deviceIDs []string) (map[string]protocol.DeviceConfig, error) {
	file, err := os.Open(path) //nolint:gosec // G304: gateway operator explicitly supplies the profile path.
	if err != nil {
		return nil, fmt.Errorf("open replacement device configuration: %w", err)
	}
	defer func() { _ = file.Close() }()
	configs, err := store.Reload(file, deviceIDs)
	if err != nil {
		return nil, fmt.Errorf("reload device configuration: %w", err)
	}
	return configs, nil
}

func setupLog(dbg bool) {
	level := slog.LevelInfo
	if dbg {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func run(o opts) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runtime, err := newGatewayRuntime(ctx, o)
	if err != nil {
		return err
	}
	return runtime.wait(ctx, o)
}

type gatewayRuntime struct {
	store      *gatewayconfig.Store
	devices    *devices.Server
	httpServer *http.Server
	errors     chan error
}

func newGatewayRuntime(ctx context.Context, o opts) (*gatewayRuntime, error) {
	if o.TLSCert == "" || o.TLSKey == "" || o.DeviceTokenFile == "" || o.DeviceConfig == "" {
		return nil, errors.New("--tls-cert, --tls-key, --device-token-file, and --device-config are required")
	}
	token, err := loadToken(o.DeviceTokenFile)
	if err != nil {
		return nil, err
	}
	profile, err := loadProfile(o.DeviceConfig)
	if err != nil {
		return nil, err
	}
	store, err := gatewayconfig.NewStore(profile)
	if err != nil {
		return nil, fmt.Errorf("create device configuration store: %w", err)
	}

	inst, err := o.advertisement()
	if err != nil {
		return nil, fmt.Errorf("build mdns advertisement: %w", err)
	}
	endpoint, err := inst.EndpointURL()
	if err != nil {
		return nil, fmt.Errorf("build device endpoint: %w", err)
	}
	slog.Info("gateway starting", "revision", revision, "listen", o.Listen, "protocol", protocol.ProtocolVersion, "endpoint", logValue(endpoint))

	// the escape hatch must be visible whenever it is on, not only at install time
	for _, note := range o.trustPolicy().StatusNotes() {
		slog.Warn("release trust", "note", logValue(note))
	}

	server, err := devices.New(devices.Options{Token: token, ServerID: o.ServerID, Config: func(deviceID string) protocol.DeviceConfig { return store.Snapshot().Effective(deviceID) }, Turns: turns.Receiver{Directory: o.DiagnosticWAV}})
	if err != nil {
		return nil, fmt.Errorf("create device session server: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle(o.Path, server)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", o.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen gateway endpoint: %w", err)
	}
	errCh := make(chan error, 2)
	go func() { errCh <- httpServer.ServeTLS(listener, o.TLSCert, o.TLSKey) }()
	if !o.NoMDNS {
		go func() { errCh <- mdns.New().Advertise(ctx, inst) }()
	}
	return &gatewayRuntime{store: store, devices: server, httpServer: httpServer, errors: errCh}, nil
}

func (r *gatewayRuntime) wait(ctx context.Context, o opts) error {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	for {
		select {
		case <-ctx.Done():
			r.devices.Close()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := r.httpServer.Shutdown(shutdownCtx)
			cancel()
			if err != nil {
				return fmt.Errorf("shutdown gateway endpoint: %w", err)
			}
			slog.Info("gateway stopped")
			return nil
		case <-hup:
			configs, err := reloadProfile(r.store, o.DeviceConfig, r.devices.DeviceIDs())
			if err != nil {
				slog.Error("retain active device configuration after reload failure", "error", err)
				continue
			}
			r.devices.PushConfig(configs)
			slog.Info("reloaded device configuration", "version", r.store.Snapshot().Version())
		case err := <-r.errors:
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("gateway service: %w", err)
			}
		}
	}
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
