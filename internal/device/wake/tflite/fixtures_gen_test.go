// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"encoding/binary"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var updateFixtures = flag.Bool("update-fixtures", false, "rewrite testdata/wake/synthetic")

type flatWriter struct{ data []byte }

func newFlatWriter() *flatWriter {
	return &flatWriter{data: []byte{0, 0, 0, 0, 'T', 'F', 'L', '3'}}
}

func (w *flatWriter) align4() {
	for len(w.data)%4 != 0 {
		w.data = append(w.data, 0)
	}
}

func (w *flatWriter) table(slots int, present ...int) uint32 {
	w.align4()
	vtable := len(w.data)
	vlen, objectLen := 4+2*slots, 4+4*slots
	w.data = append(w.data, make([]byte, vlen)...)
	binary.LittleEndian.PutUint16(w.data[vtable:], uint16(vlen))        //nolint:gosec // test schemas have at most eight slots.
	binary.LittleEndian.PutUint16(w.data[vtable+2:], uint16(objectLen)) //nolint:gosec // test schemas have at most eight slots.
	for _, slot := range present {
		binary.LittleEndian.PutUint16(w.data[vtable+4+2*slot:], uint16(4+4*slot)) //nolint:gosec // slot is from a fixed tiny test schema.
	}
	w.align4()
	table := len(w.data)
	w.data = append(w.data, make([]byte, objectLen)...)
	binary.LittleEndian.PutUint32(w.data[table:], uint32(table-vtable)) //nolint:gosec // tiny generated test buffer is far below uint32 limits.
	return uint32(table)                                                //nolint:gosec // tiny generated test buffer is far below uint32 limits.
}

func field(table uint32, slot int) uint32 {
	return table + 4 + 4*uint32(slot) //nolint:gosec // slot is from a fixed tiny test schema.
}

func (w *flatWriter) scalar8(table uint32, slot int, value byte) {
	w.data[field(table, slot)] = value
}

func (w *flatWriter) scalar32(table uint32, slot int, value uint32) {
	binary.LittleEndian.PutUint32(w.data[field(table, slot):], value)
}

func (w *flatWriter) offset(at, target uint32) {
	binary.LittleEndian.PutUint32(w.data[at:], target-at)
}

func (w *flatWriter) bytes(values []byte) uint32 {
	w.align4()
	pos := uint32(len(w.data)) //nolint:gosec // tiny generated test buffer is far below uint32 limits.
	w.data = append(w.data, make([]byte, 4)...)
	binary.LittleEndian.PutUint32(w.data[pos:], uint32(len(values))) //nolint:gosec // fixture byte vectors are tiny.
	w.data = append(w.data, values...)
	return pos
}

func (w *flatWriter) ints(values []int) uint32 {
	w.align4()
	pos := uint32(len(w.data)) //nolint:gosec // tiny generated test buffer is far below uint32 limits.
	w.data = append(w.data, make([]byte, 4+4*len(values))...)
	binary.LittleEndian.PutUint32(w.data[pos:], uint32(len(values))) //nolint:gosec // fixture integer vectors are tiny.
	for i, value := range values {
		binary.LittleEndian.PutUint32(w.data[pos+4+4*uint32(i):], uint32(int32(value))) //nolint:gosec // fixture shapes and indices are fixed small values.
	}
	return pos
}

func (w *flatWriter) offsets(count int) (uint32, []uint32) {
	w.align4()
	pos := uint32(len(w.data)) //nolint:gosec // tiny generated test buffer is far below uint32 limits.
	w.data = append(w.data, make([]byte, 4+4*count)...)
	binary.LittleEndian.PutUint32(w.data[pos:], uint32(count)) //nolint:gosec // fixture vector counts are tiny.
	fields := make([]uint32, count)
	for i := range fields {
		fields[i] = pos + 4 + 4*uint32(i)
	}
	return pos, fields
}

func floats(values ...float32) []byte {
	out := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(value))
	}
	return out
}

func int32s(values ...int32) []byte {
	out := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(out[4*i:], uint32(value)) //nolint:gosec // preserves the fixture's signed two's-complement bits.
	}
	return out
}

type syntheticTensor struct {
	shape  []int
	typeID TensorType
	buffer int
}

type syntheticOp struct {
	op      Op
	inputs  []int
	outputs []int
	options func(*flatWriter) uint32
}

type syntheticModel struct {
	tensors []syntheticTensor
	inputs  []int
	outputs []int
	op      syntheticOp
	buffers [][]byte
}

func buildSynthetic(spec syntheticModel) []byte {
	w := newFlatWriter()
	rootModel := w.table(5, 1, 2, 4)
	binary.LittleEndian.PutUint32(w.data, rootModel)

	opcodes, opcodeFields := w.offsets(1)
	w.offset(field(rootModel, modelOperatorCodes), opcodes)
	opcode := w.table(4, opcodeDeprecatedBuiltin, opcodeBuiltin)
	w.offset(opcodeFields[0], opcode)
	w.scalar8(opcode, opcodeDeprecatedBuiltin, byte(spec.op.op)) //nolint:gosec // deprecated byte is intentionally the low byte.
	w.scalar32(opcode, opcodeBuiltin, uint32(spec.op.op))        //nolint:gosec // preserves unknown signed opcode bits for rejection tests.

	subgraphs, subgraphFields := w.offsets(1)
	w.offset(field(rootModel, modelSubgraphs), subgraphs)
	subgraph := w.table(5, subgraphTensors, subgraphInputs, subgraphOutputs, subgraphOperators)
	w.offset(subgraphFields[0], subgraph)

	tensorVector, tensorFields := w.offsets(len(spec.tensors))
	w.offset(field(subgraph, subgraphTensors), tensorVector)
	for i, tensor := range spec.tensors {
		table := w.table(8, tensorShape, tensorType, tensorBuffer)
		w.offset(tensorFields[i], table)
		shape := w.ints(tensor.shape)
		w.offset(field(table, tensorShape), shape)
		w.scalar8(table, tensorType, byte(tensor.typeID))
		w.scalar32(table, tensorBuffer, uint32(tensor.buffer)) //nolint:gosec // fixture buffer indices are fixed nonnegative values.
	}

	inputs := w.ints(spec.inputs)
	outputs := w.ints(spec.outputs)
	w.offset(field(subgraph, subgraphInputs), inputs)
	w.offset(field(subgraph, subgraphOutputs), outputs)

	operators, operatorFields := w.offsets(1)
	w.offset(field(subgraph, subgraphOperators), operators)
	operator := w.table(5, operatorOpcodeIndex, operatorInputs, operatorOutputs, operatorOptions)
	w.offset(operatorFields[0], operator)
	opInputs := w.ints(spec.op.inputs)
	opOutputs := w.ints(spec.op.outputs)
	w.offset(field(operator, operatorInputs), opInputs)
	w.offset(field(operator, operatorOutputs), opOutputs)
	if spec.op.options != nil {
		options := spec.op.options(w)
		w.offset(field(operator, operatorOptions), options)
	}

	buffers, bufferFields := w.offsets(len(spec.buffers) + 1)
	w.offset(field(rootModel, modelBuffers), buffers)
	for i := range len(spec.buffers) + 1 {
		present := []int(nil)
		if i > 0 {
			present = []int{bufferData}
		}
		table := w.table(3, present...)
		w.offset(bufferFields[i], table)
		if i > 0 {
			data := w.bytes(spec.buffers[i-1])
			w.offset(field(table, bufferData), data)
		}
	}
	return w.data
}

func activationOptions(w *flatWriter) uint32 { return w.table(1, 0) }

func convOptions(w *flatWriter) uint32 {
	table := w.table(6, 0, 1, 2, 3, 4, 5)
	w.scalar8(table, 0, 1) // VALID
	w.scalar32(table, 1, 1)
	w.scalar32(table, 2, 1)
	w.scalar32(table, 4, 1)
	w.scalar32(table, 5, 1)
	return table
}

func poolOptions(w *flatWriter) uint32 {
	table := w.table(6, 0, 1, 2, 3, 4, 5)
	w.scalar8(table, 0, 1) // VALID
	w.scalar32(table, 1, 1)
	w.scalar32(table, 2, 1)
	w.scalar32(table, 3, 2)
	w.scalar32(table, 4, 2)
	return table
}

func leakyOptions(w *flatWriter) uint32 {
	table := w.table(1, 0)
	w.scalar32(table, 0, math.Float32bits(0.25))
	return table
}

func syntheticFixtures() map[string][]byte {
	return map[string][]byte{
		"oww_classifier.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 1, 96}, Float32, 0}, {[]int{1, 96}, Float32, 1}, {[]int{1}, Float32, 2}, {[]int{1, 1}, Float32, 0}},
			inputs:  []int{0}, outputs: []int{3}, op: syntheticOp{OpFullyConnected, []int{0, 1, 2}, []int{3}, activationOptions},
			buffers: [][]byte{floats(make([]float32, 96)...), floats(0)},
		}),
		"oww_unsupported.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 1, 96}, Float32, 0}, {[]int{1, 1, 96}, Float32, 0}},
			inputs:  []int{0}, outputs: []int{1}, op: syntheticOp{Op(999), []int{0}, []int{1}, nil},
		}),
		"fully_connected.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2}, Float32, 0}, {[]int{1, 2}, Float32, 1}, {[]int{1}, Float32, 2}, {[]int{1, 1}, Float32, 0}},
			inputs:  []int{0}, outputs: []int{3}, op: syntheticOp{OpFullyConnected, []int{0, 1, 2}, []int{3}, activationOptions},
			buffers: [][]byte{floats(2, -1), floats(0.5)},
		}),
		"reshape.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2}, Float32, 0}, {[]int{2}, Int32, 1}, {[]int{2, 1}, Float32, 0}},
			inputs:  []int{0}, outputs: []int{2}, op: syntheticOp{OpReshape, []int{0, 1}, []int{2}, activationOptions},
			buffers: [][]byte{int32s(2, 1)},
		}),
		"logistic.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{2}, Float32, 0}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{1},
			op: syntheticOp{OpLogistic, []int{0}, []int{1}, activationOptions},
		}),
		"conv2d.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2, 2, 1}, Float32, 0}, {[]int{1, 1, 1, 1}, Float32, 1}, {[]int{1}, Float32, 2}, {[]int{1, 2, 2, 1}, Float32, 0}},
			inputs:  []int{0}, outputs: []int{3}, op: syntheticOp{OpConv2D, []int{0, 1, 2}, []int{3}, convOptions},
			buffers: [][]byte{floats(2), floats(0.5)},
		}),
		"oww_embedding.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{
				{[]int{1, 76, 32, 1}, Float32, 0},
				{[]int{96, 2, 32, 1}, Float32, 1},
				{[]int{1, 75, 1, 96}, Float32, 0},
			},
			inputs:  []int{0},
			outputs: []int{2},
			op:      syntheticOp{OpConv2D, []int{0, 1}, []int{2}, convOptions},
			buffers: [][]byte{owwEmbeddingWeights()},
		}),
		"add.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{2}, Float32, 0}, {[]int{2}, Float32, 1}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpAdd, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{floats(10, 20)},
		}),
		"mul.tflite":        binaryFixture(OpMul, floats(3, 4)),
		"sub.tflite":        binaryFixture(OpSub, floats(3, 4)),
		"minimum.tflite":    binaryFixture(OpMinimum, floats(3, 4)),
		"maximum.tflite":    binaryFixture(OpMaximum, floats(3, 4)),
		"log.tflite":        unaryFixture(OpLog, activationOptions),
		"leaky_relu.tflite": unaryFixture(OpLeakyRelu, leakyOptions),
		"expand_dims.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{2}, Float32, 0}, {[]int{1}, Int32, 1}, {[]int{1, 2}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpExpandDims, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{int32s(0)},
		}),
		"transpose.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{2, 2}, Float32, 0}, {[]int{2}, Int32, 1}, {[]int{2, 2}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpTranspose, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{int32s(1, 0)},
		}),
		"squeeze.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2, 1}, Float32, 0}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{1},
			op: syntheticOp{OpSqueeze, []int{0}, []int{1}, nil},
		}),
		"reduce_max.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{2, 2}, Float32, 0}, {[]int{1}, Int32, 1}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpReduceMax, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{int32s(1)},
		}),
		"pad.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2}, Float32, 0}, {[]int{2, 2}, Int32, 1}, {[]int{2, 3}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpPad, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{int32s(1, 0, 0, 1)},
		}),
		"max_pool2d.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2, 2, 1}, Float32, 0}, {[]int{1, 1, 1, 1}, Float32, 0}}, inputs: []int{0}, outputs: []int{1},
			op: syntheticOp{OpMaxPool2D, []int{0}, []int{1}, poolOptions},
		}),
		"batch_matmul.tflite": buildSynthetic(syntheticModel{
			tensors: []syntheticTensor{{[]int{1, 2}, Float32, 0}, {[]int{2, 1}, Float32, 1}, {[]int{1, 1}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
			op: syntheticOp{OpBatchMatMul, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{floats(2, 3)},
		}),
	}
}

func owwEmbeddingWeights() []byte {
	weights := make([]float32, 96*2*32)
	weights[0] = 1
	return floats(weights...)
}

func binaryFixture(op Op, constant []byte) []byte {
	return buildSynthetic(syntheticModel{
		tensors: []syntheticTensor{{[]int{2}, Float32, 0}, {[]int{2}, Float32, 1}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{2},
		op: syntheticOp{op, []int{0, 1}, []int{2}, activationOptions}, buffers: [][]byte{constant},
	})
}

func unaryFixture(op Op, options func(*flatWriter) uint32) []byte {
	return buildSynthetic(syntheticModel{
		tensors: []syntheticTensor{{[]int{2}, Float32, 0}, {[]int{2}, Float32, 0}}, inputs: []int{0}, outputs: []int{1},
		op: syntheticOp{op, []int{0}, []int{1}, options},
	})
}

func syntheticDir() string {
	return filepath.Join("..", "..", "..", "..", "testdata", "wake", "synthetic")
}

func TestFixtures_Regenerate(t *testing.T) {
	if !*updateFixtures {
		t.Skip("run with -update-fixtures to rewrite testdata/wake/synthetic")
	}
	if err := os.MkdirAll(syntheticDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, data := range syntheticFixtures() {
		if err := os.WriteFile(filepath.Join(syntheticDir(), name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
