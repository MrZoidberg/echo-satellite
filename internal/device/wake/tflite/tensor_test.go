// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "testing"

func TestTensor_ResizeReusesStorage(t *testing.T) {
	tensor := &Tensor{Type: Float32}
	tensor.resize([]int{2, 3})
	tensor.F32[0] = 7
	first := &tensor.F32[0]
	tensor.resize([]int{3, 2})
	if &tensor.F32[0] != first {
		t.Error("same-sized resize allocated new storage")
	}
	if tensor.Count() != 6 || tensor.Dim(-1) != 2 {
		t.Fatalf("Count/Dim = %d/%d", tensor.Count(), tensor.Dim(-1))
	}
}
