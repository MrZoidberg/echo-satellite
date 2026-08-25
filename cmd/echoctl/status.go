package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/buttons"
	"github.com/MrZoidberg/echo-satellite/internal/device/mixer"
	"github.com/MrZoidberg/echo-satellite/internal/device/system"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
)

type resourceStatus struct {
	CPUPercent float64 `json:"cpu_percent"`
	CPUTimeMS  float64 `json:"cpu_time_ms"`
	RSSBytes   uint64  `json:"rss_bytes"`
}
type probeStatus struct {
	Microphone bool   `json:"microphone_reachable"`
	Speaker    bool   `json:"speaker_reachable"`
	LED        bool   `json:"led_reachable"`
	Buttons    bool   `json:"buttons_reachable"`
	Amplifier  string `json:"amplifier_state"`
}
type wakeStatus struct {
	Config wakeConfigStatus `json:"config"`
	Models []modelStatus    `json:"models"`
	Stats  wake.Snapshot    `json:"stats"`
}
type modelStatus struct {
	ID        string    `json:"id"`
	Kind      wake.Kind `json:"kind"`
	Phrase    string    `json:"phrase,omitempty"`
	Languages []string  `json:"languages"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
}
type wakeConfigStatus struct {
	Enabled         bool    `json:"enabled"`
	Engine          string  `json:"engine"`
	Model           string  `json:"model"`
	Threshold       float64 `json:"threshold"`
	VADEnabled      bool    `json:"vad_enabled"`
	VADThreshold    float64 `json:"vad_threshold"`
	PreRollMS       int     `json:"preroll_ms"`
	MinIntervalMS   int     `json:"min_interval_ms"`
	AlwaysScoreWake bool    `json:"always_score_wake"`
}
type statusReport struct {
	DeviceID     string         `json:"device_id"`
	Serial       string         `json:"serial,omitempty"`
	SerialSource string         `json:"serial_source"`
	Hardware     probeStatus    `json:"hardware"`
	Wake         wakeStatus     `json:"wake"`
	Resources    resourceStatus `json:"resources"`
	CapturedAt   time.Time      `json:"captured_at"`
}

func deviceStatus(w io.Writer, c statusCommand) error {
	identity, err := system.Resolve(system.SerialReader{Root: c.Root}, "", filepath.Join(c.Root, "data", "local", "etc", "echo-satellite", "device-id"))
	if err != nil {
		return fmt.Errorf("resolve identity: %w", err)
	}
	source := "persisted_or_generated"
	if identity.Serial != "" {
		source = "hardware_serial"
	}
	models, err := (wake.Store{Root: c.ModelDir}).List()
	if err != nil {
		return fmt.Errorf("list wake models: %w", err)
	}
	config := wake.Defaults()
	config.Model = c.Model
	var active wake.Model
	for _, model := range models {
		if model.ID == c.Model {
			active = model
			break
		}
	}
	stats := wake.NewStats(wake.StatsConfig{ActiveModelID: config.Model, ModelKind: active.Kind, Languages: active.Languages, Thresholds: wake.Thresholds{Wake: config.Threshold, VAD: config.VAD.Threshold}, VADEnabled: config.VAD.Enabled}).Snapshot()
	usage, err := systemUsage()
	if err != nil {
		return err
	}
	report := statusReport{DeviceID: identity.DeviceID, Serial: identity.Serial, SerialSource: source, Hardware: probeHardware(c.Root), Wake: wakeStatus{Config: wakeConfigSnapshot(config), Models: modelSnapshots(models), Stats: stats}, Resources: usage, CapturedAt: time.Now().UTC()}
	if c.JSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("encode status JSON: %w", err)
		}
		return nil
	}
	return writeReport(w, []string{"device_id: " + report.DeviceID, "serial_source: " + report.SerialSource, fmt.Sprintf("models: %d", len(models)), fmt.Sprintf("cpu_percent: %.2f", usage.CPUPercent), fmt.Sprintf("rss_bytes: %d", usage.RSSBytes)})
}

func modelSnapshots(models []wake.Model) []modelStatus {
	result := make([]modelStatus, 0, len(models))
	for _, model := range models {
		result = append(result, modelStatus{ID: model.ID, Kind: model.Kind, Phrase: model.Phrase, Languages: model.Languages, SHA256: model.SHA256, Size: model.Size})
	}
	return result
}

func wakeConfigSnapshot(config wake.Config) wakeConfigStatus {
	return wakeConfigStatus{Enabled: config.Enabled, Engine: config.Engine, Model: config.Model, Threshold: config.Threshold, VADEnabled: config.VAD.Enabled, VADThreshold: config.VAD.Threshold, PreRollMS: config.PreRollMS, MinIntervalMS: config.MinIntervalMS, AlwaysScoreWake: config.AlwaysScoreWake}
}

func systemUsage() (resourceStatus, error) {
	usage, err := system.ReadUsage("/proc/self")
	if err != nil {
		return resourceStatus{}, fmt.Errorf("read resource usage: %w", err)
	}
	return resourceStatus{CPUTimeMS: float64(usage.CPUTime) / float64(time.Millisecond), RSSBytes: usage.RSSBytes}, nil
}
func writeWakeSummary(w io.Writer, snapshot wake.Snapshot, usage resourceStatus) error {
	raw, err := json.MarshalIndent(struct {
		Stats     wake.Snapshot  `json:"stats"`
		Resources resourceStatus `json:"resources"`
	}{snapshot, usage}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal wake summary: %w", err)
	}
	_, err = fmt.Fprintf(w, "statistics:\n%s\n", raw)
	if err != nil {
		return fmt.Errorf("write wake summary: %w", err)
	}
	return nil
}
func probeHardware(root string) probeStatus {
	at := func(path string) bool {
		info, err := os.Stat(filepath.Join(root, path))
		return err == nil && !info.IsDir()
	}
	_, buttonsErr := buttons.FindControlDevices(filepath.Join(root, "dev", "input"), filepath.Join(root, "sys", "class", "input"))
	amp := probeAmplifier(root, at)
	return probeStatus{Microphone: at("dev/snd/pcmC0D24c"), Speaker: at("dev/snd/pcmC0D23p"), LED: at("sys/bus/i2c/devices/0-003f/frame"), Buttons: buttonsErr == nil, Amplifier: amp}
}

func probeAmplifier(root string, at func(string) bool) string {
	if root != "/" && root != "" {
		if at("dev/snd/controlC0") {
			return "reachable (state unavailable under injected root)"
		}
		return "unreachable"
	}
	control, err := mixer.Open(0)
	if err != nil {
		return "unreachable"
	}
	defer func() { _ = control.Close() }()
	state, err := control.Get(mixer.ControlSpeakerAmp)
	if err != nil {
		return "reachable (state unavailable)"
	}
	return state
}
