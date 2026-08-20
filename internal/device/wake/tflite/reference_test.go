// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// referenceSignal is the deterministic input the golden fixture was produced from: two tones and
// a slow amplitude sweep, at the int16 magnitudes openWakeWord feeds the mel model.
func referenceSignal(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		t := float64(i) / 16000
		env := 0.4 + 0.6*math.Sin(2*math.Pi*3*t)
		v := env * (6000*math.Sin(2*math.Pi*440*t) + 2500*math.Sin(2*math.Pi*1750*t))
		out[i] = float32(int16(v))
	}
	return out
}

func readGolden(t *testing.T, path string) []float32 {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // caller supplies only repository-owned reference-vector paths.
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()

	var out []float32
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, err := strconv.ParseFloat(line, 32)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, float32(v))
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestMelMatchesReferenceRuntime compares our interpreter against TensorFlow Lite's own C runtime
// on the same model file and the same input. The fixture was produced by the reference runtime, so
// any disagreement here is a bug in this package rather than a question of calibration.
func TestInterpreter_MelOutputMatchesReferenceVectors(t *testing.T) {
	want := readGolden(t, "../../../../testdata/wake/reference/mel_reference.txt")

	raw, err := os.ReadFile(filepath.Join(requireModelDir(t), "melspectrogram.tflite"))
	if err != nil {
		t.Skip(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	in, err := New(m)
	if err != nil {
		t.Fatal(err)
	}

	const samples = 1760
	in.ResizeInput(0, []int{1, samples})
	copy(in.Input(0).F32, referenceSignal(samples))
	if err := in.Invoke(); err != nil {
		t.Fatal(err)
	}

	got := in.Output(0).F32
	if len(got) != len(want) {
		t.Fatalf("got %d values, reference has %d", len(got), len(want))
	}

	var worst float64
	var at int
	firstMismatch := -1
	for i := range want {
		if d := math.Abs(float64(got[i] - want[i])); d > worst {
			worst, at = d, i
		}
		if firstMismatch < 0 && !closeReference(got[i], want[i]) {
			firstMismatch = i
		}
	}
	t.Logf("largest difference %.6g at index %d (ours %.6f, reference %.6f)", worst, at, got[at], want[at])
	if firstMismatch >= 0 {
		i := firstMismatch
		t.Fatalf("first mismatch at %d: got %.6f, want %.6f; maximum deviation %.6g at %d", i, got[i], want[i], worst, at)
	}

	// The reference runs the same graph in float32, so only accumulation order should differ.
}

// referenceMelFrames is the deterministic stand-in for a window of transformed mel frames that the
// embedding fixture was produced from.
func referenceMelFrames(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		f := float64(i)
		out[i] = float32(6 + 4*math.Sin(f*0.037) + 3*math.Cos(f*0.011) - 2*math.Sin(f*0.9))
	}
	return out
}

func TestInterpreter_EmbeddingOutputMatchesReferenceVectors(t *testing.T) {
	want := readGolden(t, "../../../../testdata/wake/reference/embedding_reference.txt")

	raw, err := os.ReadFile(filepath.Join(requireModelDir(t), "embedding_model.tflite"))
	if err != nil {
		t.Skip(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	in, err := New(m)
	if err != nil {
		t.Fatal(err)
	}

	in.ResizeInput(0, []int{1, 76, 32, 1})
	copy(in.Input(0).F32, referenceMelFrames(76*32))
	if err := in.Invoke(); err != nil {
		t.Fatal(err)
	}

	got := in.Output(0).F32
	if len(got) != len(want) {
		t.Fatalf("got %d values, reference has %d", len(got), len(want))
	}

	var worst float64
	var at int
	firstMismatch := -1
	for i := range want {
		if d := math.Abs(float64(got[i] - want[i])); d > worst {
			worst, at = d, i
		}
		if firstMismatch < 0 && !closeReference(got[i], want[i]) {
			firstMismatch = i
		}
	}
	t.Logf("largest difference %.6g at index %d (ours %.6f, reference %.6f)", worst, at, got[at], want[at])
	if firstMismatch >= 0 {
		i := firstMismatch
		t.Fatalf("first mismatch at %d: got %.6f, want %.6f; maximum deviation %.6g at %d", i, got[i], want[i], worst, at)
	}
}

func requireModelDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("ECHO_WAKE_MODEL_DIR")
	if dir == "" {
		t.Skip("ECHO_WAKE_MODEL_DIR is not set")
	}
	return dir
}

func closeReference(got, want float32) bool {
	delta := math.Abs(float64(got - want))
	return delta <= 1e-3 || delta <= 1e-2*math.Abs(float64(want))
}

func TestModel_ReferenceModelsUseSupportedOpcodes(t *testing.T) {
	for _, name := range []string{"melspectrogram.tflite", "embedding_model.tflite", "okay_nabu.tflite"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(requireModelDir(t), name)) //nolint:gosec // explicit test-only model directory.
			if err != nil {
				t.Fatal(err)
			}
			model, err := Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("opcodes: %v", model.Opcodes())
			if unsupported := model.UnsupportedOpcodes(); len(unsupported) != 0 {
				t.Fatalf("unsupported opcodes: %v", unsupported)
			}
		})
	}
}
