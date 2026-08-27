// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "testing"

func TestFullyConnected_RejectsMismatchedRows(t *testing.T) {
	err := fullyConnected(&OpDesc{}, []*Tensor{tf([]int{3}, 1, 2, 3), tf([]int{1, 2}, 1, 1)}, []*Tensor{out()})
	if err == nil {
		t.Fatal("fullyConnected accepted a partial input row")
	}
}
