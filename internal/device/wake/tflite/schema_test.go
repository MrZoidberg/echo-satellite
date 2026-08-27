// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "testing"

func TestParse_SyntheticSchema(t *testing.T) {
	model, err := Parse(syntheticFixtures()["conv2d.tflite"])
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Subgraphs) != 1 || len(model.Subgraphs[0].Tensors) != 4 {
		t.Fatalf("parsed model = %+v", model)
	}
	if got := model.Subgraphs[0].Ops[0].Op; got != OpConv2D {
		t.Fatalf("operator = %s, want CONV_2D", got)
	}
}

func TestReshape_AllowsBuiltinShapeWithoutSecondInput(t *testing.T) {
	op := &OpDesc{Op: OpReshape, Inputs: []int{0}, Outputs: []int{1}, dims: []int{2, 1}}
	if err := validateOperator(op, 0, 0, 2); err != nil {
		t.Fatalf("one-input RESHAPE rejected: %v", err)
	}
	output := out()
	if err := kernels[OpReshape](op, []*Tensor{tf([]int{1, 2}, 3, 4)}, []*Tensor{output}); err != nil {
		t.Fatal(err)
	}
	shape(t, "reshape", output.Shape, []int{2, 1})
	f32(t, "reshape", output.F32, []float32{3, 4})
}

func TestStridedSlice_RequiresFourInputs(t *testing.T) {
	op := &OpDesc{Op: OpStridedSlice, Inputs: []int{0, 1, 2}, Outputs: []int{3}}
	if err := validateOperator(op, 0, 0, 4); err == nil {
		t.Fatal("three-input STRIDED_SLICE accepted")
	}
}
