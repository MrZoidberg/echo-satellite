package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/client"
	deviceconfig "github.com/MrZoidberg/echo-satellite/internal/device/config"
	"github.com/MrZoidberg/echo-satellite/internal/device/endpointing"
	"github.com/MrZoidberg/echo-satellite/internal/device/led"
	"github.com/MrZoidberg/echo-satellite/internal/device/system"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/oww"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/vadlevel"
	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/discovery/mdns"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

type timedResolver struct {
	resolver client.Resolver
	timeout  time.Duration
}

var connectionBootAnimationDuration = 4 * time.Second

// connectionIndicator owns the LED contract around gateway availability:
// animated blue while booting, red while offline, and off while connected.
type connectionIndicator struct {
	animator     indicatorLED
	mu           sync.Mutex
	connected    bool
	hasConnected bool
	booting      bool
	timer        *time.Timer
}

type indicatorLED interface {
	Set(protocol.DeviceState)
	Off()
}

func newConnectionIndicator(animator indicatorLED) *connectionIndicator {
	indicator := &connectionIndicator{animator: animator, booting: true}
	animator.Set(protocol.StateThinking)
	indicator.timer = time.AfterFunc(connectionBootAnimationDuration, func() {
		indicator.mu.Lock()
		defer indicator.mu.Unlock()
		indicator.booting = false
		if indicator.connected {
			indicator.animator.Off()
		} else {
			indicator.animator.Set(protocol.StateOffline)
		}
	})
	return indicator
}

func (i *connectionIndicator) SetConnected(connected bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.connected = connected
	if connected {
		i.hasConnected = true
		if !i.booting {
			i.animator.Off()
		}
		return
	}
	if i.hasConnected {
		i.booting = false
		if i.timer != nil {
			i.timer.Stop()
		}
		i.animator.Set(protocol.StateOffline)
	}
}

func (i *connectionIndicator) IsBooting() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.booting
}

func (r timedResolver) Resolve(ctx context.Context, cfg discovery.Config, paired *discovery.Instance) (discovery.Endpoint, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	endpoint, err := r.resolver.Resolve(resolveCtx, cfg, paired)
	if err != nil {
		return discovery.Endpoint{}, fmt.Errorf("resolve within discovery timeout: %w", err)
	}
	return endpoint, nil
}

func rejectedConfig(version uint64, code string, err error) protocol.ConfigResult {
	return protocol.ConfigResult{Version: version, Status: protocol.ConfigResultRejected, Code: code, Detail: err.Error()}
}

func wakeSummary(settings deviceconfig.Settings) protocol.WakeConfig {
	return protocol.WakeConfig{Engine: settings.Wake.Engine, Models: []string{settings.Wake.Model}, WakeThreshold: settings.Wake.Threshold, VADThreshold: settings.Wake.VAD.Threshold, PreRollMS: settings.Wake.PreRollMS}
}

// deviceRuntimeConfig serializes desired-state changes with the idle boundary.
// The model check deliberately happens before persistence, so a bad gateway
// configuration cannot replace the local last-known-good revision.
type deviceRuntimeConfig struct {
	store         deviceconfig.Store
	settings      deviceconfig.Settings
	turns         *turnCoordinator
	models        wake.Store
	validateWake  func(deviceconfig.Settings) error
	prepareWake   func(deviceconfig.Settings) (wake.Engine, error)
	ensurePreRoll func(int) error
	swapWake      func(wake.Engine) error
	mu            sync.Mutex
	pending       *deviceconfig.Settings
	pendingEngine wake.Engine
	report        func(protocol.ConfigResult)
}

func (c *deviceRuntimeConfig) current() deviceconfig.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings
}

func (c *deviceRuntimeConfig) Apply(value protocol.DeviceConfig) protocol.ConfigResult {
	candidate, err := deviceconfig.FromProtocol(value)
	if err != nil {
		return rejectedConfig(value.Version, "invalid_config", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.settings
	if c.pending != nil {
		current = *c.pending
	}
	//nolint:nestif // Version ordering distinguishes current, pending, and idempotent outcomes.
	if current.Version != 0 {
		if err = deviceconfig.Compare(current, candidate); err != nil {
			code := "conflicting_version"
			if errors.Is(err, deviceconfig.ErrStaleVersion) {
				code = "stale_version"
			}
			return rejectedConfig(value.Version, code, err)
		}
		if candidate.Version == current.Version {
			status := protocol.ConfigResultApplied
			if c.pending != nil {
				status = protocol.ConfigResultPending
			}
			return protocol.ConfigResult{Version: value.Version, Status: status}
		}
	}
	validateWake := c.validateWakeCandidate
	if c.validateWake != nil {
		validateWake = c.validateWake
	}
	if err = validateWake(candidate); err != nil {
		return rejectedConfig(value.Version, "model_unavailable", err)
	}
	if c.ensurePreRoll == nil {
		return rejectedConfig(value.Version, "resource_unavailable", errors.New("wake pre-roll allocator is unavailable"))
	}
	if err = c.ensurePreRoll(candidate.Wake.PreRollMS); err != nil {
		return rejectedConfig(value.Version, "resource_unavailable", err)
	}
	prepared, err := c.prepareWake(candidate)
	if err != nil {
		return rejectedConfig(value.Version, "model_unavailable", err)
	}
	if c.turns.Active() {
		if c.pendingEngine != nil {
			_ = c.pendingEngine.Close()
		}
		c.pending = &candidate
		c.pendingEngine = prepared
		return protocol.ConfigResult{Version: value.Version, Status: protocol.ConfigResultPending}
	}
	if err = c.applyLocked(candidate, prepared); err != nil {
		_ = prepared.Close()
		return rejectedConfig(value.Version, "persistence_failed", err)
	}
	return protocol.ConfigResult{Version: value.Version, Status: protocol.ConfigResultApplied}
}

func (c *deviceRuntimeConfig) validateWakeCandidate(candidate deviceconfig.Settings) error {
	if candidate.Wake.Engine != "openwakeword" {
		return fmt.Errorf("unsupported wake engine %q", candidate.Wake.Engine)
	}
	model, err := c.models.Get(candidate.Wake.Model)
	if err != nil {
		return fmt.Errorf("load configured wake model: %w", err)
	}
	if model.Kind != wake.KindOpenWakeWord {
		return fmt.Errorf("wake model %q is %s, not openwakeword", model.ID, model.Kind)
	}
	return nil
}

func (c *deviceRuntimeConfig) applyPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil || c.turns.Active() {
		return
	}
	if err := c.applyLocked(*c.pending, c.pendingEngine); err != nil {
		slog.Error("apply pending device config", "error", err, "version", c.pending.Version)
		if c.report != nil {
			c.report(rejectedConfig(c.pending.Version, "persistence_failed", err))
		}
		_ = c.pendingEngine.Close()
		c.pending, c.pendingEngine = nil, nil
		return
	}
	slog.Info("gateway config applied", "version", c.pending.Version)
	if c.report != nil {
		c.report(protocol.ConfigResult{Version: c.pending.Version, Status: protocol.ConfigResultApplied})
	}
	c.pending, c.pendingEngine = nil, nil
}

func (c *deviceRuntimeConfig) applyLocked(candidate deviceconfig.Settings, prepared wake.Engine) error {
	if err := c.store.Save(candidate); err != nil {
		return fmt.Errorf("persist device configuration: %w", err)
	}
	c.turns.controller.StageValidatedConfig(candidate.Endpointing)
	if err := c.swapWake(prepared); err != nil {
		// The new engine has already been published. A close failure affects only
		// disposal of the replaced resource and must not roll back persisted
		// desired state or leave settings reporting the old model.
		slog.Warn("close replaced wake model", "error", err)
	}
	c.settings = candidate
	return nil
}

// swappableWakeEngine changes model resources only while a turn is idle. A
// Score call holds the read lock for its whole inference operation, so closing
// the replaced engine cannot race local wake inference.
type swappableWakeEngine struct {
	mu     sync.RWMutex
	engine wake.Engine
}

func (e *swappableWakeEngine) ID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.engine.ID()
}
func (e *swappableWakeEngine) Kind() wake.Kind {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.engine.Kind()
}
func (e *swappableWakeEngine) Score(samples []int16) (float64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	score, err := e.engine.Score(samples)
	if err != nil {
		return 0, fmt.Errorf("score wake engine: %w", err)
	}
	return score, nil
}
func (e *swappableWakeEngine) Reset() {
	e.mu.RLock()
	defer e.mu.RUnlock()
	e.engine.Reset()
}
func (e *swappableWakeEngine) Swap(next wake.Engine) error {
	if next == nil {
		return errors.New("nil wake engine")
	}
	e.mu.Lock()
	previous := e.engine
	e.engine = next
	e.mu.Unlock()
	if err := previous.Close(); err != nil {
		return fmt.Errorf("close replaced wake engine: %w", err)
	}
	return nil
}
func (e *swappableWakeEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.engine.Close(); err != nil {
		return fmt.Errorf("close wake engine: %w", err)
	}
	return nil
}

type turnTrigger struct {
	start   protocol.TurnStart
	preRoll []int16
	offset  int64
}

// turnCoordinator is the sole active-audio subscriber. It receives all
// canonical capture frames, warms endpointing while idle, and exposes only
// completed local turns to the transport client.
type turnCoordinator struct {
	subscription *audio.Subscription
	controller   *endpointing.Controller
	done         chan client.Turn
	triggers     chan turnTrigger
	mu           sync.Mutex
	active       bool
	pending      bool
	connected    bool
	nextOffset   int64
	history      []audio.Frame
	historySize  int
	turn         client.Turn
	dropped      uint64
	onIdle       func()
}

func newTurnCoordinator(subscription *audio.Subscription, controller *endpointing.Controller) *turnCoordinator {
	return &turnCoordinator{subscription: subscription, controller: controller, done: make(chan client.Turn, 1), triggers: make(chan turnTrigger, 2)}
}

func (t *turnCoordinator) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

func (t *turnCoordinator) Trigger(start protocol.TurnStart, preRoll []int16, offset int64) error {
	if !start.Trigger.Valid() {
		return errors.New("invalid turn trigger")
	}
	request := turnTrigger{start: start, preRoll: append([]int16(nil), preRoll...), offset: offset}
	t.mu.Lock()
	if !t.connected || t.active || t.pending {
		t.mu.Unlock()
		return endpointing.ErrActiveTurn
	}
	t.pending = true
	t.mu.Unlock()
	select {
	case t.triggers <- request:
		return nil
	default:
		t.mu.Lock()
		t.pending = false
		t.mu.Unlock()
		return endpointing.ErrActiveTurn
	}
}

func (t *turnCoordinator) Next(ctx context.Context) (client.Turn, error) {
	for {
		select {
		case <-ctx.Done():
			return client.Turn{}, fmt.Errorf("wait for local turn: %w", context.Cause(ctx))
		case turn := <-t.done:
			if t.isConnected() {
				return turn, nil
			}
		}
	}
}

func (t *turnCoordinator) SetConnected(connected bool) {
	t.mu.Lock()
	t.connected = connected
	becameIdle := false
	if !connected {
		becameIdle = t.active || t.pending
		t.active, t.pending = false, false
		t.controller.Cancel()
	}
	t.mu.Unlock()
	if !connected {
	drainingTurns:
		for {
			select {
			case <-t.done:
			case <-t.triggers:
			default:
				break drainingTurns
			}
		}
	}
	if becameIdle && t.onIdle != nil {
		t.onIdle()
	}
}

func (t *turnCoordinator) isConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

func (t *turnCoordinator) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-t.triggers:
			t.start(request)
		case frame, ok := <-t.subscription.Frames:
			if !ok {
				t.finish(protocol.AudioStopEOF)
				return nil
			}
			t.observe(frame)
		}
	}
}

func (t *turnCoordinator) start(request turnTrigger) {
	t.mu.Lock()
	t.pending = false
	if !t.connected || t.active {
		t.mu.Unlock()
		return
	}
	if err := t.controller.Start(len(request.preRoll)); err != nil {
		t.mu.Unlock()
		return
	}
	t.active, t.nextOffset, t.dropped = true, request.offset+int64(len(request.preRoll)), t.subscription.Dropped()
	t.turn = client.Turn{ID: fmt.Sprintf("turn-%d", time.Now().UnixNano()), Start: request.start, PCM: make([][]byte, 0, 32)}
	if len(request.preRoll) > 0 {
		t.turn.PCM = append(t.turn.PCM, encodePCM(request.preRoll))
	}
	for _, frame := range t.history {
		if reason, done := t.appendFrame(frame); done {
			go t.finish(reason)
			break
		}
	}
	t.mu.Unlock()
}

func (t *turnCoordinator) observe(frame audio.Frame) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		t.nextOffset = frame.Offset + int64(len(frame.Samples))
		t.remember(frame)
		t.controller.Observe(frame.Samples) // continuously warms the independent endpoint VAD.
		return
	}
	if t.subscription.Dropped() != t.dropped {
		go t.finish(protocol.AudioStopCaptureOverrun)
		return
	}
	if reason, done := t.appendFrame(frame); done {
		go t.finish(reason)
	}
}

func (t *turnCoordinator) appendFrame(frame audio.Frame) (protocol.AudioStopReason, bool) {
	end := frame.Offset + int64(len(frame.Samples))
	if end <= t.nextOffset {
		return "", false
	}
	from := max(0, int(t.nextOffset-frame.Offset))
	if from > len(frame.Samples) || frame.Offset+int64(from) != t.nextOffset {
		return protocol.AudioStopCaptureOverrun, true
	}
	samples := frame.Samples[from:]
	t.turn.PCM = append(t.turn.PCM, encodePCM(samples))
	t.nextOffset += int64(len(samples))
	return t.controller.Observe(samples)
}

const turnHandoffHistorySamples = 5 * wake.SampleRate

func (t *turnCoordinator) remember(frame audio.Frame) {
	copyFrame := audio.Frame{Offset: frame.Offset, Samples: append([]int16(nil), frame.Samples...)}
	t.history = append(t.history, copyFrame)
	t.historySize += len(copyFrame.Samples)
	for t.historySize > turnHandoffHistorySamples && len(t.history) > 0 {
		t.historySize -= len(t.history[0].Samples)
		t.history = t.history[1:]
	}
}

func (t *turnCoordinator) TriggerButton() error {
	t.mu.Lock()
	offset := t.nextOffset
	t.mu.Unlock()
	return t.Trigger(protocol.TurnStart{Trigger: protocol.TriggerButton}, nil, offset)
}

func (t *turnCoordinator) finish(reason protocol.AudioStopReason) {
	t.mu.Lock()
	if !t.active {
		t.mu.Unlock()
		return
	}
	t.turn.Reason, t.active = reason, false
	turn := t.turn
	connected := t.connected
	t.mu.Unlock()
	if connected {
		select {
		case t.done <- turn:
		default:
		}
	}
	if t.onIdle != nil {
		t.onIdle()
	}
}

func encodePCM(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample)) //nolint:gosec // G115: retains PCM bit pattern.
	}
	return data
}

//nolint:gocyclo // Composition owns the one-time hardware and protocol wiring.
func runConnected(ctx context.Context, o opts) (returnErr error) {
	identity, err := system.Resolve(system.SerialReader{}, o.DeviceID, system.DeviceIDFile)
	if err != nil {
		return fmt.Errorf("resolve identity: %w", err)
	}
	animator, clearLED, err := startWakeLED(o.LEDRoot)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, clearLED()) }()
	var indicator *connectionIndicator
	ledErr := make(chan error, 1)
	if animator != nil {
		indicator = newConnectionIndicator(animator)
		go func() { ledErr <- animator.Run(ctx) }()
	}
	bootstrap := deviceconfig.Bootstrap()
	bootstrap.Wake = o.wakeConfig()
	if _, tokenErr := client.LoadToken(o.GatewayTokenFile); tokenErr != nil {
		return fmt.Errorf("load gateway token: %w", tokenErr)
	}
	store := deviceconfig.Store{Path: o.ConfigState}
	settings, loadErr := store.Load(bootstrap)
	if loadErr != nil {
		slog.Warn("using local bootstrap configuration", "error", loadErr)
	}
	controller, err := endpointing.NewDefault(settings.Endpointing)
	if err != nil {
		return fmt.Errorf("create endpoint detector: %w", err)
	}
	rawSource, err := openWakeOnlySource(o)
	if err != nil {
		return err
	}
	raw := &closeOncePCMSource{PCMSource: rawSource}
	defer func() { returnErr = errors.Join(returnErr, raw.Close()) }()
	channels, err := o.micChannelList()
	if err != nil {
		return err
	}
	capturer, err := audio.NewCapturer(raw, audio.CaptureConfig{Device: raw.Format(), Channels: channels, Preprocessor: audio.Bypass{}, StepSamples: wake.StepSamples}, slog.Default())
	if err != nil {
		return fmt.Errorf("create wake capturer: %w", err)
	}
	fanout := audio.NewFanout(capturer)
	wakeSub, err := fanout.Subscribe("wake", 8)
	if err != nil {
		return fmt.Errorf("subscribe wake pipeline: %w", err)
	}
	turnSub, err := fanout.Subscribe("turn", 32)
	if err != nil {
		return fmt.Errorf("subscribe turn coordinator: %w", err)
	}
	turns := newTurnCoordinator(turnSub, controller)
	models := wake.Store{Root: o.WakeModelDir}
	model, err := models.Get(settings.Wake.Model)
	if err != nil {
		return fmt.Errorf("load wake model: %w", err)
	}
	shared, err := loadWakeSharedModels(models)
	if err != nil {
		return err
	}
	engine, err := oww.New(shared, model)
	if err != nil {
		return fmt.Errorf("prepare wake engine: %w", err)
	}
	dynamicEngine := &swappableWakeEngine{engine: engine}
	defer func() { returnErr = errors.Join(returnErr, dynamicEngine.Close()) }()
	ring, err := audio.NewRing(audio.Format{SampleRate: wake.SampleRate, Channels: 1, Layout: audio.LayoutS16LE}, time.Duration(settings.Wake.PreRollMS)*time.Millisecond)
	if err != nil {
		return fmt.Errorf("create wake pre-roll ring: %w", err)
	}
	vad := vadlevel.NewScorer()
	defer func() { returnErr = errors.Join(returnErr, vad.Close()) }()
	state := &deviceRuntimeConfig{store: store, settings: settings, turns: turns, models: models, ensurePreRoll: func(milliseconds int) error {
		return ring.EnsureDuration(time.Duration(milliseconds) * time.Millisecond)
	}}
	state.prepareWake = func(candidate deviceconfig.Settings) (wake.Engine, error) {
		candidateModel, modelErr := models.Get(candidate.Wake.Model)
		if modelErr != nil {
			return nil, fmt.Errorf("load configured wake model: %w", modelErr)
		}
		next, prepareErr := oww.New(shared, candidateModel)
		if prepareErr != nil {
			return nil, fmt.Errorf("prepare configured wake model: %w", prepareErr)
		}
		return next, nil
	}
	state.swapWake = dynamicEngine.Swap
	pipeline := wake.Pipeline{Engines: []wake.Engine{dynamicEngine}, VAD: vad, Gate: wake.Gate{Thresholds: wake.Thresholds{Wake: settings.Wake.Threshold, VAD: settings.Wake.VAD.Threshold}, MinInterval: time.Duration(settings.Wake.MinIntervalMS) * time.Millisecond}, Ring: ring, Stats: wake.NewStats(wake.StatsConfig{}), Config: settings.Wake, ConfigSource: func() wake.Config { return state.current().Wake }}
	turns.onIdle = func() {
		state.applyPending()
		if animator != nil && turns.isConnected() && indicator != nil && !indicator.IsBooting() {
			animator.Off()
		}
	}
	resolver := timedResolver{resolver: discovery.NewResolver(mdns.NewDevice(), protocol.ProtocolVersion), timeout: time.Duration(o.DiscoveryTimeout) * time.Millisecond}
	session, err := client.New(client.Options{Discovery: o.discoveryConfig(), HelloSource: func() protocol.Hello {
		current := state.current()
		return protocol.Hello{DeviceID: identity.DeviceID, AgentVersion: revision, Protocol: protocol.ProtocolVersion, Capabilities: announcedCapabilities(), WakeConfig: wakeSummary(current), ConfigVersion: current.Version}
	}, Dialer: client.WSSDialer{}, Resolver: resolver, Pairings: discovery.PairingStore{Path: o.PairingState}, Config: state, TurnSource: turns, TokenPath: o.GatewayTokenFile, SkipTLSVerify: o.TLSSkipVerify, Logger: slog.Default(), SessionChanged: func(connected bool) {
		turns.SetConnected(connected)
		if indicator != nil {
			indicator.SetConnected(connected)
		}
	}})
	if err != nil {
		return fmt.Errorf("create gateway client: %w", err)
	}
	state.report = func(result protocol.ConfigResult) {
		if reportErr := session.ReportConfigResult(result); reportErr != nil {
			slog.Warn("report deferred config result", "error", reportErr, "version", result.Version)
		}
	}
	events := make(chan wake.Event)
	buttonWorkers, err := startButtonWatchers(ctx, animator, func() error {
		triggerErr := turns.TriggerButton()
		if triggerErr == nil && animator != nil {
			animator.Set(protocol.StateListening)
		}
		return triggerErr
	})
	if err != nil {
		return err
	}
	workers := []wakeWorker{fanout.Run, turns.Run, func(workerCtx context.Context) error {
		return pipeline.Run(workerCtx, subscriptionFrames{wakeSub}, events)
	}, func(workerCtx context.Context) error { return consumeWakeEvents(workerCtx, events, turns, animator) }, session.Run}
	workers = append(workers, buttonWorkers...)
	if animator != nil {
		workers = append(workers, func(workerCtx context.Context) error {
			select {
			case ledWorkerErr := <-ledErr:
				return ledWorkerErr
			case <-workerCtx.Done():
				return nil
			}
		})
	}
	return runWakeWorkers(ctx, raw, workers)
}

func consumeWakeEvents(ctx context.Context, events <-chan wake.Event, turns *turnCoordinator, animator *led.Service) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			start := protocol.TurnStart{Trigger: protocol.TriggerWake, Model: event.ModelID, WakeScore: event.WakeScore, VADScore: event.InstantVADScore, PreRollMS: len(event.PreRoll) * 1000 / wake.SampleRate}
			offset := int64(event.AudioPosition*wake.SampleRate/time.Second) - int64(len(event.PreRoll))
			if err := turns.Trigger(start, event.PreRoll, offset); err == nil {
				if animator != nil {
					animator.Set(protocol.StateListening)
				}
				slog.Info("wake accepted", "model_id", event.ModelID, "wake_score", event.WakeScore, "vad_score", event.InstantVADScore)
			}
		}
	}
}
