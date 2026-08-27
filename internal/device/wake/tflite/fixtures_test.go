// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixtures_MatchGenerator(t *testing.T) {
	for name, want := range syntheticFixtures() {
		got, err := os.ReadFile(filepath.Join(syntheticDir(), name)) //nolint:gosec // test-controlled fixture path
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; regenerate with -update-fixtures", name)
		}
	}
}

func TestInterpreter_SyntheticModels(t *testing.T) {
	tests := []struct {
		name  string
		input []float32
		shape []int
		want  []float32
	}{
		{"fully_connected.tflite", []float32{3, 1}, []int{1, 2}, []float32{5.5}},
		{"reshape.tflite", []float32{3, 4}, []int{1, 2}, []float32{3, 4}},
		{"logistic.tflite", []float32{0, 1}, []int{2}, []float32{0.5, 0.7310586}},
		{"conv2d.tflite", []float32{1, 2, 3, 4}, []int{1, 2, 2, 1}, []float32{2.5, 4.5, 6.5, 8.5}},
		{"add.tflite", []float32{1, 2}, []int{2}, []float32{11, 22}},
		{"mul.tflite", []float32{2, 3}, []int{2}, []float32{6, 12}},
		{"sub.tflite", []float32{5, 8}, []int{2}, []float32{2, 4}},
		{"minimum.tflite", []float32{2, 5}, []int{2}, []float32{2, 4}},
		{"maximum.tflite", []float32{2, 5}, []int{2}, []float32{3, 5}},
		{"log.tflite", []float32{1, 2.7182817}, []int{2}, []float32{0, 1}},
		{"leaky_relu.tflite", []float32{-4, 2}, []int{2}, []float32{-1, 2}},
		{"expand_dims.tflite", []float32{3, 4}, []int{2}, []float32{3, 4}},
		{"transpose.tflite", []float32{1, 2, 3, 4}, []int{2, 2}, []float32{1, 3, 2, 4}},
		{"squeeze.tflite", []float32{3, 4}, []int{1, 2, 1}, []float32{3, 4}},
		{"reduce_max.tflite", []float32{1, 5, 7, 2}, []int{2, 2}, []float32{5, 7}},
		{"pad.tflite", []float32{3, 4}, []int{1, 2}, []float32{0, 0, 0, 3, 4, 0}},
		{"max_pool2d.tflite", []float32{1, 5, 3, 2}, []int{1, 2, 2, 1}, []float32{5}},
		{"batch_matmul.tflite", []float32{4, 5}, []int{1, 2}, []float32{23}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(syntheticDir(), test.name))
			if err != nil {
				t.Fatal(err)
			}
			model, err := Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			interpreter, err := New(model)
			if err != nil {
				t.Fatal(err)
			}
			if got := interpreter.InputShape(0); !equalInts(got, test.shape) {
				t.Fatalf("input shape %v, want %v", got, test.shape)
			}
			copy(interpreter.Input(0).F32, test.input)
			if err := interpreter.Invoke(); err != nil {
				t.Fatal(err)
			}
			f32(t, test.name, interpreter.Output(0).F32, test.want)
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
