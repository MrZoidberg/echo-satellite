// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Interpreter runs one subgraph. It is not safe for concurrent use: tensors are reused between
// invocations so that steady-state inference does not allocate.
type Interpreter struct {
	model   *Model
	graph   *Subgraph
	tensors []*Tensor
}

// New prepares the model's first subgraph for execution.
func New(m *Model) (*Interpreter, error) {
	if m == nil || len(m.Subgraphs) == 0 || m.Subgraphs[0] == nil {
		return nil, fmt.Errorf("%w: model has no executable subgraph", ErrBadModel)
	}
	g := m.Subgraphs[0]
	in := &Interpreter{model: m, graph: g, tensors: make([]*Tensor, len(g.Tensors))}

	for i, d := range g.Tensors {
		t := &Tensor{Name: d.Name, Type: d.Type, Shape: append([]int(nil), d.Shape...)}
		raw := m.buffer(d.Buffer)
		if raw != nil {
			if err := decode(t, raw); err != nil {
				return nil, fmt.Errorf("%w: tensor %d (%s): %w", ErrBadModel, i, d.Name, err)
			}
			t.Const = true
		} else {
			switch d.Type {
			case Float32, Int32, Int64, Bool:
			default:
				return nil, fmt.Errorf("%w: tensor %d (%s) has unsupported type %s", ErrBadModel, i, d.Name, d.Type)
			}
			t.resize(t.Shape)
		}
		in.tensors[i] = t
	}

	for i, o := range g.Ops {
		if _, ok := kernels[o.Op]; !ok {
			return nil, fmt.Errorf("%w: %s at operator %d", ErrUnsupportedOp, o.Name(), i)
		}
	}
	return in, nil
}

func decode(t *Tensor, raw []byte) error {
	width := 1
	switch t.Type {
	case Float32, Int32:
		width = 4
	case Int64:
		width = 8
	case Bool:
	case Int8:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	default:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	}
	if expected := t.Count() * width; len(raw) != expected {
		return fmt.Errorf("constant has %d bytes, shape %v and type %s require %d", len(raw), t.Shape, t.Type, expected)
	}
	switch t.Type {
	case Float32:
		n := len(raw) / 4
		t.F32 = make([]float32, n)
		for i := range t.F32 {
			t.F32[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
		}
	case Int32:
		n := len(raw) / 4
		t.I16 = make([]int16, n)
		for i := range t.I16 {
			value, err := narrowSigned(uint64(binary.LittleEndian.Uint32(raw[4*i:])), 32)
			if err != nil {
				return err
			}
			t.I16[i] = value
		}
	case Int64:
		n := len(raw) / 8
		t.I16 = make([]int16, n)
		for i := range t.I16 {
			value, err := narrowSigned(binary.LittleEndian.Uint64(raw[8*i:]), 64)
			if err != nil {
				return err
			}
			t.I16[i] = value
		}
	case Bool:
		t.I16 = make([]int16, len(raw))
		for i, b := range raw {
			if b != 0 {
				t.I16[i] = 1
			}
		}
	case Int8:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	default:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	}
	return nil
}

func narrowSigned(bits uint64, width int) (int16, error) {
	var value int64
	switch width {
	case 32:
		bits &= math.MaxUint32
		if bits <= math.MaxInt32 {
			value = int64(bits)
		} else {
			value = -1 - int64((^bits)&math.MaxUint32)
		}
	default:
		if bits <= math.MaxInt64 {
			value = int64(bits)
		} else {
			value = -1 - int64(^bits) //nolint:gosec // complement is <= MaxInt64 in this branch.
		}
	}
	if value < math.MinInt16 || value > math.MaxInt16 {
		return 0, fmt.Errorf("integer constant %d does not fit int16", value)
	}
	return int16(value), nil
}

// Tensor returns a tensor by graph index.
func (in *Interpreter) Tensor(i int) *Tensor { return in.tensors[i] }

// Input returns the i'th subgraph input.
func (in *Interpreter) Input(i int) *Tensor { return in.tensors[in.graph.Inputs[i]] }

// InputShape returns a defensive copy of the i'th input shape.
func (in *Interpreter) InputShape(i int) []int {
	return append([]int(nil), in.Input(i).Shape...)
}

// Output returns the i'th subgraph output.
func (in *Interpreter) Output(i int) *Tensor { return in.tensors[in.graph.Outputs[i]] }

// ResizeInput sets an input's shape. Every other shape follows from it during Invoke, so this is
// the only place a caller has to think about dynamic dimensions.
func (in *Interpreter) ResizeInput(i int, shape []int) {
	in.Input(i).resize(shape)
}

// Invoke runs every operator in order, recomputing shapes as it goes.
func (in *Interpreter) Invoke() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: invocation panic: %v", ErrBadModel, recovered)
		}
	}()
	for i, o := range in.graph.Ops {
		ins := make([]*Tensor, len(o.Inputs))
		for j, idx := range o.Inputs {
			// An optional input, such as a missing bias, is encoded as index -1.
			if idx >= 0 {
				ins[j] = in.tensors[idx]
			}
		}
		outs := make([]*Tensor, len(o.Outputs))
		for j, idx := range o.Outputs {
			outs[j] = in.tensors[idx]
		}
		if err := kernels[o.Op](o, ins, outs); err != nil {
			return fmt.Errorf("%w: op %d (%s): %w", ErrShapeMismatch, i, o.Op, err)
		}
	}
	return nil
}

type kernel func(o *OpDesc, in, out []*Tensor) error

func activate(v float32, a Activation) float32 {
	switch a {
	case ActNone, ActSignBit:
		return v
	case ActRelu:
		if v < 0 {
			return 0
		}
	case ActRelu1:
		if v < -1 {
			return -1
		}
		if v > 1 {
			return 1
		}
	case ActRelu6:
		if v < 0 {
			return 0
		}
		if v > 6 {
			return 6
		}
	case ActTanh:
		return float32(math.Tanh(float64(v)))
	}
	return v
}

// padFor is TFLite's padding split for SAME: the total padding is distributed with the smaller
// half before the data.
func padFor(stride, dilation, in, filter, out int) int {
	effective := (filter-1)*dilation + 1
	p := ((out-1)*stride + effective - in) / 2
	if p < 0 {
		return 0
	}
	return p
}

func outputSize(in, filter, stride, dilation int, samePad bool) int {
	if samePad {
		return (in + stride - 1) / stride
	}
	effective := (filter-1)*dilation + 1
	if in < effective {
		return 0
	}
	return (in-effective)/stride + 1
}
