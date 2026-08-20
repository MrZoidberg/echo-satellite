// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "testing"

func TestFlatBuffers_ReadsSyntheticRoot(t *testing.T) {
	raw := syntheticFixtures()["logistic.tflite"]
	r := root(buf(raw))
	if got := r.vec(modelOperatorCodes).len(); got != 1 {
		t.Fatalf("operator-code count = %d, want 1", got)
	}
}
