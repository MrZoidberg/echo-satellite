//go:build !arm64 || noasm

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package vec

// Built for anything but arm64, and for arm64 with the noasm tag, which exists
// so both implementations can be benchmarked on the hardware that matters.

// Dot is the sum of a[i]*b[i].
func Dot(a, b []float32) float32 {
	return dotGo(a, b)
}

// AXPY adds gain*x[i] to dst[i].
func AXPY(dst []float32, gain float32, x []float32) {
	axpyGo(dst, gain, x)
}
