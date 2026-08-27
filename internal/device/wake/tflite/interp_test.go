// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "testing"

func TestInterpreter_InputShapeIsDefensiveCopy(t *testing.T) {
	model, err := Parse(syntheticFixtures()["add.tflite"])
	if err != nil {
		t.Fatal(err)
	}
	interpreter, err := New(model)
	if err != nil {
		t.Fatal(err)
	}
	shape := interpreter.InputShape(0)
	shape[0] = 99
	if got := interpreter.InputShape(0)[0]; got != 2 {
		t.Fatalf("input shape mutated through returned slice: %d", got)
	}
}
