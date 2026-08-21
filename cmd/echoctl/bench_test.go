package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
)

func TestWakeBench_RejectsNonPositiveSteps(t *testing.T) {
	err := wakeBench(&bytes.Buffer{}, benchCommand{Model: "okay_nabu", ModelDir: t.TempDir(), Steps: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "steps must be positive")
}

func TestWakeBench_ReportsMissingModelDirectory(t *testing.T) {
	err := wakeBench(&bytes.Buffer{}, benchCommand{Model: "okay_nabu", ModelDir: filepath.Join(t.TempDir(), "absent"), Steps: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "melspectrogram.tflite")
}

func TestBenchSyntheticFeed_ProducesContinuousSteps(t *testing.T) {
	feed := &benchSyntheticFeed{}
	first := feed.NextStep()
	second := feed.NextStep()
	assert.Len(t, first, wake.StepSamples)
	assert.Len(t, second, wake.StepSamples)
	assert.NotEqual(t, first, second, "successive steps should continue the waveform, not repeat it")
	require.NoError(t, feed.Close())
}

func TestBenchFixtureFeed_CyclesShorterFixture(t *testing.T) {
	path := writeRawFixture(t, make([]int16, wake.StepSamples/2))
	feed, err := newBenchFeed(path)
	require.NoError(t, err)
	defer func() { _ = feed.Close() }()

	fixture := feed.(*benchFixtureFeed)
	require.Less(t, len(fixture.samples), wake.StepSamples, "the fixture must be shorter than one step to exercise the cycle")

	steps := 3
	for range steps {
		step := feed.NextStep()
		assert.Len(t, step, wake.StepSamples)
	}
	assert.Equal(t, wake.StepSamples*steps, fixture.offset)
}

// writeRawFixture writes samples as raw 16 kHz mono S16LE PCM, the format newBenchFeed accepts
// for any non-.wav path.
func writeRawFixture(t *testing.T, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.raw")
	buf := new(bytes.Buffer)
	for _, s := range samples {
		require.NoError(t, binary.Write(buf, binary.LittleEndian, s))
	}
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path
}

func TestBenchFixtureFeed_RejectsMissingFile(t *testing.T) {
	_, err := newBenchFeed(filepath.Join(t.TempDir(), "missing.wav"))
	require.Error(t, err)
}

func TestBenchStagePercentiles_ReportsNearestRankAndMax(t *testing.T) {
	durations := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		10 * time.Millisecond,
	}
	stats := benchStagePercentiles(durations)
	assert.InDelta(t, 3, stats.P50Ms, 1e-9)
	assert.InDelta(t, 10, stats.P95Ms, 1e-9)
	assert.InDelta(t, 10, stats.MaxMs, 1e-9)
}

func TestBenchStagePercentiles_EmptyIsZeroValue(t *testing.T) {
	assert.Equal(t, benchStageStats{}, benchStagePercentiles(nil))
}

func TestShiftFloat32Ring_DropsOldestAndAppendsNewest(t *testing.T) {
	ring := []float32{1, 2, 3, 4}
	shiftFloat32Ring(ring, []float32{5, 6})
	assert.Equal(t, []float32{3, 4, 5, 6}, ring)
}

func TestBenchReportLines_IncludesEveryStageAndBudgetVerdict(t *testing.T) {
	report := benchReport{
		Model: "okay_nabu", ModelDir: ".assets/wake-models", Steps: 500,
		Mel:        benchStageStats{P50Ms: 1, P95Ms: 2, MaxMs: 3},
		Embedding:  benchStageStats{P50Ms: 1, P95Ms: 2, MaxMs: 3},
		Classifier: benchStageStats{P50Ms: 1, P95Ms: 2, MaxMs: 3},
		VAD:        benchStageStats{P50Ms: 0.01, P95Ms: 0.02, MaxMs: 0.03},
		Total:      benchStageStats{P50Ms: 3, P95Ms: 6, MaxMs: 9},
		BudgetMs:   20, WithinBudget: true, CPUPercent: 12.5, RSSBytes: 1024,
	}
	lines := benchReportLines(report)
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "model:      okay_nabu")
	assert.Contains(t, joined, "mel:")
	assert.Contains(t, joined, "embedding:")
	assert.Contains(t, joined, "classifier:")
	assert.Contains(t, joined, "vad:")
	assert.Contains(t, joined, "total:")
	assert.Contains(t, joined, "PASS")
}

func TestBenchBudgetVerdict_FailsWhenOverBudget(t *testing.T) {
	assert.Equal(t, "FAIL", benchBudgetVerdict(false))
	assert.Equal(t, "PASS", benchBudgetVerdict(true))
}

// requireBenchModelDir points at real vetted wake models when present, and skips otherwise. Real
// model binaries are never committed (docs/wake-models.md), so this end-to-end path is exercised
// only when an operator has downloaded them, matching the tflite package's own convention.
func requireBenchModelDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("ECHO_WAKE_MODEL_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", ".assets", "wake-models")
	}
	if _, err := os.Stat(filepath.Join(dir, "melspectrogram.tflite")); err != nil { //nolint:gosec // G703: dir is a fixed test-controlled fixture path, not attacker input.
		t.Skip("vetted wake models are not present; see docs/wake-models.md")
	}
	return dir
}

func TestWakeBench_RunsEndToEndAgainstRealModels(t *testing.T) {
	dir := requireBenchModelDir(t)
	var report bytes.Buffer
	// Steps must clear the embedding stream's warmup (the first ~76/8 steps produce no output
	// while the window fills) so the classifier is actually exercised at least once.
	err := wakeBench(&report, benchCommand{Model: "okay_nabu", ModelDir: dir, Steps: 16, JSON: true})
	require.NoError(t, err)

	var decoded benchReport
	require.NoError(t, json.Unmarshal(report.Bytes(), &decoded))
	assert.Equal(t, "okay_nabu", decoded.Model)
	assert.Equal(t, 16, decoded.Steps)
	assert.InDelta(t, 20.0, decoded.BudgetMs, 1e-9)
	assert.Positive(t, decoded.Mel.MaxMs)
	assert.Positive(t, decoded.Embedding.MaxMs)
	assert.Positive(t, decoded.Classifier.MaxMs)
	assert.GreaterOrEqual(t, decoded.VAD.MaxMs, 0.0)
	assert.Positive(t, decoded.Total.MaxMs)
}

func TestWakeBench_TextReportListsEveryStage(t *testing.T) {
	dir := requireBenchModelDir(t)
	var report bytes.Buffer
	require.NoError(t, wakeBench(&report, benchCommand{Model: "okay_nabu", ModelDir: dir, Steps: 4}))
	out := report.String()
	for _, want := range []string{"mel:", "embedding:", "classifier:", "vad:", "total:", "cpu:", "rss:", "budget:"} {
		assert.Contains(t, out, want)
	}
}

func TestWakeBench_ReplaysFixtureFile(t *testing.T) {
	dir := requireBenchModelDir(t)
	var report bytes.Buffer
	fixture := filepath.Join("..", "..", "testdata", "audio", "tone_1k_16k_mono.wav")
	require.NoError(t, wakeBench(&report, benchCommand{Model: "okay_nabu", ModelDir: dir, Steps: 4, FromFile: fixture, JSON: true}))
	var decoded benchReport
	require.NoError(t, json.Unmarshal(report.Bytes(), &decoded))
	assert.Equal(t, 4, decoded.Steps)
}
