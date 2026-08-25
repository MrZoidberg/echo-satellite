package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatus_JSONContainsEverySection16DiagnosticField(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	serialPath := filepath.Join(root, "sys", "devices", "soc0", "serial_number")
	require.NoError(t, os.MkdirAll(filepath.Dir(serialPath), 0o700))
	require.NoError(t, os.WriteFile(serialPath, []byte("DOT-123\n"), 0o600))

	var output bytes.Buffer
	require.NoError(t, deviceStatus(&output, statusCommand{JSON: true, Root: root, ModelDir: filepath.Join(root, "models"), Model: "okay_nabu"}))
	var report map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	assert.Equal(t, "dot-123", report["device_id"])
	wakeReport := report["wake"].(map[string]any)
	stats := wakeReport["stats"].(map[string]any)
	for _, field := range []string{"active_model_id", "model_kind", "languages", "wake_threshold", "vad_enabled", "vad_threshold", "last_wake_score", "last_vad_score", "max_wake_score", "wake_count", "rejected_high_wake_low_vad_count", "steps_processed", "frames_dropped", "wake_inference", "vad_inference"} {
		assert.Contains(t, stats, field)
	}
	assert.Contains(t, report, "resources")
	resources, ok := report["resources"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, resources, "cpu_time_ms")
	assert.Contains(t, resources, "rss_bytes")
	assert.Contains(t, report, "hardware")
}
