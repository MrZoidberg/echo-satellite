package vec

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLengths = []int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 15, 16, 17, 19, 31, 32, 33, 63, 64, 65, 96, 127, 512, 513, 1000, 1024,
}

func deterministicFloats(n int, offset float64) []float32 {
	out := make([]float32, n)
	for i := range out {
		x := float64(i+1) + offset
		out[i] = float32(math.Sin(x*0.731) + math.Cos(x*0.217)*0.5)
	}
	return out
}

func TestDot_MatchesReferenceImplementation(t *testing.T) {
	t.Parallel()

	for _, n := range testLengths {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			a := deterministicFloats(n, 0)
			b := deterministicFloats(n, 1000)

			var scale float64
			for i := range a {
				scale += math.Abs(float64(a[i]) * float64(b[i]))
			}

			want := dotGo(a, b)
			got := Dot(a, b)
			delta := math.Abs(float64(got-want)) / math.Max(1, scale)
			assert.LessOrEqualf(t, delta, 1e-6, "Dot(%d) got %v, want %v, off by %.3g of what was added", n, got, want, delta)
		})
	}
}

func TestDot_ExactPowerOfTwoInputs(t *testing.T) {
	t.Parallel()

	for _, n := range testLengths {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			a := make([]float32, n)
			b := make([]float32, n)
			for i := range a {
				a[i] = 2
				b[i] = float32(i%4 + 1)
			}

			var want float32
			for i := range a {
				want += a[i] * b[i]
			}

			if got := Dot(a, b); got != want {
				t.Fatalf("Dot(%d) = %v, want %v", n, got, want)
			}
		})
	}
}

func TestDot_EmptySliceReturnsZero(t *testing.T) {
	t.Parallel()

	assert.Zero(t, Dot(nil, nil))
}

func TestDot_ShortSecondSlicePanicsLikeGo(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		Dot(make([]float32, 8), make([]float32, 4))
	})
}

func TestAXPY_MatchesReferenceImplementation(t *testing.T) {
	t.Parallel()

	const gain = 0.375
	for _, n := range testLengths {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			x := deterministicFloats(n, 2000)
			want := deterministicFloats(n, 3000)
			got := make([]float32, n)
			copy(got, want)

			axpyGo(want, gain, x)
			AXPY(got, gain, x)

			require.Len(t, got, len(want))
			for i := range want {
				assert.InDeltaf(t, want[i], got[i], 1e-6, "n=%d index=%d", n, i)
			}
		})
	}
}

func TestAXPY_ExactPowerOfTwoInputs(t *testing.T) {
	t.Parallel()

	for _, n := range testLengths {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			x := make([]float32, n)
			dst := make([]float32, n)
			want := make([]float32, n)
			for i := range x {
				x[i] = float32(i%4 + 1)
				dst[i] = 8
				want[i] = 8 + 2*float32(i%4+1)
			}

			AXPY(dst, 2, x)
			assert.Equal(t, want, dst)
		})
	}
}

func TestAXPY_StaysInBounds(t *testing.T) {
	t.Parallel()

	for _, n := range testLengths {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()
			if n == 0 {
				return
			}

			buf := make([]float32, n+16)
			for i := range buf {
				buf[i] = 99
			}

			x := make([]float32, n)
			for i := range x {
				x[i] = 1
			}

			AXPY(buf[:n], 1, x)
			for i := n; i < len(buf); i++ {
				if buf[i] != 99 {
					t.Fatalf("n=%d wrote past end at index=%d: got %v", n, i, buf[i])
				}
			}
		})
	}
}

func TestAXPY_EmptySliceReturns(t *testing.T) {
	t.Parallel()

	AXPY(nil, 1, nil)
}

func TestAXPY_ShortDestinationPanicsLikeGo(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		AXPY(make([]float32, 4), 1, make([]float32, 8))
	})
}

var sink float32

func BenchmarkDot(b *testing.B) {
	for _, n := range []int{32, 64, 512, 1024} {
		x := make([]float32, n)
		y := make([]float32, n)
		for i := range x {
			x[i] = float32(i) * 0.001
			y[i] = float32(n-i) * 0.001
		}

		b.Run("impl/"+strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(n * 4))
			for b.Loop() {
				sink = Dot(x, y)
			}
		})
		b.Run("go/"+strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(n * 4))
			for b.Loop() {
				sink = dotGo(x, y)
			}
		})
	}
}

func BenchmarkAXPY(b *testing.B) {
	for _, n := range []int{32, 64, 512, 1024} {
		x := make([]float32, n)
		dst := make([]float32, n)
		for i := range x {
			x[i] = float32(i) * 0.001
			dst[i] = float32(n-i) * 0.001
		}

		b.Run("impl/"+strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(n * 4))
			for b.Loop() {
				AXPY(dst, 1e-9, x)
			}
		})
		b.Run("go/"+strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(n * 4))
			for b.Loop() {
				axpyGo(dst, 1e-9, x)
			}
		})
	}
}
