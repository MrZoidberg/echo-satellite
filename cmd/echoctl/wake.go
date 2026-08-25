package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/oww"
	"github.com/MrZoidberg/echo-satellite/internal/device/wake/vadlevel"
)

func wakeInstall(w io.Writer, command wakeInstallCommand) error {
	store := wake.Store{Root: command.ModelDir}
	model, err := store.Install(wake.InstallRequest{
		ID: command.Args.ID, SourcePath: command.From, SidecarPath: command.Metadata,
		ExpectedSHA256: command.SHA256, Overwrite: command.Overwrite,
	})
	if err != nil {
		return fmt.Errorf("install wake model: %w", err)
	}
	return writeReport(w, []string{
		"installed: " + model.ID,
		fmt.Sprintf("kind:      %s", model.Kind),
		"sha256:    " + model.SHA256,
	})
}

func wakeList(w io.Writer, command wakeListCommand) error {
	store := wake.Store{Root: command.ModelDir}
	models, err := store.List()
	if err != nil {
		return fmt.Errorf("list wake models: %w", err)
	}
	sharedMel := filePresent(store.SharedPath("melspectrogram.tflite"))
	sharedEmbedding := filePresent(store.SharedPath("embedding_model.tflite"))
	lines := []string{fmt.Sprintf("shared: mel=%t embedding=%t", sharedMel, sharedEmbedding)}
	if len(models) == 0 {
		lines = append(lines, "models: none")
	}
	for _, model := range models {
		digest := model.SHA256
		if len(digest) > 12 {
			digest = digest[:12]
		}
		languages := strings.Join(model.Languages, ",")
		if languages == "" {
			languages = "n/a"
		}
		lines = append(lines, fmt.Sprintf(
			"model: %s kind=%s phrase=%q languages=%s size=%d sha256=%s",
			model.ID, model.Kind, model.Phrase, languages, model.Size, digest,
		))
	}
	return writeReport(w, lines)
}

func filePresent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

type diagnosticFrames struct{ frames chan audio.Frame }

func (s diagnosticFrames) Frames() <-chan audio.Frame { return s.frames }

func wakeTest(w io.Writer, command wakeTestCommand) error {
	if command.Threshold < 0 || command.Threshold > 1 || command.VADThreshold < 0 || command.VADThreshold > 1 || command.PreRollMS < 0 {
		return errors.New("wake thresholds must be in [0,1] and pre-roll must not be negative")
	}
	if command.Seconds < 0 {
		return errors.New("wake diagnostic duration must not be negative")
	}
	store := wake.Store{Root: command.ModelDir}
	model, err := store.Get(command.Model)
	if err != nil {
		return fmt.Errorf("load wake model: %w", err)
	}
	mel, err := loadBenchModel(command.ModelDir, "melspectrogram.tflite")
	if err != nil {
		return fmt.Errorf("create pre-roll ring: %w", err)
	}
	embedding, err := loadBenchModel(command.ModelDir, "embedding_model.tflite")
	if err != nil {
		return fmt.Errorf("create wake capturer: %w", err)
	}
	engine, err := oww.New(oww.SharedModels{Mel: mel, Embedding: embedding}, model)
	if err != nil {
		return fmt.Errorf("prepare wake engine: %w", err)
	}
	defer func() { _ = engine.Close() }()
	vad := vadlevel.NewScorer()
	defer func() { _ = vad.Close() }()
	config := wake.Defaults()
	config.Model = model.ID
	config.Threshold = command.Threshold
	config.VAD.Enabled = !command.NoVAD
	config.VAD.Threshold = command.VADThreshold
	config.PreRollMS = command.PreRollMS
	stats := wake.NewStats(wake.StatsConfig{ActiveModelID: model.ID, ModelKind: model.Kind, Languages: model.Languages, Thresholds: wake.Thresholds{Wake: command.Threshold, VAD: command.VADThreshold}, VADEnabled: !command.NoVAD})
	ring, err := audio.NewRing(audio.Format{SampleRate: wake.SampleRate, Channels: 1, Layout: audio.LayoutS16LE}, time.Duration(command.PreRollMS)*time.Millisecond)
	if err != nil {
		return fmt.Errorf("create pre-roll ring: %w", err)
	}
	pipeline := wake.Pipeline{Engines: []wake.Engine{engine}, VAD: vad, Gate: wake.Gate{Thresholds: wake.Thresholds{Wake: command.Threshold, VAD: command.VADThreshold}, MinInterval: time.Duration(config.MinIntervalMS) * time.Millisecond}, Ring: ring, Stats: stats, Config: config}
	return runWakeDiagnostic(w, command.wakeInputOptions, pipeline, stats, command.SavePreRoll)
}

func runWakeDiagnostic(w io.Writer, input wakeInputOptions, pipeline wake.Pipeline, stats *wake.Stats, saveDir string) error {
	started := time.Now()
	source, channels, err := openWakeSource(input.FromFile)
	if err != nil {
		return fmt.Errorf("create wake capturer: %w", err)
	}
	defer func() { _ = source.Close() }()
	ctx, cancel := diagnosticContext(input.FromFile, input.Seconds)
	defer cancel()
	capturer, err := audio.NewCapturer(source, audio.CaptureConfig{Device: source.Format(), Channels: channels, Preprocessor: audio.Bypass{}, StepSamples: wake.StepSamples}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return fmt.Errorf("create VAD capturer: %w", err)
	}
	var accepted []wake.Event
	captureErr := capturer.Run(ctx, func(frame audio.Frame) error {
		frames := make(chan audio.Frame, 1)
		frames <- frame
		close(frames)
		events := make(chan wake.Event, len(pipeline.Engines))
		if pipelineErr := pipeline.Run(ctx, diagnosticFrames{frames}, events); pipelineErr != nil {
			return fmt.Errorf("run wake pipeline: %w", pipelineErr)
		}
		close(events)
		for event := range events {
			accepted = append(accepted, event)
		}
		if input.PrintSteps {
			snapshot := stats.Snapshot()
			if _, writeErr := fmt.Fprintf(w, "step time=%.3fs wake=%.4f vad=%.4f\n", float64(frame.Offset)/audio.CanonicalSampleRate, snapshot.LastWakeScore, snapshot.LastVADScore); writeErr != nil {
				return fmt.Errorf("write wake step: %w", writeErr)
			}
		}
		return nil
	})
	if captureErr != nil {
		return fmt.Errorf("capture wake audio: %w", captureErr)
	}
	for _, event := range accepted {
		if _, err = fmt.Fprintf(w, "wake model=%s wake=%.4f vad=%.4f elapsed=%s preroll_samples=%d\n", event.ModelID, event.WakeScore, event.VADScore, event.At.Sub(started).Round(time.Millisecond), len(event.PreRoll)); err != nil {
			return fmt.Errorf("write wake event: %w", err)
		}
		if saveDir != "" {
			if saveErr := savePreRoll(saveDir, event); saveErr != nil {
				return saveErr
			}
		}
	}
	usage, err := systemUsage()
	if err != nil {
		return err
	}
	return writeWakeSummary(w, stats.Snapshot(), usage)
}

func wakeVADTest(w io.Writer, command wakeVADTestCommand) error {
	if command.Threshold < 0 || command.Threshold > 1 {
		return errors.New("VAD threshold must be in [0,1]")
	}
	if command.Seconds < 0 {
		return errors.New("VAD diagnostic duration must not be negative")
	}
	source, channels, err := openWakeSource(command.FromFile)
	if err != nil {
		return fmt.Errorf("create VAD capturer: %w", err)
	}
	defer func() { _ = source.Close() }()
	ctx, cancel := diagnosticContext(command.FromFile, command.Seconds)
	defer cancel()
	capturer, err := audio.NewCapturer(source, audio.CaptureConfig{Device: source.Format(), Channels: channels, Preprocessor: audio.Bypass{}, StepSamples: wake.StepSamples}, nil)
	if err != nil {
		return fmt.Errorf("create VAD capturer: %w", err)
	}
	vad := vadlevel.NewScorer()
	defer func() { _ = vad.Close() }()
	var count, above int
	var sum, maxScore float64
	err = capturer.Run(ctx, func(frame audio.Frame) error {
		for len(frame.Samples) >= wake.StepSamples {
			score, e := vad.Score(frame.Samples[:wake.StepSamples])
			if e != nil {
				return fmt.Errorf("score VAD: %w", e)
			}
			count++
			sum += score
			maxScore = max(maxScore, score)
			if score >= command.Threshold {
				above++
			}
			if command.PrintSteps {
				_, e = fmt.Fprintf(w, "step=%d time=%.3fs vad=%.4f\n", count, float64((count-1)*wake.StepSamples)/wake.SampleRate, score)
			}
			frame.Samples = frame.Samples[wake.StepSamples:]
			if e != nil {
				return fmt.Errorf("write VAD step: %w", e)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("capture VAD audio: %w", err)
	}
	mean, fraction := 0.0, 0.0
	if count > 0 {
		mean = sum / float64(count)
		fraction = float64(above) / float64(count)
	}
	_, err = fmt.Fprintf(w, "steps: %d\nmean: %.4f\nmax: %.4f\nfraction_above_threshold: %.4f\n", count, mean, maxScore, fraction)
	if err != nil {
		return fmt.Errorf("write VAD summary: %w", err)
	}
	return nil
}

func openWakeSource(path string) (audio.PCMSource, []int, error) {
	raw := audio.Format{SampleRate: 16000, Channels: 9, Layout: audio.LayoutS24_3LE}
	if path != "" {
		s, e := audio.NewFileSource(path, raw, false)
		if e != nil {
			return nil, nil, fmt.Errorf("open wake audio: %w", e)
		}
		channels := []int{0}
		if s.Format().Channels == 1 {
			channels = []int{0}
		}
		return s, channels, nil
	}
	s, e := openCaptureSource(micRecordCommand{})
	return s, []int{0}, e
}
func diagnosticContext(path string, seconds float64) (context.Context, context.CancelFunc) {
	if path == "" && seconds > 0 {
		return context.WithTimeout(context.Background(), time.Duration(seconds*float64(time.Second)))
	}
	return context.WithCancel(context.Background())
}
func savePreRoll(dir string, event wake.Event) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create pre-roll directory: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.wav", event.ModelID, event.At.UnixNano()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // operator explicitly opted in and selected this output directory.
	if err != nil {
		return fmt.Errorf("create pre-roll WAV: %w", err)
	}
	format := audio.Format{SampleRate: wake.SampleRate, Channels: 1, Layout: audio.LayoutS16LE}
	sink, err := audio.NewWAVSink(f, format)
	if err == nil {
		raw := make([]byte, len(event.PreRoll)*2)
		for i, sample := range event.PreRoll {
			binary.LittleEndian.PutUint16(raw[i*2:], uint16(sample)) //nolint:gosec // conversion preserves the signed PCM bit pattern.
		}
		_, err = sink.WriteInterleaved(raw)
	}
	if err == nil {
		err = sink.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		return fmt.Errorf("write pre-roll WAV: %w", err)
	}
	return nil
}
