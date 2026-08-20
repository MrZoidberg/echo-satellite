// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

// Tensor is an NHWC row-major buffer plus its current shape. Integer tensors
// decode into I16 because the supported graphs use them only for small shapes,
// axes, permutations, and Boolean values.
type Tensor struct {
	Name  string
	Type  TensorType
	Shape []int
	F32   []float32
	I16   []int16
	Const bool
}

func count(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

// Count is the number of elements the current shape describes.
func (t *Tensor) Count() int { return count(t.Shape) }

// Dim is the size of an axis, counting from the end for negative i.
func (t *Tensor) Dim(i int) int {
	if i < 0 {
		i += len(t.Shape)
	}
	if i < 0 || i >= len(t.Shape) {
		return 1
	}
	return t.Shape[i]
}

func (t *Tensor) resize(shape []int) {
	n := count(shape)
	t.Shape = append(t.Shape[:0], shape...)
	switch t.Type {
	case Float32:
		if cap(t.F32) < n {
			t.F32 = make([]float32, n)
		}
		t.F32 = t.F32[:n]
	default:
		if cap(t.I16) < n {
			t.I16 = make([]int16, n)
		}
		t.I16 = t.I16[:n]
	}
}
