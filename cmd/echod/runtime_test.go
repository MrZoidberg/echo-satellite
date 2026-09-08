package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	deviceconfig "github.com/MrZoidberg/echo-satellite/internal/device/config"
	"github.com/MrZoidberg/echo-satellite/internal/device/endpointing"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

type runtimeEngine struct {
	id     string
	closed int
	err    error
}

func (e *runtimeEngine) ID() string                   { return e.id }
func (*runtimeEngine) Kind() wake.Kind                { return wake.KindOpenWakeWord }
func (*runtimeEngine) Score([]int16) (float64, error) { return 0, nil }
func (*runtimeEngine) Reset()                         {}
func (e *runtimeEngine) Close() error                 { e.closed++; return e.err }

type runtimeDetector struct{ score float64 }

func (d *runtimeDetector) Observe([]int16)      {}
func (d *runtimeDetector) SpeechScore() float64 { return d.score }

type indicatorStub struct {
	mu    sync.Mutex
	calls []string
}

func (s *indicatorStub) Set(state protocol.DeviceState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, string(state))
}
func (s *indicatorStub) Off() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "off")
}
func (s *indicatorStub) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestConnectionIndicator_BootTimeoutDisconnectAndReconnect(t *testing.T) {
	original := connectionBootAnimationDuration
	connectionBootAnimationDuration = time.Millisecond
	t.Cleanup(func() { connectionBootAnimationDuration = original })
	indicator := &indicatorStub{}
	state := newConnectionIndicator(indicator)
	require.Eventually(t, func() bool { return len(indicator.Calls()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"thinking", "offline"}, indicator.Calls())
	state.SetConnected(true)
	assert.Equal(t, []string{"thinking", "offline", "off"}, indicator.Calls())
	state.SetConnected(false)
	assert.Equal(t, []string{"thinking", "offline", "off", "offline"}, indicator.Calls())
}

func TestConnectionIndicator_HoldsBootAnimationUntilTimeoutWhenConnected(t *testing.T) {
	original := connectionBootAnimationDuration
	connectionBootAnimationDuration = 20 * time.Millisecond
	t.Cleanup(func() { connectionBootAnimationDuration = original })
	indicator := &indicatorStub{}
	state := newConnectionIndicator(indicator)
	state.SetConnected(true)
	assert.Equal(t, []string{"thinking"}, indicator.Calls())
	require.Eventually(t, func() bool { return len(indicator.Calls()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"thinking", "off"}, indicator.Calls())
}

func TestTurnCoordinator_WakePreRollJoinsNextFanoutFrameWithoutGap(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	fanout := audio.NewFanout(nil)
	subscription, err := fanout.Subscribe("turn", 1)
	require.NoError(t, err)
	turns := newTurnCoordinator(subscription, controller)
	turns.SetConnected(true)
	preRoll := samples(1)
	turns.start(turnTrigger{start: protocol.TurnStart{Trigger: protocol.TriggerWake, Model: "okay", WakeScore: 0.8, VADScore: 0.8}, preRoll: preRoll, offset: 320})
	// A subscriber may receive a frame that overlaps the event boundary. The
	// coordinator must trim it, rather than duplicate or leave a sample gap.
	turns.observe(audio.Frame{Offset: 600, Samples: samples(2)})
	turns.finish(protocol.AudioStopEOF)
	turn, err := turns.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, turn.PCM, 2)
	assert.Equal(t, 320*2+280*2, len(turn.PCM[0])+len(turn.PCM[1]))
	assert.Equal(t, int16(1), decodePCM(turn.PCM[0])[0])
	assert.Equal(t, int16(2), decodePCM(turn.PCM[1])[0])
}

func TestTurnCoordinator_ButtonHasNoWakeDiagnosticsAndRejectsNestedTrigger(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	fanout := audio.NewFanout(nil)
	subscription, err := fanout.Subscribe("turn", 1)
	require.NoError(t, err)
	turns := newTurnCoordinator(subscription, controller)
	turns.SetConnected(true)
	require.NoError(t, turns.TriggerButton())
	require.ErrorIs(t, turns.TriggerButton(), endpointing.ErrActiveTurn)
	trigger := <-turns.triggers
	turns.start(trigger)
	turns.finish(protocol.AudioStopEOF)
	turn, err := turns.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, protocol.TriggerButton, turn.Start.Trigger)
	assert.Empty(t, turn.Start.Model)
	assert.Zero(t, turn.Start.WakeScore)
	assert.Zero(t, turn.Start.VADScore)
}

func TestTurnCoordinator_DropsOfflineTurnInsteadOfRetainingAudioForReconnect(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	fanout := audio.NewFanout(nil)
	subscription, err := fanout.Subscribe("turn", 1)
	require.NoError(t, err)
	turns := newTurnCoordinator(subscription, controller)
	turns.start(turnTrigger{start: protocol.TurnStart{Trigger: protocol.TriggerButton}})
	turns.observe(audio.Frame{Offset: 0, Samples: samples(7)})
	turns.finish(protocol.AudioStopEOF)
	assert.Empty(t, turns.done, "offline microphone audio must never survive for a later session")
	turns.SetConnected(true)
	assert.Empty(t, turns.done)
}

func TestTurnCoordinator_RejectsOfflineCaptureAcrossReconnect(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	turns := newTestTurnCoordinator(t, controller)
	require.ErrorIs(t, turns.TriggerButton(), endpointing.ErrActiveTurn)
	turns.SetConnected(true)
	require.NoError(t, turns.TriggerButton())
	trigger := <-turns.triggers
	turns.start(trigger)
	turns.observe(audio.Frame{Offset: 0, Samples: samples(7)})
	turns.SetConnected(false)
	turns.SetConnected(true)
	turns.finish(protocol.AudioStopEOF)
	assert.Empty(t, turns.done, "captured outage audio must never become reconnect audio")
}

func TestTurnCoordinator_CatchesUpFramesObservedBeforeWakeEvent(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	turns := newTestTurnCoordinator(t, controller)
	turns.SetConnected(true)
	turns.observe(audio.Frame{Offset: 0, Samples: samples(1)})
	turns.observe(audio.Frame{Offset: 320, Samples: samples(2)})
	turns.start(turnTrigger{start: protocol.TurnStart{Trigger: protocol.TriggerWake}, preRoll: samples(1), offset: 0})
	turns.finish(protocol.AudioStopEOF)
	turn, err := turns.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, turn.PCM, 2)
	assert.Equal(t, int16(1), decodePCM(turn.PCM[0])[0])
	assert.Equal(t, int16(2), decodePCM(turn.PCM[1])[0])
}

func TestTurnCoordinator_DisconnectRunsIdleCallbackAfterDrainingTurns(t *testing.T) {
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	turns := newTestTurnCoordinator(t, controller)
	turns.SetConnected(true)
	turns.start(turnTrigger{start: protocol.TurnStart{Trigger: protocol.TriggerButton}})
	called := 0
	turns.onIdle = func() { called++ }
	turns.SetConnected(false)
	assert.Equal(t, 1, called)
}

func TestDeviceRuntimeConfig_ExpandsPreRollBeforePersistingAndPublishesWholeRevision(t *testing.T) {
	state, candidate, expanded, swapped := newRuntimeConfigForTest(t, filepath.Join(t.TempDir(), "config.json"))
	result := state.Apply(candidate.ToProtocol())
	require.Equal(t, protocol.ConfigResultApplied, result.Status)
	assert.Equal(t, candidate.Wake.PreRollMS, expanded())
	assert.Equal(t, candidate.Version, state.current().Version)
	assert.Equal(t, 1, swapped())
	loaded, err := state.store.Load(deviceconfig.Bootstrap())
	require.NoError(t, err)
	assert.Equal(t, candidate, loaded)
}

func TestDeviceRuntimeConfig_DoesNotPublishRuntimeWhenPersistenceFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	state, candidate, _, swapped := newRuntimeConfigForTest(t, filepath.Join(blocker, "config.json"))
	result := state.Apply(candidate.ToProtocol())
	assert.Equal(t, protocol.ConfigResultRejected, result.Status)
	assert.Equal(t, "persistence_failed", result.Code)
	assert.Zero(t, state.current().Version)
	assert.Zero(t, swapped())
}

func TestSwappableWakeEngine_PublishesReplacementWhenOldCloseFails(t *testing.T) {
	old := &runtimeEngine{id: "old", err: errors.New("close failed")}
	next := &runtimeEngine{id: "next"}
	engine := &swappableWakeEngine{engine: old}
	require.Error(t, engine.Swap(next))
	assert.Equal(t, "next", engine.ID())
	assert.Equal(t, 1, old.closed)
}

func newRuntimeConfigForTest(t *testing.T, statePath string) (*deviceRuntimeConfig, deviceconfig.Settings, func() int, func() int) {
	t.Helper()
	controller, err := endpointing.New(protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, &runtimeDetector{})
	require.NoError(t, err)
	turns := newTurnCoordinator(nil, controller)
	candidate := deviceconfig.Bootstrap()
	candidate.Version, candidate.Wake.PreRollMS = 1, 1200
	var expanded, swapped int
	state := &deviceRuntimeConfig{
		store: deviceconfig.Store{Path: statePath}, settings: deviceconfig.Bootstrap(), turns: turns,
		validateWake:  func(deviceconfig.Settings) error { return nil },
		ensurePreRoll: func(value int) error { expanded = value; return nil },
		prepareWake:   func(deviceconfig.Settings) (wake.Engine, error) { return &runtimeEngine{id: "next"}, nil },
		swapWake:      func(wake.Engine) error { swapped++; return nil },
	}
	return state, candidate, func() int { return expanded }, func() int { return swapped }
}

func newTestTurnCoordinator(t *testing.T, controller *endpointing.Controller) *turnCoordinator {
	t.Helper()
	subscription, err := audio.NewFanout(nil).Subscribe("turn", 1)
	require.NoError(t, err)
	return newTurnCoordinator(subscription, controller)
}

func samples(value int16) []int16 {
	result := make([]int16, 320)
	for index := range result {
		result[index] = value
	}
	return result
}

func decodePCM(data []byte) []int16 {
	result := make([]int16, len(data)/2)
	for index := range result {
		result[index] = int16(binary.LittleEndian.Uint16(data[index*2:])) //nolint:gosec // G115: restores the signed PCM bit pattern.
	}
	return result
}
