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
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/audio/alsa"
	"github.com/MrZoidberg/echo-satellite/internal/device/buttons"
	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/device/system"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/oww"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/vadlevel"
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

	if o.WakeOnly {
		return runWakeOnly(ctx, o)
	}

	slog.Info("echod starting", "mode", "milestone-0", "revision", revision, "device_id", o.DeviceID, "protocol", protocol.ProtocolVersion)
	slog.Info("announced capabilities", "capabilities", announcedCapabilities())
	logGatewayTarget(ctx, o)

	slog.Warn("no device subsystem is implemented yet",
		"detail", "microphone, local wake stack and gateway transport land in milestones 1 and 2")

	<-ctx.Done()
	slog.Info("echod stopped")
	return nil
}

type subscriptionFrames struct{ subscription *audio.Subscription }

func (s subscriptionFrames) Frames() <-chan audio.Frame { return s.subscription.Frames }

type wakeWorker func(context.Context) error

type closeOncePCMSource struct {
	audio.PCMSource
	once sync.Once
	err  error
}

func (s *closeOncePCMSource) Close() error {
	s.once.Do(func() { s.err = s.PCMSource.Close() })
	return s.err
}

func runWakeOnly(parent context.Context, o opts) (returnErr error) {
	closeLog, err := configureWakeLog(o)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeLog()) }()

	identity, err := system.Resolve(system.SerialReader{}, o.DeviceID, system.DeviceIDFile)
	if err != nil {
		return fmt.Errorf("resolve identity: %w", err)
	}
	cfg := o.wakeConfig()
	slog.Info("echod starting", "mode", "wake-only", "revision", revision, "device_id", identity.DeviceID, "protocol", protocol.ProtocolVersion)
	slog.Info("wake configuration", "device_id", identity.DeviceID, "model_id", cfg.Model,
		"wake_threshold", cfg.Threshold, "vad_threshold", cfg.VAD.Threshold,
		"vad_enabled", cfg.VAD.Enabled, "vad_lookback_ms", cfg.VAD.LookbackMS,
		"preroll_ms", cfg.PreRollMS, "min_wake_interval_ms", cfg.MinIntervalMS,
		"always_score_wake", cfg.AlwaysScoreWake)

	store := wake.Store{Root: o.WakeModelDir}
	model, err := store.Get(cfg.Model)
	if err != nil {
		return fmt.Errorf("load wake model: %w", err)
	}
	shared, err := loadWakeSharedModels(store)
	if err != nil {
		return err
	}
	engine, err := oww.New(shared, model)
	if err != nil {
		return fmt.Errorf("prepare wake engine: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, engine.Close()) }()

	rawSource, err := openWakeOnlySource(o)
	if err != nil {
		return err
	}
	source := &closeOncePCMSource{PCMSource: rawSource}
	sourceOwned := true
	defer func() {
		if sourceOwned {
			returnErr = errors.Join(returnErr, source.Close())
		}
	}()
	channels, err := o.micChannelList()
	if err != nil {
		return err
	}
	capturer, err := audio.NewCapturer(source, audio.CaptureConfig{
		Device: source.Format(), Channels: channels, Preprocessor: audio.Bypass{}, StepSamples: wake.StepSamples,
	}, slog.Default())
	if err != nil {
		return fmt.Errorf("create wake capturer: %w", err)
	}
	fanout := audio.NewFanout(capturer)
	subscription, err := fanout.Subscribe("wake", 8)
	if err != nil {
		return fmt.Errorf("subscribe wake pipeline: %w", err)
	}
	ring, err := audio.NewRing(audio.Format{SampleRate: wake.SampleRate, Channels: 1, Layout: audio.LayoutS16LE}, time.Duration(cfg.PreRollMS)*time.Millisecond)
	if err != nil {
		return fmt.Errorf("create pre-roll ring: %w", err)
	}
	stats := wake.NewStats(wake.StatsConfig{
		ActiveModelID: model.ID, ModelKind: model.Kind, Languages: model.Languages,
		Thresholds: wake.Thresholds{Wake: cfg.Threshold, VAD: cfg.VAD.Threshold},
		VADEnabled: cfg.VAD.Enabled, VADLookbackMS: cfg.VAD.LookbackMS,
	})
	vad := vadlevel.NewScorer()
	defer func() { returnErr = errors.Join(returnErr, vad.Close()) }()
	pipeline := wake.Pipeline{
		Engines: []wake.Engine{engine}, VAD: vad,
		Gate: wake.Gate{Thresholds: wake.Thresholds{Wake: cfg.Threshold, VAD: cfg.VAD.Threshold}, MinInterval: time.Duration(cfg.MinIntervalMS) * time.Millisecond},
		Ring: ring, Stats: stats, Config: cfg,
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	animator, clearLED, ledErr := startWakeLED(o.LEDRoot)
	if ledErr != nil {
		return ledErr
	}
	defer func() { returnErr = errors.Join(returnErr, clearLED()) }()

	workers := []wakeWorker{
		fanout.Run,
		func(workerCtx context.Context) error {
			return pipeline.Run(workerCtx, subscriptionFrames{subscription}, makeWakeEventChannel(workerCtx, identity.DeviceID, animator))
		},
		func(workerCtx context.Context) error {
			return logWakeStats(workerCtx, o.StatsInterval, identity.DeviceID, model.ID, stats, subscription, capturer)
		},
	}
	buttonWorkers, err := startButtonWatchers(ctx, animator)
	if err != nil {
		return err
	}
	workers = append(workers, buttonWorkers...)
	if animator != nil {
		workers = append(workers, animator.Run)
	}

	sourceOwned = false
	runErr := runWakeWorkers(ctx, source, workers)
	slog.Info("echod stopped", "mode", "wake-only", "device_id", identity.DeviceID)
	return runErr
}

func runWakeWorkers(parent context.Context, source io.Closer, workers []wakeWorker) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan error, len(workers))
	for _, worker := range workers {
		go func() { results <- worker(ctx) }()
	}

	var firstErr error
	completed := 0
	select {
	case firstErr = <-results:
		completed = 1
	case <-parent.Done():
	}
	cancel()
	closeErr := source.Close()
	for ; completed < len(workers); completed++ {
		firstErr = errors.Join(firstErr, <-results)
	}
	return errors.Join(firstErr, closeErr)
}

func configureWakeLog(o opts) (func() error, error) {
	if o.LogFile == "" {
		return func() error { return nil }, nil
	}
	writer, err := system.NewRotatingWriter(o.LogFile, o.LogMaxBytes, 3)
	if err != nil {
		return nil, fmt.Errorf("open wake log: %w", err)
	}
	level := slog.LevelInfo
	if o.Dbg {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level})))
	return writer.Close, nil
}

func loadWakeSharedModels(store wake.Store) (oww.SharedModels, error) {
	load := func(name string) (*tflite.Model, error) {
		path := store.SharedPath(name)
		raw, err := os.ReadFile(path) //nolint:gosec // The operator-selected model store is the intended source.
		if err != nil {
			return nil, fmt.Errorf("read shared wake model %q: %w", path, err)
		}
		model, err := tflite.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse shared wake model %q: %w", path, err)
		}
		return model, nil
	}
	mel, err := load("melspectrogram.tflite")
	if err != nil {
		return oww.SharedModels{}, err
	}
	embedding, err := load("embedding_model.tflite")
	if err != nil {
		return oww.SharedModels{}, err
	}
	return oww.SharedModels{Mel: mel, Embedding: embedding}, nil
}

func openWakeOnlySource(o opts) (audio.PCMSource, error) {
	format := audio.Format{SampleRate: alsa.MicRate, Channels: alsa.MicChannels, Layout: audio.LayoutS24_3LE}
	if o.MicFromFile != "" {
		source, err := audio.NewFileSource(o.MicFromFile, format, true)
		if err != nil {
			return nil, fmt.Errorf("open paced microphone file: %w", err)
		}
		return source, nil
	}
	pcm, err := alsa.OpenCapture(alsa.Config{Card: alsa.MicCard, Device: alsa.MicDevice, Rate: alsa.MicRate, Channels: alsa.MicChannels, Format: alsa.MicFormat, PeriodFrames: alsa.MicPeriodFrames, Periods: alsa.MicPeriods})
	if err != nil {
		return nil, fmt.Errorf("open ALSA microphone: %w", err)
	}
	return pcmSource{pcm: pcm, format: format}, nil
}

type pcmSource struct {
	pcm    *alsa.PCM
	format audio.Format
}

func (s pcmSource) Format() audio.Format { return s.format }
func (s pcmSource) ReadInterleaved(buffer []byte) (int, error) {
	frames, err := s.pcm.ReadInterleaved(buffer)
	if err != nil {
		return frames, fmt.Errorf("read ALSA microphone: %w", err)
	}
	return frames, nil
}

func (s pcmSource) Close() error {
	if err := s.pcm.Close(); err != nil {
		return fmt.Errorf("close ALSA microphone: %w", err)
	}
	return nil
}

func makeWakeEventChannel(ctx context.Context, deviceID string, animator *led.Animator) chan wake.Event {
	events := make(chan wake.Event)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				slog.Info("wake accepted", "device_id", deviceID, "model_id", event.ModelID,
					"wake_score", event.WakeScore, "vad_score", event.InstantVADScore,
					"effective_vad_score", event.EffectiveVADScore, "vad_lookback_ms", event.VADLookbackMS,
					"step_ms", 80, "audio_position", event.AudioPosition, "preroll_samples", len(event.PreRoll))
				if animator != nil {
					animator.Set(protocol.StateListening)
				}
			}
		}
	}()
	return events
}

func logWakeStats(ctx context.Context, interval time.Duration, deviceID, modelID string, stats *wake.Stats, subscription *audio.Subscription, capturer *audio.Capturer) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var sampler system.Sampler
	if initial, err := system.ReadUsage("/proc/self"); err == nil {
		_ = sampler.CPUPercent(initial, time.Now())
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case sampledAt := <-ticker.C:
			usage, err := system.ReadUsage("/proc/self")
			if err != nil {
				return fmt.Errorf("sample wake resources: %w", err)
			}
			stats.SetFramesDropped(subscription.Dropped())
			snapshot := stats.Snapshot()
			slog.Info("wake statistics", "device_id", deviceID, "model_id", modelID,
				"wake_score", snapshot.LastWakeScore, "vad_score", snapshot.LastInstantVADScore,
				"effective_vad_score", snapshot.LastEffectiveVADScore, "vad_lookback_ms", snapshot.VADLookbackMS,
				"step_ms", 80, "rss_bytes", usage.RSSBytes, "cpu_percent", sampler.CPUPercent(usage, sampledAt),
				"wake_count", snapshot.WakeCount, "steps_processed", snapshot.StepsProcessed,
				"frames_dropped", snapshot.FramesDropped, "xruns", capturer.XRuns())
		}
	}
}

func startWakeLED(root string) (*led.Animator, func() error, error) {
	if _, err := os.Stat(filepath.Join(root, "frame")); errors.Is(err, os.ErrNotExist) {
		if root != led.DefaultRoot {
			return nil, nil, fmt.Errorf("probe LED controller: configured frame %q: %w", filepath.Join(root, "frame"), err)
		}
		slog.Debug("LED controller unavailable", "root", root)
		return nil, func() error { return nil }, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("probe LED controller: %w", err)
	}
	device := led.New(root)
	ticker := time.NewTicker(50 * time.Millisecond)
	animator := led.NewAnimator(device, ticker.C)
	clearLED := func() error {
		ticker.Stop()
		if err := device.Clear(); err != nil {
			return fmt.Errorf("clear LED on shutdown: %w", err)
		}
		return nil
	}
	return animator, clearLED, nil
}

func startButtonWatchers(ctx context.Context, animator *led.Animator) ([]wakeWorker, error) {
	devices, err := buttons.FindControlDevices(buttons.DefaultInputDir, buttons.DefaultSysClassDir)
	if errors.Is(err, buttons.ErrNoInputDevice) {
		slog.Debug("button devices unavailable")
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover button devices: %w", err)
	}
	presses := make(chan buttons.Press)
	streams := make([]*os.File, 0, len(devices))
	for _, device := range devices {
		stream, openErr := os.Open(device.Path)
		if openErr != nil {
			for _, opened := range streams {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("open button device %q: %w", device.Path, openErr)
		}
		streams = append(streams, stream)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case press := <-presses:
				slog.Info("button press", "button", press.Key.String(), "action", press.Action, "held", press.Held)
				if press.Key == buttons.KeyAction && press.Action == buttons.ActionTap {
					slog.Info("action button would start a turn", "milestone", 2)
					if animator != nil {
						animator.Set(protocol.StateListening)
						time.AfterFunc(300*time.Millisecond, func() { animator.Set(protocol.StateIdle) })
					}
				}
			}
		}
	}()
	workers := make([]wakeWorker, 0, len(devices))
	for index, device := range devices {
		watcher := buttons.NewWatcher(streams[index], device.Keys...)
		workers = append(workers, func(workerCtx context.Context) error {
			return watcher.Run(workerCtx, presses)
		})
	}
	return workers, nil
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
