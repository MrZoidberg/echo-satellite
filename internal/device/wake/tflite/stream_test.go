// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func embeddingModel(t *testing.T) *Model {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(requireModelDir(t), "embedding_model.tflite"))
	if err != nil {
		t.Skip(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The streaming path must agree with the windowed model exactly: same filters, same accumulation
// order, only fewer rows recomputed. Anything else means the graph is not what NewStream checked
// it for.
func TestStreamMatchesWindowedModel(t *testing.T) {
	m := embeddingModel(t)

	const (
		window = 76
		bins   = 32
		stride = 8
		steps  = 6
	)

	// A deterministic ramp with enough variation that a wrong row alignment cannot pass.
	total := window + steps*stride
	mels := make([]float32, total*bins)
	for i := range mels {
		mels[i] = float32(math.Sin(float64(i)*0.07)) + float32(i%bins)/64
	}

	windowed, err := New(m)
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewStream(m, []int{1, window, bins, 1})
	if err != nil {
		t.Fatal(err)
	}
	if s.Warmup() != window {
		t.Errorf("warmup = %d, want %d", s.Warmup(), window)
	}

	// Prime with the first window, then compare each subsequent step.
	got, err := s.Write(mels[:window*bins])
	if err != nil {
		t.Fatal(err)
	}
	compareToWindow(t, windowed, mels, 0, window, bins, got)

	for step := 1; step <= steps; step++ {
		from := window + (step-1)*stride
		got, err := s.Write(mels[from*bins : (from+stride)*bins])
		if err != nil {
			t.Fatal(err)
		}
		compareToWindow(t, windowed, mels, step*stride, window, bins, got)
	}
}

func compareToWindow(t *testing.T, in *Interpreter, mels []float32, offset, window, bins int, got []float32) {
	t.Helper()

	in.ResizeInput(0, []int{1, window, bins, 1})
	copy(in.Input(0).F32, mels[offset*bins:(offset+window)*bins])
	if err := in.Invoke(); err != nil {
		t.Fatal(err)
	}
	want := in.Output(0).F32

	if len(got) != len(want) {
		t.Fatalf("offset %d: got %d values, want %d", offset, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("offset %d: value %d is %v streaming, %v windowed", offset, i, got[i], want[i])
		}
	}
}

func TestStream_RejectsOperatorsNotProvenRowLocal(t *testing.T) {
	model := &Model{Subgraphs: []*Subgraph{{
		Inputs: []int{0}, Outputs: []int{2},
		Tensors: []*TensorDesc{{Shape: []int{1, 2, 2, 1}}, {Shape: []int{1}}, {Shape: []int{1, 1, 2, 1}}},
		Ops:     []*OpDesc{{Op: OpMean, Inputs: []int{0, 1}, Outputs: []int{2}}},
	}}}
	if _, err := NewStream(model, []int{1, 2, 2, 1}); err == nil {
		t.Fatal("NewStream accepted a reduction across time")
	}
}

func TestStream_ResetDropsBufferedRows(t *testing.T) {
	bufferedStage := stage{need: 2, consume: 1, rowSize: 1, width: 1, channels: 1, pending: []float32{1}}
	stream := &Stream{stages: []stage{bufferedStage}}
	stream.Reset()
	if len(stream.stages[0].pending) != 0 {
		t.Fatal("Reset retained buffered rows")
	}
}

func TestStream_RejectsDynamicParametersAndWrongFinalOutput(t *testing.T) {
	t.Run("dynamic convolution weights", func(t *testing.T) {
		model := &Model{Subgraphs: []*Subgraph{{Inputs: []int{0, 1}, Outputs: []int{2}, Tensors: []*TensorDesc{
			{Type: Float32, Shape: []int{1, 2, 2, 1}}, {Type: Float32, Shape: []int{1, 1, 1, 1}}, {Type: Float32, Shape: []int{1, 2, 2, 1}},
		}, Ops: []*OpDesc{{Op: OpConv2D, Inputs: []int{0, 1}, Outputs: []int{2}, conv: convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1}}}}}}
		if _, err := NewStream(model, []int{1, 2, 2, 1}); err == nil {
			t.Fatal("NewStream accepted dynamic convolution weights")
		}
	})
	t.Run("chain does not reach graph output", func(t *testing.T) {
		model := &Model{Subgraphs: []*Subgraph{{Inputs: []int{0}, Outputs: []int{2}, Tensors: []*TensorDesc{
			{Type: Float32, Shape: []int{1, 2, 2, 1}}, {Type: Float32, Shape: []int{1, 2, 2, 1}}, {Type: Float32, Shape: []int{1, 2, 2, 1}},
		}, Ops: []*OpDesc{{Op: OpRelu, Inputs: []int{0}, Outputs: []int{1}}}}}}
		if _, err := NewStream(model, []int{1, 2, 2, 1}); err == nil {
			t.Fatal("NewStream accepted a chain ending before the graph output")
		}
	})
}

func BenchmarkStreamStep(b *testing.B) {
	dir := os.Getenv("ECHO_WAKE_MODEL_DIR")
	if dir == "" {
		b.Skip("ECHO_WAKE_MODEL_DIR is not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "embedding_model.tflite")) //nolint:gosec // explicit test-only model directory.
	if err != nil {
		b.Skip(err)
	}
	m, err := Parse(raw)
	if err != nil {
		b.Fatal(err)
	}
	s, err := NewStream(m, []int{1, 76, 32, 1})
	if err != nil {
		b.Fatal(err)
	}

	prime := make([]float32, 76*32)
	for i := range prime {
		prime[i] = float32(i%32) / 32
	}
	if _, err := s.Write(prime); err != nil {
		b.Fatal(err)
	}
	step := make([]float32, 8*32)

	for b.Loop() {
		if _, err := s.Write(step); err != nil {
			b.Fatal(err)
		}
	}
}
