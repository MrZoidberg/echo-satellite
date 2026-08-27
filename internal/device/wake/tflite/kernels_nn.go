// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"fmt"
	"math"

	"github.com/MrZoidberg/echo-satellite/internal/device/vec"
)

func conv2D(o *OpDesc, in, out []*Tensor) error {
	p := o.conv
	x, w, y := in[0], in[1], out[0]
	if len(x.Shape) != 4 || len(w.Shape) != 4 {
		return fmt.Errorf("expected 4-D input and filter, got %v and %v", x.Shape, w.Shape)
	}

	batch, ih, iw, ic := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oc, kh, kw := w.Shape[0], w.Shape[1], w.Shape[2]
	if w.Shape[3] != ic {
		return fmt.Errorf("filter has %d input channels, input has %d", w.Shape[3], ic)
	}

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	oh := outputSize(ih, kh, p.strideH, p.dilationH, p.samePad)
	ow := outputSize(iw, kw, p.strideW, p.dilationW, p.samePad)
	y.resize([]int{batch, oh, ow, oc})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, p.dilationH, ih, kh, oh)
		padW = padFor(p.strideW, p.dilationW, iw, kw, ow)
	}

	xd, wd, yd := x.F32, w.F32, y.F32
	yi := 0
	for b := range batch {
		for oy := range oh {
			for ox := range ow {
				for f := range oc {
					acc := convDot(xd, wd, bias, b, oy, ox, f, ih, iw, ic, kh, kw, padH, padW, p)
					yd[yi] = activate(acc, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func convDot(xd, wd, bias []float32, b, oy, ox, f, ih, iw, ic, kh, kw, padH, padW int, p convParams) float32 {
	var acc float32
	if bias != nil {
		acc = bias[f]
	}
	for ky := range kh {
		iy := oy*p.strideH - padH + ky*p.dilationH
		if iy < 0 || iy >= ih {
			continue
		}

		// With one channel the contiguous run is along width, which is where
		// the mel model's 512-tap filters live.
		if ic == 1 && p.dilationW == 1 {
			x0 := ox*p.strideW - padW
			lo, hi := max(0, -x0), min(kw, iw-x0)
			if hi > lo {
				xo := (b*ih+iy)*iw + x0 + lo
				wo := (f*kh+ky)*kw + lo
				acc += vec.Dot(wd[wo:wo+hi-lo], xd[xo:xo+hi-lo])
			}
			continue
		}

		for kx := range kw {
			ix := ox*p.strideW - padW + kx*p.dilationW
			if ix < 0 || ix >= iw {
				continue
			}
			xo := ((b*ih+iy)*iw + ix) * ic
			wo := ((f*kh+ky)*kw + kx) * ic
			acc += vec.Dot(xd[xo:xo+ic], wd[wo:wo+ic])
		}
	}
	return acc
}

func maxPool2D(o *OpDesc, in, out []*Tensor) error {
	p := o.pool
	x, y := in[0], out[0]
	if len(x.Shape) != 4 {
		return fmt.Errorf("expected a 4-D input, got %v", x.Shape)
	}

	batch, ih, iw, c := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oh := outputSize(ih, p.filterH, p.strideH, 1, p.samePad)
	ow := outputSize(iw, p.filterW, p.strideW, 1, p.samePad)
	y.resize([]int{batch, oh, ow, c})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, 1, ih, p.filterH, oh)
		padW = padFor(p.strideW, 1, iw, p.filterW, ow)
	}

	xd, yd := x.F32, y.F32
	yi := 0
	for b := range batch {
		for oy := range oh {
			y0 := oy*p.strideH - padH
			for ox := range ow {
				x0 := ox*p.strideW - padW
				for ch := range c {
					best := float32(math.Inf(-1))
					for ky := range p.filterH {
						iy := y0 + ky
						if iy < 0 || iy >= ih {
							continue
						}
						for kx := range p.filterW {
							ix := x0 + kx
							if ix < 0 || ix >= iw {
								continue
							}
							if v := xd[((b*ih+iy)*iw+ix)*c+ch]; v > best {
								best = v
							}
						}
					}
					yd[yi] = activate(best, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func fullyConnected(o *OpDesc, in, out []*Tensor) error {
	x, w, y := in[0], in[1], out[0]
	if len(w.Shape) < 2 {
		return fmt.Errorf("expected 2-D weights, got %v", w.Shape)
	}

	inC := w.Shape[len(w.Shape)-1]
	oc := count(w.Shape[:len(w.Shape)-1])
	total := x.Count()
	if inC == 0 || total%inC != 0 {
		return fmt.Errorf("input of %d values does not divide into rows of %d", total, inC)
	}
	batch := total / inC

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	y.resize([]int{batch, oc})
	act := o.act
	for b := range batch {
		row := x.F32[b*inC : (b+1)*inC]
		for f := range oc {
			acc := vec.Dot(row, w.F32[f*inC:(f+1)*inC])
			if bias != nil {
				acc += bias[f]
			}
			y.F32[b*oc+f] = activate(acc, act)
		}
	}
	return nil
}

func batchMatMul(o *OpDesc, in, out []*Tensor) error {
	a, b, y := in[0], in[1], out[0]
	adjX, adjY := o.adjX, o.adjY
	if len(a.Shape) < 2 || len(b.Shape) < 2 {
		return fmt.Errorf("expected at least 2-D inputs, got %v and %v", a.Shape, b.Shape)
	}

	m, k := a.Dim(-2), a.Dim(-1)
	if adjX {
		m, k = k, m
	}
	kb, n := b.Dim(-2), b.Dim(-1)
	if adjY {
		kb, n = n, kb
	}
	if k != kb {
		return fmt.Errorf("inner dimensions %d and %d do not match", k, kb)
	}

	batchShape, err := broadcastShape(a.Shape[:len(a.Shape)-2], b.Shape[:len(b.Shape)-2])
	if err != nil {
		return err
	}
	y.resize(append(append([]int{}, batchShape...), m, n))

	batches := count(batchShape)
	as := strides(a.Shape[:len(a.Shape)-2], batchShape)
	bs := strides(b.Shape[:len(b.Shape)-2], batchShape)
	idx := make([]int, len(batchShape))

	ao, bo := 0, 0
	for batch := range batches {
		am := a.F32[ao*m*k:]
		bm := b.F32[bo*kb*n:]
		ym := y.F32[batch*m*n:]

		for i := range m {
			for j := range n {
				var acc float32
				for p := range k {
					var av, bv float32
					if adjX {
						av = am[p*m+i]
					} else {
						av = am[i*k+p]
					}
					if adjY {
						bv = bm[j*kb+p]
					} else {
						bv = bm[p*n+j]
					}
					acc += av * bv
				}
				ym[i*n+j] = acc
			}
		}
		ao, bo = step(idx, batchShape, as, bs, ao, bo)
	}
	return nil
}

// step advances an odometer over shape and returns the two source offsets that go with the new
// position. Strides of zero are how a broadcast axis stays put.
func step(idx, shape, as, bs []int, ao, bo int) (int, int) {
	for reverse := range shape {
		d := len(shape) - 1 - reverse
		idx[d]++
		ao += as[d]
		bo += bs[d]
		if idx[d] < shape[d] {
			return ao, bo
		}
		idx[d] = 0
		ao -= as[d] * shape[d]
		bo -= bs[d] * shape[d]
	}
	return ao, bo
}
