// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"errors"
	"testing"
)

func TestInterpreter_RejectsUnknownOpcodeWithErrUnsupportedOp(t *testing.T) {
	spec := syntheticModel{
		tensors: []syntheticTensor{{[]int{1}, Float32, 0}, {[]int{1}, Float32, 0}},
		inputs:  []int{0}, outputs: []int{1}, op: syntheticOp{Op(999), []int{0}, []int{1}, activationOptions},
	}
	model, err := Parse(buildSynthetic(spec))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(model)
	if !errors.Is(err, ErrUnsupportedOp) {
		t.Fatalf("New error = %v, want ErrUnsupportedOp", err)
	}
	if got := err.Error(); got != "tflite: unsupported operator: OP_999 at operator 0" {
		t.Fatalf("error = %q; opcode must be named", got)
	}
}

func TestModel_ReportsUnsupportedOpcodes(t *testing.T) {
	model := &Model{Subgraphs: []*Subgraph{{Ops: []*OpDesc{{Op: OpAdd}, {Op: Op(999)}, {Op: OpAdd}, {Op: Op(998)}, {Op: OpCustom, Custom: "AcmeOne"}, {Op: OpCustom, Custom: "AcmeTwo"}}}}}
	if got := model.Opcodes(); !equalStrings(got, []string{"ADD", "OP_999", "OP_998", "CUSTOM:AcmeOne", "CUSTOM:AcmeTwo"}) {
		t.Fatalf("Opcodes = %v", got)
	}
	if got := model.UnsupportedOpcodes(); !equalStrings(got, []string{"OP_999", "OP_998", "CUSTOM:AcmeOne", "CUSTOM:AcmeTwo"}) {
		t.Fatalf("UnsupportedOpcodes = %v", got)
	}
}

func TestSupportedOpcodes_MatchesKernelRegistry(t *testing.T) {
	want := make(map[string]bool, len(kernels))
	for opcode := range kernels {
		want[opcode.String()] = true
	}
	got := SupportedOpcodes()
	if len(got) != len(want) {
		t.Fatalf("SupportedOpcodes has %d entries, registry has %d", len(got), len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("SupportedOpcodes contains unregistered %s", name)
		}
	}
	got[0] = "MUTATED"
	if SupportedOpcodes()[0] == "MUTATED" {
		t.Error("SupportedOpcodes returned shared storage")
	}
}

func equalStrings(a, b []string) bool {
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
