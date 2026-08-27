// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"errors"
	"fmt"
	"math"

	"github.com/MrZoidberg/echo-satellite/internal/device/vec"
)

func broadcastShape(a, b []int) ([]int, error) {
	rank := max(len(a), len(b))
	out := make([]int, rank)
	for i := range rank {
		da, db := 1, 1
		if d := len(a) - rank + i; d >= 0 {
			da = a[d]
		}
		if d := len(b) - rank + i; d >= 0 {
			db = b[d]
		}
		switch {
		case da == db, db == 1:
			out[i] = da
		case da == 1:
			out[i] = db
		default:
			return nil, fmt.Errorf("shapes %v and %v do not broadcast", a, b)
		}
	}
	return out, nil
}

// strides maps src onto the axes of out, giving the offset to add when walking each output axis.
// A broadcast axis gets a stride of zero so it reads the same element repeatedly.
func strides(src, out []int) []int {
	s := make([]int, len(out))
	stride := 1
	for i := range src {
		d := len(src) - 1 - i
		o := len(out) - 1 - i
		if o < 0 {
			break
		}
		if src[d] != 1 {
			s[o] = stride
		}
		stride *= src[d]
	}
	return s
}

// step1 advances an odometer over shape and returns the source offset for the new position.
func step1(idx, shape, strides []int, off int) int {
	for reverse := range shape {
		d := len(shape) - 1 - reverse
		idx[d]++
		off += strides[d]
		if idx[d] < shape[d] {
			return off
		}
		idx[d] = 0
		off -= strides[d] * shape[d]
	}
	return off
}

func contiguous(shape []int) []int {
	s := make([]int, len(shape))
	stride := 1
	for reverse := range shape {
		d := len(shape) - 1 - reverse
		s[d] = stride
		stride *= shape[d]
	}
	return s
}

func unary(f func(float32) float32) kernel {
	return func(o *OpDesc, in, out []*Tensor) error {
		x, y := in[0], out[0]
		y.resize(x.Shape)
		for i, v := range x.F32 {
			y.F32[i] = f(v)
		}
		return nil
	}
}

func elementwise(f func(a, b float32) float32) kernel {
	return func(o *OpDesc, in, out []*Tensor) error {
		a, b, y := in[0], in[1], out[0]
		shape, err := broadcastShape(a.Shape, b.Shape)
		if err != nil {
			return err
		}
		y.resize(shape)
		act := o.act
		n := y.Count()

		if a.Count() == n && b.Count() == n {
			for i := range n {
				y.F32[i] = activate(f(a.F32[i], b.F32[i]), act)
			}
			return nil
		}

		as, bs := strides(a.Shape, shape), strides(b.Shape, shape)
		idx := make([]int, len(shape))
		ao, bo := 0, 0
		for i := range n {
			y.F32[i] = activate(f(a.F32[ao], b.F32[bo]), act)
			ao, bo = step(idx, shape, as, bs, ao, bo)
		}
		return nil
	}
}

func add(o *OpDesc, in, out []*Tensor) error {
	a, b, y := in[0], in[1], out[0]
	if o.act == ActNone && equalShape(a.Shape, b.Shape) {
		y.resize(a.Shape)
		copy(y.F32, b.F32)
		vec.AXPY(y.F32, 1, a.F32)
		return nil
	}
	return elementwise(func(a, b float32) float32 { return a + b })(o, in, out)
}

func equalShape(a, b []int) bool {
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

func leakyRelu(o *OpDesc, in, out []*Tensor) error {
	alpha := o.alpha
	x, y := in[0], out[0]
	y.resize(x.Shape)
	for i, v := range x.F32 {
		if v < 0 {
			v *= alpha
		}
		y.F32[i] = v
	}
	return nil
}

func reduceOp(init float32, combine func(acc, v float32) float32, mean bool) kernel {
	return func(o *OpDesc, in, out []*Tensor) error {
		x, y := in[0], out[0]
		axes, shape, err := reductionAxesShape(x, in[1], o.keepDims)
		if err != nil {
			return err
		}
		y.resize(shape)

		// Output strides indexed by input axis: reduced axes contribute nothing, and the
		// remaining axes keep their relative order, which is what the output shape is.
		os := make([]int, len(x.Shape))
		stride := 1
		for reverse := range x.Shape {
			d := len(x.Shape) - 1 - reverse
			if axes[d] {
				continue
			}
			os[d] = stride
			stride *= x.Shape[d]
		}

		for i := range y.F32 {
			y.F32[i] = init
		}
		idx := make([]int, len(x.Shape))
		yo := 0
		for i := range x.Count() {
			y.F32[yo] = combine(y.F32[yo], x.F32[i])
			yo = step1(idx, x.Shape, os, yo)
		}

		if mean {
			n := 1
			for d, r := range axes {
				if r {
					n *= x.Shape[d]
				}
			}
			inv := 1 / float32(n)
			for i := range y.F32 {
				y.F32[i] *= inv
			}
		}
		return nil
	}
}

func reductionAxesShape(x, axesTensor *Tensor, keep bool) ([]bool, []int, error) {
	rank := len(x.Shape)
	axes := make([]bool, rank)
	if axesTensor != nil && len(axesTensor.I16) > 0 {
		if !integerType(axesTensor.Type) {
			return nil, nil, fmt.Errorf("axes have type %s, want int32 or int64", axesTensor.Type)
		}
		for _, a := range axesTensor.I16 {
			d := int(a)
			if d < 0 {
				d += rank
			}
			if d < 0 || d >= rank {
				return nil, nil, fmt.Errorf("axis %d out of range for shape %v", a, x.Shape)
			}
			axes[d] = true
		}
	} else {
		for d := range axes {
			axes[d] = true
		}
	}
	shape := make([]int, 0, rank)
	for d, n := range x.Shape {
		switch {
		case !axes[d]:
			shape = append(shape, n)
		case keep:
			shape = append(shape, 1)
		}
	}
	return axes, shape, nil
}

func shapeOf(o *OpDesc, in, out []*Tensor) error {
	if len(in) != 1 || len(out) != 1 || in[0] == nil || out[0] == nil {
		return errors.New("shape needs one input and one output")
	}
	x, y := in[0], out[0]
	if !integerType(y.Type) {
		return fmt.Errorf("shape output has type %s, want int32 or int64", y.Type)
	}
	y.resize([]int{len(x.Shape)})
	for i, d := range x.Shape {
		if d < 0 || d > maxTensorInteger {
			return fmt.Errorf("shape dimension %d does not fit this runtime's integer storage", d)
		}
		value, err := narrowSigned(uint64(d), 32)
		if err != nil {
			return fmt.Errorf("shape dimension %d: %w", d, err)
		}
		y.I16[i] = value
	}
	return nil
}

func integerType(t TensorType) bool { return t == Int32 || t == Int64 }

const maxTensorInteger = 32767

type stridedSliceParams struct {
	beginMask, endMask, ellipsisMask, newAxisMask, shrinkAxisMask uint32
	offset                                                        bool
}

func stridedSlice(o *OpDesc, in, out []*Tensor) error {
	if err := validateStridedSliceTensors(in, out); err != nil {
		return err
	}
	x, begin, end, stride, y := in[0], in[1], in[2], in[3], out[0]
	starts, steps, shape, axisOut, err := stridedSlicePlan(o.slice, x.Shape, begin, end, stride)
	if err != nil {
		return err
	}
	y.resize(shape)
	xs := contiguous(x.Shape)
	base := 0
	for d := range x.Shape {
		base += starts[d] * xs[d]
	}
	for i := range y.Count() {
		offset := base
		for d := range x.Shape {
			if od := axisOut[d]; od >= 0 {
				offset += coordinate(i, shape, od) * steps[d] * xs[d]
			}
		}
		if x.Type == Float32 {
			y.F32[i] = x.F32[offset]
		} else {
			y.I16[i] = x.I16[offset]
		}
	}
	return nil
}

func validateStridedSliceTensors(in, out []*Tensor) error {
	if len(in) != 4 || len(out) != 1 {
		return errors.New("strided slice needs four inputs and one output")
	}
	for _, tensor := range append(in, out...) {
		if tensor == nil {
			return errors.New("strided slice does not allow omitted tensors")
		}
	}
	x, begin, end, stride, y := in[0], in[1], in[2], in[3], out[0]
	if x.Type != y.Type || (x.Type != Float32 && !integerType(x.Type)) {
		return fmt.Errorf("strided slice supports matching float32, int32, or int64 tensors, got %s and %s", x.Type, y.Type)
	}
	if !integerType(begin.Type) || !integerType(end.Type) || !integerType(stride.Type) {
		return errors.New("begin, end, and stride must be int32 or int64")
	}
	return nil
}

func stridedSlicePlan(params stridedSliceParams, inputShape []int, begin, end, stride *Tensor) ([]int, []int, []int, []int, error) {
	if params.ellipsisMask != 0 || params.newAxisMask != 0 || params.offset {
		return nil, nil, nil, nil, fmt.Errorf("unsupported strided-slice masks ellipsis=%#x new_axis=%#x offset=%t", params.ellipsisMask, params.newAxisMask, params.offset)
	}
	rank := len(inputShape)
	if maskExceedsRank(params.beginMask, rank) || maskExceedsRank(params.endMask, rank) || maskExceedsRank(params.shrinkAxisMask, rank) {
		return nil, nil, nil, nil, fmt.Errorf("strided-slice mask exceeds input rank %d", rank)
	}
	if len(begin.I16) != rank || len(end.I16) != rank || len(stride.I16) != rank {
		return nil, nil, nil, nil, fmt.Errorf("begin, end, and stride must each have %d values", rank)
	}
	starts := make([]int, rank)
	steps := make([]int, rank)
	shape := make([]int, 0, rank)
	axisOut := make([]int, rank)
	for d := range rank {
		axisOut[d] = -1
		shrunk := params.shrinkAxisMask&(1<<d) != 0
		start, size, step, err := sliceAxis(inputShape[d], int(begin.I16[d]), int(end.I16[d]), int(stride.I16[d]), params.beginMask&(1<<d) != 0, params.endMask&(1<<d) != 0, shrunk)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("axis %d: %w", d, err)
		}
		starts[d], steps[d] = start, step
		if !shrunk {
			axisOut[d] = len(shape)
			shape = append(shape, size)
		}
	}
	return starts, steps, shape, axisOut, nil
}

func maskExceedsRank(mask uint32, rank int) bool {
	return rank < 32 && mask>>rank != 0
}

func coordinate(index int, shape []int, axis int) int {
	for d := len(shape) - 1; d > axis; d-- {
		index /= shape[d]
	}
	return index % shape[axis]
}

func sliceAxis(dim, begin, end, stride int, beginMask, endMask, shrink bool) (start, size, step int, err error) {
	if dim < 0 || stride == 0 {
		return 0, 0, 0, errors.New("dimension and stride must be non-zero and non-negative")
	}
	if shrink {
		if beginMask {
			begin = 0
		}
		if begin < 0 {
			begin += dim
		}
		if begin < 0 || begin >= dim {
			return 0, 0, 0, fmt.Errorf("shrink index %d out of range for %d values", begin, dim)
		}
		return begin, 1, stride, nil
	}
	if stride > 0 {
		if beginMask {
			begin = 0
		} else {
			begin = clampSliceIndex(begin, dim, 0, dim)
		}
		if endMask {
			end = dim
		} else {
			end = clampSliceIndex(end, dim, 0, dim)
		}
		return begin, max(0, (end-begin+stride-1)/stride), stride, nil
	}
	if beginMask {
		begin = dim - 1
	} else {
		begin = clampSliceIndex(begin, dim, -1, dim-1)
	}
	if endMask {
		end = -1
	} else {
		end = clampSliceIndex(end, dim, -1, dim-1)
	}
	step = -stride
	return begin, max(0, (begin-end+step-1)/step), stride, nil
}

func clampSliceIndex(index, dim, low, high int) int {
	if index < 0 {
		index += dim
	}
	return min(max(index, low), high)
}

func reduceProd(o *OpDesc, in, out []*Tensor) error {
	if len(in) != 2 || len(out) != 1 || in[0] == nil || in[1] == nil || out[0] == nil {
		return errors.New("reduce prod needs two inputs and one output")
	}
	x, y := in[0], out[0]
	if x.Type != y.Type || (x.Type != Float32 && !integerType(x.Type)) {
		return fmt.Errorf("reduce prod supports matching float32, int32, or int64 tensors, got %s and %s", x.Type, y.Type)
	}
	axes, shape, err := reductionAxesShape(x, in[1], o.keepDims)
	if err != nil {
		return err
	}
	y.resize(shape)
	os := make([]int, len(x.Shape))
	stride := 1
	for reverse := range x.Shape {
		d := len(x.Shape) - 1 - reverse
		if !axes[d] {
			os[d] = stride
			stride *= x.Shape[d]
		}
	}
	if x.Type == Float32 {
		for i := range y.F32 {
			y.F32[i] = 1
		}
	} else {
		for i := range y.I16 {
			y.I16[i] = 1
		}
	}
	idx, yo := make([]int, len(x.Shape)), 0
	for i := range x.Count() {
		if x.Type == Float32 {
			y.F32[yo] *= x.F32[i]
		} else {
			y.I16[yo] *= x.I16[i]
		}
		yo = step1(idx, x.Shape, os, yo)
	}
	return nil
}

func pack(o *OpDesc, in, out []*Tensor) error {
	if len(in) == 0 || len(out) != 1 {
		return errors.New("pack needs inputs and one output")
	}
	for _, tensor := range append(in, out...) {
		if tensor == nil {
			return errors.New("pack does not allow omitted tensors")
		}
	}
	if o.values != 0 && o.values != len(in) {
		return fmt.Errorf("pack options declare %d values, got %d", o.values, len(in))
	}
	base, y := in[0], out[0]
	if base.Type != y.Type || (base.Type != Float32 && !integerType(base.Type)) {
		return fmt.Errorf("pack supports matching float32, int32, or int64 tensors, got %s and %s", base.Type, y.Type)
	}
	for i, x := range in[1:] {
		if x.Type != base.Type || !equalShape(base.Shape, x.Shape) {
			return fmt.Errorf("input %d has type %s and shape %v, want %s %v", i+1, x.Type, x.Shape, base.Type, base.Shape)
		}
	}
	axis := o.axis
	if axis < 0 {
		axis += len(base.Shape) + 1
	}
	if axis < 0 || axis > len(base.Shape) {
		return fmt.Errorf("axis %d out of range for shape %v", o.axis, base.Shape)
	}
	shape := make([]int, 0, len(base.Shape)+1)
	shape = append(shape, base.Shape[:axis]...)
	shape = append(shape, len(in))
	shape = append(shape, base.Shape[axis:]...)
	y.resize(shape)
	outer, inner := count(base.Shape[:axis]), count(base.Shape[axis:])
	for b := range outer {
		for i, x := range in {
			start := (b*len(in) + i) * inner
			if base.Type == Float32 {
				copy(y.F32[start:], x.F32[b*inner:(b+1)*inner])
			} else {
				copy(y.I16[start:], x.I16[b*inner:(b+1)*inner])
			}
		}
	}
	return nil
}

func fill(o *OpDesc, in, out []*Tensor) error {
	if len(in) != 2 || len(out) != 1 || in[0] == nil || in[1] == nil || out[0] == nil {
		return errors.New("fill needs two inputs and one output")
	}
	shapeTensor, value, y := in[0], in[1], out[0]
	if !integerType(shapeTensor.Type) {
		return fmt.Errorf("fill shape has type %s, want int32 or int64", shapeTensor.Type)
	}
	if value.Type != y.Type || value.Count() != 1 {
		return fmt.Errorf("fill value has type %s and %d values, want one %s", value.Type, value.Count(), y.Type)
	}
	shape := make([]int, len(shapeTensor.I16))
	for i, d := range shapeTensor.I16 {
		if d < 0 {
			return fmt.Errorf("shape dimension %d is negative", d)
		}
		shape[i] = int(d)
	}
	if _, err := checkedCount(shape); err != nil {
		return err
	}
	y.resize(shape)
	if y.Type == Float32 {
		for i := range y.F32 {
			y.F32[i] = value.F32[0]
		}
		return nil
	}
	if integerType(y.Type) {
		for i := range y.I16 {
			y.I16[i] = value.I16[0]
		}
		return nil
	}
	return fmt.Errorf("fill output has unsupported type %s", y.Type)
}

func checkedCount(shape []int) (int, error) {
	n := 1
	maxInt := int(^uint(0) >> 1)
	for _, d := range shape {
		if d < 0 || (d != 0 && n > maxInt/d) {
			return 0, fmt.Errorf("invalid shape %v", shape)
		}
		n *= d
	}
	return n, nil
}

func padZero(o *OpDesc, in, out []*Tensor) error {
	x, p, y := in[0], in[1], out[0]
	rank := len(x.Shape)
	if len(p.I16) < 2*rank {
		return fmt.Errorf("padding has %d values for a %d-D input", len(p.I16), rank)
	}

	shape := make([]int, rank)
	for d := range rank {
		shape[d] = int(p.I16[2*d]) + x.Shape[d] + int(p.I16[2*d+1])
	}
	y.resize(shape)
	for i := range y.F32 {
		y.F32[i] = 0
	}

	os := contiguous(shape)
	base := 0
	for d := range rank {
		base += int(p.I16[2*d]) * os[d]
	}

	idx := make([]int, rank)
	yo := base
	for i := range x.Count() {
		y.F32[yo] = x.F32[i]
		yo = step1(idx, x.Shape, os, yo)
	}
	return nil
}

func transpose(o *OpDesc, in, out []*Tensor) error {
	x, p, y := in[0], in[1], out[0]
	rank := len(x.Shape)
	if len(p.I16) != rank {
		return fmt.Errorf("permutation has %d values for a %d-D input", len(p.I16), rank)
	}

	perm := make([]int, rank)
	shape := make([]int, rank)
	for i := range rank {
		d := int(p.I16[i])
		if d < 0 {
			d += rank
		}
		if d < 0 || d >= rank {
			return fmt.Errorf("permutation %v out of range for shape %v", p.I16, x.Shape)
		}
		perm[i] = d
		shape[i] = x.Shape[d]
	}
	y.resize(shape)

	xs := contiguous(x.Shape)
	ps := make([]int, rank)
	for i := range rank {
		ps[i] = xs[perm[i]]
	}

	idx := make([]int, rank)
	xo := 0
	for i := range y.F32 {
		y.F32[i] = x.F32[xo]
		xo = step1(idx, shape, ps, xo)
	}
	return nil
}

func copyData(y, x *Tensor) {
	if x.Type == Float32 {
		copy(y.F32, x.F32)
		return
	}
	copy(y.I16, x.I16)
}

func reshape(o *OpDesc, in, out []*Tensor) error {
	x, y := in[0], out[0]

	var target []int
	if len(in) > 1 && in[1] != nil && len(in[1].I16) > 0 {
		for _, v := range in[1].I16 {
			target = append(target, int(v))
		}
	} else {
		target = append(target, o.dims...)
	}
	if target == nil {
		return errors.New("no target shape")
	}

	// One dimension may be -1, standing for whatever makes the count work out.
	total := x.Count()
	known, free := 1, -1
	for i, d := range target {
		if d < 0 {
			free = i
			continue
		}
		known *= d
	}
	if free >= 0 {
		if known == 0 || total%known != 0 {
			return fmt.Errorf("cannot reshape %d values into %v", total, target)
		}
		target[free] = total / known
	} else if known != total {
		return fmt.Errorf("cannot reshape %d values into %v", total, target)
	}

	y.resize(target)
	copyData(y, x)
	return nil
}

func squeeze(o *OpDesc, in, out []*Tensor) error {
	x, y := in[0], out[0]
	rank := len(x.Shape)

	drop := make([]bool, rank)
	dims := o.dims
	if len(dims) == 0 {
		for d, n := range x.Shape {
			drop[d] = n == 1
		}
	} else {
		for _, d := range dims {
			if d < 0 {
				d += rank
			}
			if d < 0 || d >= rank {
				return fmt.Errorf("squeeze dim %d out of range for shape %v", d, x.Shape)
			}
			if x.Shape[d] != 1 {
				return fmt.Errorf("cannot squeeze axis %d of shape %v", d, x.Shape)
			}
			drop[d] = true
		}
	}

	var shape []int
	for d, n := range x.Shape {
		if !drop[d] {
			shape = append(shape, n)
		}
	}
	y.resize(shape)
	copyData(y, x)
	return nil
}

func expandDims(o *OpDesc, in, out []*Tensor) error {
	x, a, y := in[0], in[1], out[0]
	if len(a.I16) == 0 {
		return errors.New("no axis")
	}
	axis := int(a.I16[0])
	if axis < 0 {
		axis += len(x.Shape) + 1
	}
	if axis < 0 || axis > len(x.Shape) {
		return fmt.Errorf("axis %d out of range for shape %v", a.I16[0], x.Shape)
	}

	shape := make([]int, 0, len(x.Shape)+1)
	shape = append(shape, x.Shape[:axis]...)
	shape = append(shape, 1)
	shape = append(shape, x.Shape[axis:]...)
	y.resize(shape)
	copyData(y, x)
	return nil
}

var kernels = map[Op]kernel{
	OpConv2D:         conv2D,
	OpMaxPool2D:      maxPool2D,
	OpFullyConnected: fullyConnected,
	OpBatchMatMul:    batchMatMul,

	OpAdd:     add,
	OpSub:     elementwise(func(a, b float32) float32 { return a - b }),
	OpMul:     elementwise(func(a, b float32) float32 { return a * b }),
	OpDiv:     elementwise(func(a, b float32) float32 { return a / b }),
	OpMaximum: elementwise(func(a, b float32) float32 { return max(a, b) }),
	OpMinimum: elementwise(func(a, b float32) float32 { return min(a, b) }),
	OpSquaredDiff: elementwise(func(a, b float32) float32 {
		d := a - b
		return d * d
	}),

	OpLogistic: unary(func(v float32) float32 {
		return float32(1 / (1 + math.Exp(-float64(v))))
	}),
	OpLog:    unary(func(v float32) float32 { return float32(math.Log(float64(v))) }),
	OpExp:    unary(func(v float32) float32 { return float32(math.Exp(float64(v))) }),
	OpSqrt:   unary(func(v float32) float32 { return float32(math.Sqrt(float64(v))) }),
	OpRsqrt:  unary(func(v float32) float32 { return 1 / float32(math.Sqrt(float64(v))) }),
	OpSquare: unary(func(v float32) float32 { return v * v }),
	OpRelu:   unary(func(v float32) float32 { return max(v, 0) }),
	OpRelu6:  unary(func(v float32) float32 { return min(max(v, 0), 6) }),

	OpLeakyRelu: leakyRelu,

	OpMean:       reduceOp(0, func(a, v float32) float32 { return a + v }, true),
	OpSum:        reduceOp(0, func(a, v float32) float32 { return a + v }, false),
	OpReduceMax:  reduceOp(float32(math.Inf(-1)), func(a, v float32) float32 { return max(a, v) }, false),
	OpReduceProd: reduceProd,

	OpShape:        shapeOf,
	OpStridedSlice: stridedSlice,
	OpPack:         pack,
	OpFill:         fill,
	OpPad:          padZero,
	OpTranspose:    transpose,
	OpReshape:      reshape,
	OpSqueeze:      squeeze,
	OpExpandDims:   expandDims,
}
