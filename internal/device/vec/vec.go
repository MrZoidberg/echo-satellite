// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

// Package vec is the small set of float32 slice loops that the wake stack and
// resampler spend time in. Arm64 builds use hand-written NEON assembly where it
// pays, while every other build uses portable Go.
//
// Go assembly does not require cgo, so these kernels preserve the
// CGO_ENABLED=0 device build. The noasm tag forces the portable path so the
// same hardware can benchmark both implementations.
//
// The implementations do not agree bit for bit and are not meant to: they group
// additions differently, and float32 addition is not associative.
package vec

// dotGo is the portable inner product.
//
// Four running sums keep independent accumulator chains available instead of
// serializing every addition behind the previous one. The tail handles whatever
// is left when the length is not a multiple of four.
func dotGo(a, b []float32) float32 {
	b = b[:len(a)]

	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}

	s := s0 + s1 + s2 + s3
	for ; i < len(a); i++ {
		s += a[i] * b[i]
	}

	return s
}

func axpyGo(dst []float32, gain float32, x []float32) {
	dst = dst[:len(x)]
	for i, v := range x {
		dst[i] += gain * v
	}
}
