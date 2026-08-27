package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/system"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/tflite"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/vadlevel"
)

// The mel and embedding shapes are Task 18's oww engine design, reproduced here only far enough
// to measure a realistic per-step inference cost before that engine exists. melLookbackSamples
// matches oww.melLookback (3 * 160 = 480); everything else this file needs (the embedding window's
// frame/bin count, the classifier's frame count, and the embedding dimension) is read from each
// model's own input shape, exactly as the future oww engine will do.
const benchMelLookbackSamples = 3 * 160

const benchMelWindowSamples = wake.StepSamples + benchMelLookbackSamples

// benchBudgetMs is the §16/Task 17 target: combined wake plus VAD inference for one 80 ms step
// should fit well inside the step, leaving headroom for capture, playback, LED and the future
// turn stream.
const benchBudgetMs = 20.0

type benchStageStats struct {
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	MaxMs float64 `json:"max_ms"`
}

type benchReport struct {
	Model        string          `json:"model"`
	ModelDir     string          `json:"model_dir"`
	Steps        int             `json:"steps"`
	Mel          benchStageStats `json:"mel"`
	Embedding    benchStageStats `json:"embedding"`
	Classifier   benchStageStats `json:"classifier"`
	VAD          benchStageStats `json:"vad"`
	Total        benchStageStats `json:"total"`
	BudgetMs     float64         `json:"budget_ms"`
	WithinBudget bool            `json:"within_budget"`
	CPUPercent   float64         `json:"cpu_percent"`
	RSSBytes     uint64          `json:"rss_bytes"`
}

// wakeBench runs the mel, embedding and classifier models plus the VAD over c.Steps synthetic or
// fixture-replayed 80 ms steps and reports per-stage timing, CPU and RSS.
func wakeBench(w io.Writer, c benchCommand) error {
	if c.Steps <= 0 {
		return fmt.Errorf("steps must be positive: %d", c.Steps)
	}

	models, err := loadBenchModels(c.ModelDir, c.Model)
	if err != nil {
		return err
	}

	feed, err := newBenchFeed(c.FromFile)
	if err != nil {
		return err
	}
	defer func() { _ = feed.Close() }()

	vad := vadlevel.NewScorer()
	defer func() { _ = vad.Close() }()

	window := make([]int16, benchMelWindowSamples)
	clsRing := make([]float32, models.clsFrames*models.embedDims)

	stages := newBenchStageTimings(c.Steps)

	sampler := system.Sampler{}
	baseline, err := system.ReadUsage("/proc/self")
	if err != nil {
		return fmt.Errorf("read baseline resource usage: %w", err)
	}
	sampler.CPUPercent(baseline, time.Now())

	for step := range c.Steps {
		stepSamples := feed.NextStep()
		durations, stepErr := runBenchStep(models, vad, window, clsRing, stepSamples)
		if stepErr != nil {
			return fmt.Errorf("bench step %d: %w", step, stepErr)
		}
		stages.append(durations)
	}

	final, err := system.ReadUsage("/proc/self")
	if err != nil {
		return fmt.Errorf("read final resource usage: %w", err)
	}
	cpuPercent := sampler.CPUPercent(final, time.Now())

	total := benchStagePercentiles(stages.total)
	report := benchReport{
		Model:        c.Model,
		ModelDir:     c.ModelDir,
		Steps:        c.Steps,
		Mel:          benchStagePercentiles(stages.mel),
		Embedding:    benchStagePercentiles(stages.embedding),
		Classifier:   benchStagePercentiles(stages.classifier),
		VAD:          benchStagePercentiles(stages.vad),
		Total:        total,
		BudgetMs:     benchBudgetMs,
		WithinBudget: total.P95Ms <= benchBudgetMs,
		CPUPercent:   cpuPercent,
		RSSBytes:     final.RSSBytes,
	}

	if c.JSON {
		return writeBenchJSON(w, report)
	}
	return writeReport(w, benchReportLines(report))
}

// shiftFloat32Ring drops the oldest len(fresh) values of ring and appends fresh at the end. It
// panics if fresh does not fit, which callers guard against explicitly with a clearer error.
func shiftFloat32Ring(ring, fresh []float32) {
	copy(ring, ring[len(fresh):])
	copy(ring[len(ring)-len(fresh):], fresh)
}

type benchStageTimings struct {
	mel        []time.Duration
	embedding  []time.Duration
	classifier []time.Duration
	vad        []time.Duration
	total      []time.Duration
}

func newBenchStageTimings(steps int) *benchStageTimings {
	return &benchStageTimings{
		mel:        make([]time.Duration, 0, steps),
		embedding:  make([]time.Duration, 0, steps),
		classifier: make([]time.Duration, 0, steps),
		vad:        make([]time.Duration, 0, steps),
		total:      make([]time.Duration, 0, steps),
	}
}

func (t *benchStageTimings) append(d benchStepDurations) {
	t.mel = append(t.mel, d.mel)
	t.embedding = append(t.embedding, d.embedding)
	t.classifier = append(t.classifier, d.classifier)
	t.vad = append(t.vad, d.vad)
	t.total = append(t.total, d.total)
}

// benchModels holds the loaded interpreters plus the shapes read from their own input tensors,
// so nothing about frame or dimension counts is hard-coded beyond the mel window's lookback
// length. The embedding model is evaluated through tflite.Stream, matching Task 18's planned
// oww.Engine and EchoLocal's reference implementation: it recomputes only the rows the new mel
// frames touch instead of the whole 76-row window on every step. An earlier version of this
// benchmark ran the embedding model through a plain Interpreter.Invoke() over the full window each
// step, which measured a different (and far more expensive) computation than the pipeline this
// budget gates — see docs/device-diagnostics.md's 2026-08-21 entry for the corrected numbers.
type benchModels struct {
	mel        *tflite.Interpreter
	embedding  *tflite.Stream
	classifier *tflite.Interpreter

	embFrames, embBins   int
	clsFrames, embedDims int
}

func loadBenchModels(modelDir, model string) (*benchModels, error) {
	mel, err := loadBenchInterpreter(modelDir, "melspectrogram.tflite")
	if err != nil {
		return nil, err
	}
	embModel, err := loadBenchModel(modelDir, "embedding_model.tflite")
	if err != nil {
		return nil, err
	}
	classifier, err := loadBenchInterpreter(modelDir, model+".tflite")
	if err != nil {
		return nil, err
	}

	// The embedding model's own fixed input shape ([1, frames, bins, 1]) is read once via a
	// throwaway interpreter, outside the benchmark loop, purely to size the stream and the
	// classifier ring below; it is not the interpreter used to score steps.
	shapeProbe, err := tflite.New(embModel)
	if err != nil {
		return nil, fmt.Errorf("prepare embedding model for shape probe: %w", err)
	}
	embShape := shapeProbe.InputShape(0)
	if len(embShape) != 4 {
		return nil, fmt.Errorf("embedding model input shape %v: want 4 dimensions [1, frames, bins, 1]", embShape)
	}

	embedding, err := tflite.NewStream(embModel, embShape)
	if err != nil {
		return nil, fmt.Errorf("prepare embedding model stream: %w", err)
	}

	clsShape := classifier.InputShape(0)
	if len(clsShape) != 3 {
		return nil, fmt.Errorf("classifier model input shape %v: want 3 dimensions [1, frames, dims]", clsShape)
	}

	m := &benchModels{
		mel: mel, embedding: embedding, classifier: classifier,
		embFrames: embShape[1], embBins: embShape[2],
		clsFrames: clsShape[1], embedDims: clsShape[2],
	}
	mel.ResizeInput(0, []int{1, benchMelWindowSamples})
	classifier.ResizeInput(0, []int{1, m.clsFrames, m.embedDims})
	return m, nil
}

type benchStepDurations struct {
	mel, embedding, classifier, vad, total time.Duration
}

// runBenchStep advances window with stepSamples, then runs mel, embedding, classifier and VAD in
// their real streaming order: mel's freshest frames feed the embedding stream directly (no
// external ring — tflite.Stream keeps its own row buffer), the embedding output feeds the
// classifier ring, and the VAD scores the same raw step. window and clsRing persist across calls
// so the streaming state carries over exactly as the future oww engine's will.
//
// During warmup (the first ~melLookback/embedStride steps) the embedding stream has not yet
// accumulated a full window and Write returns no output; the classifier is skipped for those
// steps rather than fed a stale or zeroed ring, matching what the real pipeline does.
func runBenchStep(m *benchModels, vad wake.VAD, window []int16, clsRing []float32, stepSamples []int16) (benchStepDurations, error) {
	// Keep the newest lookback tail, then place this step's new samples after it.
	copy(window[:benchMelLookbackSamples], window[len(window)-benchMelLookbackSamples:])
	copy(window[benchMelLookbackSamples:], stepSamples)

	start := time.Now()

	melInput := m.mel.Input(0).F32
	for i, s := range window {
		melInput[i] = float32(s)
	}
	melStart := time.Now()
	if err := m.mel.Invoke(); err != nil {
		return benchStepDurations{}, fmt.Errorf("invoke mel model: %w", err)
	}
	melDuration := time.Since(melStart)

	newFrames := m.mel.Output(0).F32
	if len(newFrames) == 0 || len(newFrames)%m.embBins != 0 {
		return benchStepDurations{}, fmt.Errorf(
			"mel model produced %d values, want a positive multiple of %d bins", len(newFrames), m.embBins)
	}

	embStart := time.Now()
	embOut, err := m.embedding.Write(newFrames)
	if err != nil {
		return benchStepDurations{}, fmt.Errorf("write embedding stream: %w", err)
	}
	embDuration := time.Since(embStart)

	var clsDuration time.Duration
	if len(embOut) > 0 {
		if len(embOut) != m.embedDims {
			return benchStepDurations{}, fmt.Errorf("embedding model produced %d values, want %d", len(embOut), m.embedDims)
		}
		shiftFloat32Ring(clsRing, embOut)

		copy(m.classifier.Input(0).F32, clsRing)
		clsStart := time.Now()
		if err := m.classifier.Invoke(); err != nil {
			return benchStepDurations{}, fmt.Errorf("invoke classifier model: %w", err)
		}
		clsDuration = time.Since(clsStart)
	}

	vadStart := time.Now()
	if _, err := vad.Score(stepSamples); err != nil {
		return benchStepDurations{}, fmt.Errorf("score VAD: %w", err)
	}
	vadDuration := time.Since(vadStart)

	return benchStepDurations{
		mel: melDuration, embedding: embDuration, classifier: clsDuration, vad: vadDuration,
		total: time.Since(start),
	}, nil
}

func loadBenchInterpreter(modelDir, filename string) (*tflite.Interpreter, error) {
	m, err := loadBenchModel(modelDir, filename)
	if err != nil {
		return nil, err
	}
	in, err := tflite.New(m)
	if err != nil {
		return nil, fmt.Errorf("prepare model %s: %w", filepath.Join(modelDir, filename), err)
	}
	return in, nil
}

// loadBenchModel reads and validates a model without wrapping it in an Interpreter, so callers
// that need a *tflite.Model directly (tflite.NewStream) do not pay for a throwaway invocation
// path they never use.
func loadBenchModel(modelDir, filename string) (*tflite.Model, error) {
	path := filepath.Join(modelDir, filename)
	raw, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the model directory to benchmark.
	if err != nil {
		return nil, fmt.Errorf("read model %s: %w", path, err)
	}
	m, err := tflite.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse model %s: %w", path, err)
	}
	if unsupported := m.UnsupportedOpcodes(); len(unsupported) != 0 {
		return nil, fmt.Errorf("model %s uses unsupported opcodes: %v", path, unsupported)
	}
	return m, nil
}

// benchStagePercentiles reports p50/p95/max in milliseconds using nearest-rank percentiles, so
// the numbers are always durations that were actually observed rather than interpolated.
func benchStagePercentiles(durations []time.Duration) benchStageStats {
	if len(durations) == 0 {
		return benchStageStats{}
	}
	sorted := append([]time.Duration(nil), durations...)
	slices.Sort(sorted)
	return benchStageStats{
		P50Ms: durationMs(nearestRank(sorted, 0.50)),
		P95Ms: durationMs(nearestRank(sorted, 0.95)),
		MaxMs: durationMs(sorted[len(sorted)-1]),
	}
}

func nearestRank(sorted []time.Duration, p float64) time.Duration {
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	idx = max(idx, 0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func durationMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func writeBenchJSON(w io.Writer, report benchReport) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bench report: %w", err)
	}
	if _, err = w.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write bench report: %w", err)
	}
	return nil
}

func benchReportLines(r benchReport) []string {
	stage := func(name string, s benchStageStats) string {
		return fmt.Sprintf("%-10s p50 %8.3fms  p95 %8.3fms  max %8.3fms", name+":", s.P50Ms, s.P95Ms, s.MaxMs)
	}
	return []string{
		fmt.Sprintf("model:      %s (%s)", r.Model, r.ModelDir),
		fmt.Sprintf("steps:      %d", r.Steps),
		stage("mel", r.Mel),
		stage("embedding", r.Embedding),
		stage("classifier", r.Classifier),
		stage("vad", r.VAD),
		stage("total", r.Total),
		fmt.Sprintf("cpu:        %.1f%%", r.CPUPercent),
		fmt.Sprintf("rss:        %d bytes", r.RSSBytes),
		fmt.Sprintf("budget:     %.0fms combined wake + VAD, %s", r.BudgetMs, benchBudgetVerdict(r.WithinBudget)),
	}
}

func benchBudgetVerdict(within bool) string {
	if within {
		return "PASS"
	}
	return "FAIL"
}

// benchFeed produces successive wake.StepSamples windows of 16 kHz mono s16 PCM.
type benchFeed interface {
	NextStep() []int16
	Close() error
}

// benchSyntheticFeed is a deterministic two-tone, slow-envelope waveform: the same shape as the
// tflite package's reference fixture, generated independently so bench does not depend on test
// files, and continuous across step boundaries rather than repeating the same 80 ms window.
type benchSyntheticFeed struct {
	sampleIndex int
}

func (f *benchSyntheticFeed) NextStep() []int16 {
	out := make([]int16, wake.StepSamples)
	for i := range out {
		t := float64(f.sampleIndex+i) / float64(wake.SampleRate)
		env := 0.4 + 0.6*math.Sin(2*math.Pi*3*t)
		v := env * (6000*math.Sin(2*math.Pi*440*t) + 2500*math.Sin(2*math.Pi*1750*t))
		out[i] = int16(v)
	}
	f.sampleIndex += wake.StepSamples
	return out
}

func (*benchSyntheticFeed) Close() error { return nil }

// benchFixtureFeed replays a 16 kHz mono s16 PCM fixture (raw or WAV), cycling back to the start
// once exhausted so --steps can exceed the fixture's own length.
type benchFixtureFeed struct {
	samples []int16
	offset  int
}

func newBenchFeed(fromFile string) (benchFeed, error) {
	if fromFile == "" {
		return &benchSyntheticFeed{}, nil
	}
	return newBenchFixtureFeed(fromFile)
}

func newBenchFixtureFeed(path string) (*benchFixtureFeed, error) {
	format := audio.Format{SampleRate: wake.SampleRate, Channels: 1, Layout: audio.LayoutS16LE}
	source, err := audio.NewFileSource(path, format, false)
	if err != nil {
		return nil, fmt.Errorf("open bench fixture: %w", err)
	}
	defer func() { _ = source.Close() }()

	if source.Format().SampleRate != wake.SampleRate || source.Format().Channels != 1 {
		return nil, fmt.Errorf("bench fixture must be %d Hz mono, got %d Hz %d channel(s)",
			wake.SampleRate, source.Format().SampleRate, source.Format().Channels)
	}

	var samples []int16
	raw := make([]byte, 4096*source.Format().BytesPerFrame())
	decoded := make([]int16, 4096)
	for {
		frames, readErr := source.ReadInterleaved(raw)
		if frames > 0 {
			n, decodeErr := audio.DecodeS16LE(decoded[:frames], raw[:frames*source.Format().BytesPerFrame()])
			if decodeErr != nil {
				return nil, fmt.Errorf("decode bench fixture: %w", decodeErr)
			}
			samples = append(samples, decoded[:n]...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("read bench fixture: %w", readErr)
		}
		if frames == 0 {
			break
		}
	}
	if len(samples) == 0 {
		return nil, errors.New("bench fixture contains no samples")
	}
	return &benchFixtureFeed{samples: samples}, nil
}

func (f *benchFixtureFeed) NextStep() []int16 {
	out := make([]int16, wake.StepSamples)
	for i := range out {
		out[i] = f.samples[f.offset%len(f.samples)]
		f.offset++
	}
	return out
}

func (*benchFixtureFeed) Close() error { return nil }
