package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
)

const scorecardMaxDelaySamples = 256

type micScorecardReport struct {
	Capture       micScorecardCapture   `json:"capture"`
	Position      string                `json:"position"`
	DistanceMM    int                   `json:"distance_mm"`
	Condition     string                `json:"condition"`
	XRuns         *uint64               `json:"xruns"`
	DroppedFrames *uint64               `json:"dropped_frames"`
	RawAudio      micScorecardRawAudio  `json:"raw_audio"`
	Channels      []micScorecardChannel `json:"channels"`
}

type micScorecardCapture struct {
	SampleRate int    `json:"sample_rate_hz"`
	Channels   int    `json:"channels"`
	Frames     int    `json:"frames"`
	Layout     string `json:"layout"`
	Health     string `json:"health"`
}

type micScorecardRawAudio struct {
	Disposition string `json:"disposition"`
}

type micScorecardChannel struct {
	Channel                 int      `json:"channel"`
	PeakDBFS                *float64 `json:"peak_dbfs"`
	RMSDBFS                 *float64 `json:"rms_dbfs"`
	ClippingFraction        float64  `json:"clipping_fraction"`
	CorrelationWithMic0     float64  `json:"correlation_with_mic0"`
	RelativeDelaySamples    int      `json:"relative_delay_samples"`
	Polarity                string   `json:"polarity"`
	NoiseFloorDBFS          *float64 `json:"noise_floor_dbfs,omitempty"`
	SpeechNoiseSeparationDB *float64 `json:"speech_noise_separation_db,omitempty"`
}

type micComparisonReport struct {
	Position         string                      `json:"position"`
	DistanceMM       int                         `json:"distance_mm"`
	Condition        string                      `json:"condition"`
	InputHealth      micComparisonHealth         `json:"input_health"`
	NoiseHealth      micComparisonHealth         `json:"noise_health"`
	InputDisposition string                      `json:"input_disposition"`
	Candidates       []audio.ConditioningMetrics `json:"candidates"`
}

type micComparisonHealth struct {
	Xruns         uint64 `json:"xruns"`
	DroppedFrames uint64 `json:"dropped_frames"`
}

type micCaptureHealth struct {
	SHA256        string `json:"sha256"`
	XRuns         uint64 `json:"xruns"`
	DroppedFrames uint64 `json:"dropped_frames"`
	SampleRate    int    `json:"sample_rate_hz"`
	Channels      []int  `json:"channels"`
	Frames        int    `json:"frames"`
}

func micRecord(w io.Writer, c micRecordCommand) error {
	if c.Seconds <= 0 {
		return fmt.Errorf("seconds must be positive: %g", c.Seconds)
	}
	source, err := openCaptureSource(c)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	channels, err := parseMicChannels(c.Channels, source.Format().Channels)
	if err != nil {
		return err
	}
	file, err := os.Create(c.Out)
	if err != nil {
		return fmt.Errorf("create microphone WAV: %w", err)
	}
	defer func() { _ = file.Close() }()
	wav, err := audio.NewWAVWriter(file, audio.Format{SampleRate: source.Format().SampleRate, Channels: len(channels), Layout: audio.LayoutS16LE})
	if err != nil {
		return fmt.Errorf("create microphone WAV writer: %w", err)
	}
	limit := int(math.Round(c.Seconds * float64(source.Format().SampleRate)))
	peaks, sums, count := make([]float64, len(channels)), make([]float64, len(channels)), 0
	raw := make([]byte, 320*source.Format().BytesPerFrame())
	decoded := make([]int16, 320*source.Format().Channels)
	selected := make([]int16, 320*len(channels))
	for count < limit {
		frames, readErr := source.ReadInterleaved(raw)
		if frames > limit-count {
			frames = limit - count
		}
		if frames > 0 {
			n, decodeErr := decodeCapture(decoded, raw[:frames*source.Format().BytesPerFrame()], source.Format().Layout)
			if decodeErr != nil {
				return decodeErr
			}
			n, selectErr := audio.SelectChannels(selected, decoded[:n], source.Format().Channels, channels)
			if selectErr != nil {
				return fmt.Errorf("select microphone channels: %w", selectErr)
			}
			if _, err = wav.Write(selected[:n]); err != nil {
				return fmt.Errorf("write microphone WAV: %w", err)
			}
			accumulateLevels(selected[:n], len(channels), peaks, sums)
			count += frames
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read microphone PCM: %w", readErr)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err = wav.Close(); err != nil {
		return fmt.Errorf("close microphone WAV: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close microphone output: %w", err)
	}
	if err = source.Close(); err != nil {
		return fmt.Errorf("close microphone capture: %w", err)
	}
	if count != limit {
		return fmt.Errorf("microphone capture incomplete: got %d frames, want %d", count, limit)
	}
	if err := writeCaptureHealth(c, source.Format().SampleRate, channels, count); err != nil {
		return err
	}
	return writeMicRecordReport(w, c, source.Format().SampleRate, channels, count, peaks, sums)
}

func writeCaptureHealth(c micRecordCommand, sampleRate int, channels []int, frames int) error {
	if c.HealthOut == "" {
		return nil
	}
	digest, err := fileSHA256(c.Out)
	if err != nil {
		return err
	}
	if err := writeJSONFile(c.HealthOut, micCaptureHealth{SHA256: digest, SampleRate: sampleRate, Channels: channels, Frames: frames}); err != nil {
		return fmt.Errorf("write microphone capture health: %w", err)
	}
	return nil
}

func writeMicRecordReport(w io.Writer, c micRecordCommand, sampleRate int, channels []int, count int, peaks, sums []float64) error {
	lines := []string{fmt.Sprintf("recorded: %s (%d Hz, %d channels, %d frames)", c.Out, sampleRate, len(channels), count)}
	if c.PrintLevels {
		for i, channel := range channels {
			lines = append(lines, fmt.Sprintf("channel mic%d: peak %.2f dBFS, rms %.2f dBFS", channel, dbfs(peaks[i]), dbfs(math.Sqrt(sums[i]/float64(max(count, 1))))))
		}
	}
	return writeReport(w, lines)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // The diagnostic caller selected this local capture path.
	if err != nil {
		return "", fmt.Errorf("open microphone capture for digest: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash microphone capture: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func decodeCapture(dst []int16, raw []byte, layout audio.SampleLayout) (int, error) {
	if layout == audio.LayoutS24_3LE {
		n, err := audio.DecodeS24_3LE(dst, raw)
		if err != nil {
			return 0, fmt.Errorf("decode S24_3LE capture: %w", err)
		}
		return n, nil
	}
	n, err := audio.DecodeS16LE(dst, raw)
	if err != nil {
		return 0, fmt.Errorf("decode S16LE capture: %w", err)
	}
	return n, nil
}

func parseMicChannels(value string, available int) ([]int, error) {
	if value == "all" {
		// The Dot exposes nine ALSA channels, but 7--8 are playback references,
		// not physical microphones.  "all" therefore means all physical mics.
		channels := make([]int, min(7, available))
		for i := range channels {
			channels[i] = i
		}
		return channels, nil
	}
	parts := strings.Split(value, ",")
	channels := make([]int, 0, len(parts))
	seen := make(map[int]bool)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "mic") {
			return nil, fmt.Errorf("invalid microphone channel %q", part)
		}
		channel, err := strconv.Atoi(strings.TrimPrefix(part, "mic"))
		if err != nil || channel < 0 || channel >= min(7, available) || seen[channel] {
			return nil, fmt.Errorf("invalid microphone channel %q", part)
		}
		seen[channel] = true
		channels = append(channels, channel)
	}
	if len(channels) == 0 {
		return nil, errors.New("at least one microphone channel is required")
	}
	return channels, nil
}

func accumulateLevels(samples []int16, channels int, peaks, sums []float64) {
	for i, sample := range samples {
		value := math.Abs(float64(sample))
		channel := i % channels
		peaks[channel] = max(peaks[channel], value)
		sums[channel] += value * value
	}
}

func dbfs(value float64) float64 {
	if value == 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(value/32768)
}

// micScorecard analyzes an already captured simultaneous seven-microphone WAV.
// It never uploads audio.  A matched room-noise recording supplies the optional
// baseline needed to report speech/noise separation; callers can remove input
// only after a successfully published scorecard; retention is explicit opt-in.
func micScorecard(w io.Writer, c micScorecardCommand) error {
	if c.DistanceMM <= 0 {
		return fmt.Errorf("distance must be positive: %d mm", c.DistanceMM)
	}
	format, samples, err := readSevenMicWAV(c.Input)
	if err != nil {
		return err
	}
	frames := len(samples) / format.Channels
	report := micScorecardReport{
		Capture:  micScorecardCapture{SampleRate: format.SampleRate, Channels: format.Channels, Frames: frames, Layout: sampleLayoutName(format.Layout), Health: "unverified"},
		Position: c.Position, DistanceMM: c.DistanceMM, Condition: c.Condition,
		RawAudio: micScorecardRawAudio{Disposition: "deleted"},
		Channels: scoreMicChannels(samples, format.Channels),
	}
	if c.Health != "" {
		health, readErr := readCaptureHealth(c.Health, c.Input, format, frames)
		if readErr != nil {
			return readErr
		}
		report.XRuns, report.DroppedFrames = &health.XRuns, &health.DroppedFrames
		report.Capture.Health = "verified"
	}
	if c.Noise != "" {
		noiseFormat, noiseSamples, readErr := readSevenMicWAV(c.Noise)
		if readErr != nil {
			return readErr
		}
		if noiseFormat.SampleRate != format.SampleRate {
			return fmt.Errorf("noise capture sample rate %d does not match input %d", noiseFormat.SampleRate, format.SampleRate)
		}
		noiseChannels := scoreMicChannels(noiseSamples, noiseFormat.Channels)
		for i := range report.Channels {
			floor := noiseChannels[i].RMSDBFS
			report.Channels[i].NoiseFloorDBFS = floor
			if report.Channels[i].RMSDBFS != nil && floor != nil {
				separation := *report.Channels[i].RMSDBFS - *floor
				report.Channels[i].SpeechNoiseSeparationDB = &separation
			}
		}
	}
	if c.RetainInput {
		report.RawAudio.Disposition = "retained"
	}
	if !c.RetainInput {
		if err := publishDeletedScorecard(c.Input, c.Out, report); err != nil {
			return err
		}
	} else if err := writeJSONFile(c.Out, report); err != nil {
		return fmt.Errorf("write microphone scorecard: %w", err)
	}
	return writeReport(w, []string{fmt.Sprintf("scorecard: %s (%d channels, %d frames, raw audio %s)", c.Out, format.Channels, report.Capture.Frames, report.RawAudio.Disposition)})
}

// micCompare applies each candidate to simultaneous seven-channel captures.
// It is deliberately offline: this produces reproducible evidence but cannot
// substitute for Task 10's live wake, endpointing, and capture-health trials.
func micCompare(w io.Writer, c micCompareCommand) error {
	if c.DistanceMM <= 0 {
		return fmt.Errorf("distance must be positive: %d mm", c.DistanceMM)
	}
	format, speech, err := readSevenMicWAV(c.Input)
	if err != nil {
		return err
	}
	noiseFormat, noise, err := readSevenMicWAV(c.Noise)
	if err != nil {
		return err
	}
	if noiseFormat != format {
		return errors.New("noise capture format does not match input")
	}
	frames := len(speech) / format.Channels
	health, err := readCaptureHealth(c.Health, c.Input, format, frames)
	if err != nil {
		return err
	}
	noiseHealth, err := readCaptureHealth(c.NoiseHealth, c.Noise, noiseFormat, len(noise)/noiseFormat.Channels)
	if err != nil {
		return err
	}
	if health.XRuns != 0 || health.DroppedFrames != 0 || noiseHealth.XRuns != 0 || noiseHealth.DroppedFrames != 0 {
		return errors.New("conditioning comparison requires captures with zero XRuns and dropped frames")
	}
	report := micComparisonReport{Position: c.Position, DistanceMM: c.DistanceMM, Condition: c.Condition, InputHealth: micComparisonHealth{Xruns: health.XRuns, DroppedFrames: health.DroppedFrames}, NoiseHealth: micComparisonHealth{Xruns: noiseHealth.XRuns, DroppedFrames: noiseHealth.DroppedFrames}, InputDisposition: "deleted"}
	for _, candidate := range []audio.ConditioningCandidate{audio.CandidateChannel0, audio.CandidateUnsteeredMix, audio.CandidateDelaySum} {
		conditioner, createErr := audio.NewConditioning(candidate)
		if createErr != nil {
			return fmt.Errorf("create %s conditioner: %w", candidate, createErr)
		}
		conditionSamples(conditioner, speech)
		noiseConditioner, createErr := audio.NewConditioning(candidate)
		if createErr != nil {
			return fmt.Errorf("create %s noise conditioner: %w", candidate, createErr)
		}
		noiseReduced := conditionSamples(noiseConditioner, noise)
		report.Candidates = append(report.Candidates, conditioner.Metrics(noiseReduced))
	}
	if c.RetainInput {
		report.InputDisposition = "retained"
		if err := writeJSONFile(c.Out, report); err != nil {
			return fmt.Errorf("write conditioning comparison: %w", err)
		}
	} else if err := publishDeletedComparison(c.Input, c.Noise, c.Out, report); err != nil {
		return err
	}
	return writeReport(w, []string{fmt.Sprintf("comparison: %s (%d candidates, raw audio %s)", c.Out, len(report.Candidates), report.InputDisposition)})
}

func conditionSamples(conditioner *audio.Conditioning, samples []int16) []int16 {
	frames := len(samples) / audio.PhysicalMicrophones
	reduced := make([]int16, 0, frames)
	for start := 0; start < frames; start += audio.ConditioningCadenceSamples {
		end := min(start+audio.ConditioningCadenceSamples, frames)
		frame := make([]int16, (end-start)*audio.PhysicalMicrophones)
		copy(frame, samples[start*audio.PhysicalMicrophones:end*audio.PhysicalMicrophones])
		_, raw := conditioner.ProcessBlock(deinterleaveScorecard(frame))
		reduced = append(reduced, raw...)
	}
	return reduced
}

func deinterleaveScorecard(samples []int16) [][]int16 {
	frames := len(samples) / audio.PhysicalMicrophones
	mics := make([][]int16, audio.PhysicalMicrophones)
	for mic := range mics {
		mics[mic] = make([]int16, frames)
		for frame := range frames {
			mics[mic][frame] = samples[frame*audio.PhysicalMicrophones+mic]
		}
	}
	return mics
}

func publishDeletedComparison(input, noise, output string, report micComparisonReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conditioning comparison: %w", err)
	}
	pending, err := os.CreateTemp(filepath.Dir(output), ".comparison-*")
	if err != nil {
		return fmt.Errorf("create pending conditioning comparison: %w", err)
	}
	pendingName := pending.Name()
	defer func() { _ = os.Remove(pendingName) }()
	if _, err := pending.Write(append(data, '\n')); err != nil {
		_ = pending.Close()
		return fmt.Errorf("write pending conditioning comparison: %w", err)
	}
	if err := pending.Close(); err != nil {
		return fmt.Errorf("close pending conditioning comparison: %w", err)
	}
	if err := os.Remove(input); err != nil {
		return fmt.Errorf("delete compared microphone capture: %w", err)
	}
	if err := os.Remove(noise); err != nil {
		return fmt.Errorf("delete compared noise capture: %w", err)
	}
	if err := os.Rename(pendingName, output); err != nil {
		return fmt.Errorf("publish conditioning comparison: %w", err)
	}
	return nil
}

func publishDeletedScorecard(input, output string, report micScorecardReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal microphone scorecard: %w", err)
	}
	pending, err := os.CreateTemp(filepath.Dir(output), ".scorecard-*")
	if err != nil {
		return fmt.Errorf("create pending microphone scorecard: %w", err)
	}
	pendingName := pending.Name()
	defer func() { _ = os.Remove(pendingName) }()
	if _, err := pending.Write(append(data, '\n')); err != nil {
		_ = pending.Close()
		return fmt.Errorf("write pending microphone scorecard: %w", err)
	}
	if err := pending.Close(); err != nil {
		return fmt.Errorf("close pending microphone scorecard: %w", err)
	}
	if err := os.Remove(input); err != nil {
		return fmt.Errorf("delete scored microphone capture: %w", err)
	}
	if err := os.Rename(pendingName, output); err != nil {
		return fmt.Errorf("publish microphone scorecard: %w", err)
	}
	return nil
}

func readCaptureHealth(path, capture string, format audio.Format, frames int) (micCaptureHealth, error) {
	data, err := os.ReadFile(path) //nolint:gosec // The operator intentionally supplies the sidecar generated for this capture.
	if err != nil {
		return micCaptureHealth{}, fmt.Errorf("read microphone capture health: %w", err)
	}
	var health micCaptureHealth
	if unmarshalErr := json.Unmarshal(data, &health); unmarshalErr != nil {
		return micCaptureHealth{}, fmt.Errorf("parse microphone capture health: %w", unmarshalErr)
	}
	digest, err := fileSHA256(capture)
	if err != nil {
		return micCaptureHealth{}, err
	}
	if health.SHA256 == "" || health.SHA256 != digest {
		return micCaptureHealth{}, errors.New("microphone capture health does not match input")
	}
	if health.SampleRate != format.SampleRate || health.Frames != frames || len(health.Channels) != audio.PhysicalMicrophones {
		return micCaptureHealth{}, errors.New("microphone capture health does not match capture format")
	}
	for channel := range health.Channels {
		if health.Channels[channel] != channel {
			return micCaptureHealth{}, errors.New("microphone capture health does not preserve physical channel order")
		}
	}
	return health, nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}

func readSevenMicWAV(path string) (audio.Format, []int16, error) {
	file, err := os.Open(path) //nolint:gosec // The operator intentionally selects the local diagnostic capture.
	if err != nil {
		return audio.Format{}, nil, fmt.Errorf("open microphone capture %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	format, samples, err := audio.ReadWAV(file)
	if err != nil {
		return audio.Format{}, nil, fmt.Errorf("read microphone capture %q: %w", path, err)
	}
	if format.Channels != 7 {
		return audio.Format{}, nil, fmt.Errorf("microphone capture %q has %d channels; Task 9 requires exactly seven physical microphones", path, format.Channels)
	}
	if format.SampleRate != audio.CanonicalSampleRate || format.Layout != audio.LayoutS16LE {
		return audio.Format{}, nil, fmt.Errorf("microphone capture %q must be canonical %d Hz S16_LE", path, audio.CanonicalSampleRate)
	}
	if len(samples) == 0 {
		return audio.Format{}, nil, fmt.Errorf("microphone capture %q contains no frames", path)
	}
	return format, samples, nil
}

func scoreMicChannels(samples []int16, channels int) []micScorecardChannel {
	report := make([]micScorecardChannel, channels)
	for channel := range report {
		peak, sum, clipped := 0.0, 0.0, 0
		for frame := range len(samples) / channels {
			value := float64(samples[frame*channels+channel])
			peak = max(peak, math.Abs(value))
			sum += value * value
			if math.Abs(value) >= math.MaxInt16 {
				clipped++
			}
		}
		frames := max(len(samples)/channels, 1)
		report[channel] = micScorecardChannel{Channel: channel, PeakDBFS: dbfsJSON(peak), RMSDBFS: dbfsJSON(math.Sqrt(sum / float64(frames))), ClippingFraction: float64(clipped) / float64(frames), Polarity: "reference"}
	}
	for channel := 1; channel < channels; channel++ {
		delay, correlation := strongestCorrelation(samples, channels, channel)
		report[channel].RelativeDelaySamples = delay
		report[channel].CorrelationWithMic0 = correlation
		switch {
		case correlation == 0:
			report[channel].Polarity = "undetermined"
		case correlation < 0:
			report[channel].Polarity = "inverted"
		default:
			report[channel].Polarity = "normal"
		}
	}
	report[0].CorrelationWithMic0 = 1
	return report
}

func dbfsJSON(value float64) *float64 {
	if value == 0 {
		return nil
	}
	result := dbfs(value)
	return &result
}

func strongestCorrelation(samples []int16, channels, channel int) (int, float64) {
	frames := len(samples) / channels
	margin := min(scorecardMaxDelaySamples, (frames-1)/2)
	if margin == 0 {
		return 0, 0
	}
	bestDelay, bestCorrelation := 0, 0.0
	for delay := -margin; delay <= margin; delay++ {
		var dot, refEnergy, channelEnergy float64
		for frame := margin; frame < frames-margin; frame++ {
			ref := float64(samples[frame*channels])
			other := float64(samples[(frame+delay)*channels+channel])
			dot += ref * other
			refEnergy += ref * ref
			channelEnergy += other * other
		}
		if refEnergy == 0 || channelEnergy == 0 {
			continue
		}
		correlation := dot / math.Sqrt(refEnergy*channelEnergy)
		if math.Abs(correlation) > math.Abs(bestCorrelation) || (math.Abs(correlation) == math.Abs(bestCorrelation) && (absInt(delay) < absInt(bestDelay) || (absInt(delay) == absInt(bestDelay) && delay < bestDelay))) {
			bestDelay, bestCorrelation = delay, correlation
		}
	}
	return bestDelay, bestCorrelation
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func sampleLayoutName(layout audio.SampleLayout) string {
	switch layout {
	case audio.LayoutS16LE:
		return "S16_LE"
	case audio.LayoutS24_3LE:
		return "S24_3LE"
	default:
		return "unknown"
	}
}
