package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/client"
	deviceconfig "github.com/MrZoidberg/echo-satellite/internal/device/config"
	"github.com/MrZoidberg/echo-satellite/internal/device/endpointing"
	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const simFrameSamples = 320

type timedResolver struct {
	resolver client.Resolver
	timeout  time.Duration
}

func (r timedResolver) Resolve(ctx context.Context, cfg discovery.Config, paired *discovery.Instance) (string, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	endpoint, err := r.resolver.Resolve(resolveCtx, cfg, paired)
	if err != nil {
		return "", fmt.Errorf("resolve within discovery timeout: %w", err)
	}
	return endpoint, nil
}

type simConfig struct {
	store     deviceconfig.Store
	bootstrap deviceconfig.Settings
	mu        sync.RWMutex
	settings  deviceconfig.Settings
}

func newSimConfig(store deviceconfig.Store, bootstrap deviceconfig.Settings) *simConfig {
	return &simConfig{store: store, bootstrap: bootstrap, settings: bootstrap}
}
func (c *simConfig) load() error {
	settings, err := c.store.Load(c.bootstrap)
	c.mu.Lock()
	c.settings = settings
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("load persisted simulator config: %w", err)
	}
	return nil
}
func (c *simConfig) current() deviceconfig.Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}
func (c *simConfig) Apply(value protocol.DeviceConfig) protocol.ConfigResult {
	candidate, err := deviceconfig.FromProtocol(value)
	if err != nil {
		return rejectedConfig(value.Version, "invalid_config", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.settings.Version != 0 {
		if err := deviceconfig.Compare(c.settings, candidate); err != nil {
			code := "conflicting_version"
			if errors.Is(err, deviceconfig.ErrStaleVersion) {
				code = "stale_version"
			}
			return rejectedConfig(value.Version, code, err)
		}
		if candidate.Version == c.settings.Version {
			return protocol.ConfigResult{Version: value.Version, Status: protocol.ConfigResultApplied}
		}
	}
	if err := c.store.Save(candidate); err != nil {
		return rejectedConfig(value.Version, "persistence_failed", err)
	}
	c.settings = candidate
	return protocol.ConfigResult{Version: value.Version, Status: protocol.ConfigResultApplied}
}
func rejectedConfig(version uint64, code string, err error) protocol.ConfigResult {
	return protocol.ConfigResult{Version: version, Status: protocol.ConfigResultRejected, Code: code, Detail: err.Error()}
}

type wavTurns struct {
	opts    opts
	config  *simConfig
	samples []int16
	mu      sync.Mutex
	sent    bool
}

func newWAVTurns(o opts, config *simConfig) (*wavTurns, error) {
	file, err := os.Open(o.Mic)
	if err != nil {
		return nil, fmt.Errorf("open mic WAV: %w", err)
	}
	defer func() { _ = file.Close() }()
	format, samples, err := audio.ReadWAV(file)
	if err != nil {
		return nil, fmt.Errorf("read mic WAV: %w", err)
	}
	if format != (audio.Format{SampleRate: 16_000, Channels: 1, Layout: audio.LayoutS16LE}) {
		return nil, fmt.Errorf("mic WAV must be mono 16 kHz signed 16-bit PCM, got %+v", format)
	}
	return &wavTurns{opts: o, config: config, samples: samples}, nil
}
func (s *wavTurns) Next(ctx context.Context) (client.Turn, error) {
	if err := ctx.Err(); err != nil {
		return client.Turn{}, fmt.Errorf("wait for simulated turn: %w", context.Cause(ctx))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent {
		return client.Turn{}, io.EOF
	}
	s.sent = true
	settings := s.config.current()
	controller, err := endpointing.NewDefault(settings.Endpointing)
	if err != nil {
		return client.Turn{}, fmt.Errorf("create endpoint controller: %w", err)
	}
	if err = controller.Start(0); err != nil {
		return client.Turn{}, fmt.Errorf("start endpoint controller: %w", err)
	}
	start, err := s.opts.turnStart()
	if err != nil {
		return client.Turn{}, err
	}
	if start.Trigger == protocol.TriggerWake {
		start.Model, start.PreRollMS = settings.Wake.Model, settings.Wake.PreRollMS
	}
	frames := make([][]byte, 0, (len(s.samples)+simFrameSamples-1)/simFrameSamples)
	reason := protocol.AudioStopEOF
	for from := 0; from < len(s.samples); from += simFrameSamples {
		to := min(from+simFrameSamples, len(s.samples))
		frame := s.samples[from:to]
		frames = append(frames, encodeSamples(frame))
		if stop, done := controller.Observe(frame); done {
			reason = stop
			break
		}
	}
	if reason == protocol.AudioStopEOF {
		if stop, done := controller.EOF(); done {
			reason = stop
		}
	}
	return client.Turn{ID: "dotsim-turn-1", Start: start, PCM: frames, Reason: reason}, nil
}
func encodeSamples(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample)) //nolint:gosec // G115: conversion preserves the signed PCM bit pattern.
	}
	return data
}
