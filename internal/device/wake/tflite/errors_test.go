// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"errors"
	"testing"
)

func TestInterpreter_RejectsTruncatedFlatBuffer(t *testing.T) {
	_, err := Parse([]byte{1, 2, 3})
	if !errors.Is(err, ErrBadModel) {
		t.Fatalf("Parse error = %v, want ErrBadModel", err)
	}
}

func TestInvoke_WrapsShapeMismatch(t *testing.T) {
	model, err := Parse(syntheticFixtures()["add.tflite"])
	if err != nil {
		t.Fatal(err)
	}
	interpreter, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	interpreter.Input(0).Shape = []int{3}
	interpreter.Input(0).F32 = []float32{1, 2, 3}
	if err := interpreter.Invoke(); !errors.Is(err, ErrShapeMismatch) {
		t.Fatalf("Invoke error = %v, want ErrShapeMismatch", err)
	}
}

func TestInvoke_RecoversMalformedModelPanic(t *testing.T) {
	interpreter := &Interpreter{graph: &Subgraph{Ops: []*OpDesc{{Op: OpAdd, Inputs: []int{99}, Outputs: []int{0}}}}, tensors: []*Tensor{{}}}
	if err := interpreter.Invoke(); !errors.Is(err, ErrBadModel) {
		t.Fatalf("Invoke error = %v, want ErrBadModel", err)
	}
}

func TestNew_RejectsWrongConstantBufferSize(t *testing.T) {
	model, err := Parse(syntheticFixtures()["add.tflite"])
	if err != nil {
		t.Fatal(err)
	}
	model.buffers[1].n--
	_, err = New(model)
	if !errors.Is(err, ErrBadModel) {
		t.Fatalf("New error = %v, want ErrBadModel", err)
	}
}
