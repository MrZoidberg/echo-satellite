// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import (
	"encoding/binary"
	"math"
)

// FlatBuffers accessors, enough of the format to read a .tflite file.
//
// A table is addressed by its position in the buffer and reached through a vtable that maps
// field slots to byte offsets within the table. A slot past the end of the vtable, or holding
// zero, means the field was not written and takes its default. Offsets to tables, strings and
// vectors are stored relative to the position of the offset itself, so following one is always
// an addition. Unions occupy two slots: a type byte followed by the value.
type buf []byte

func (b buf) u8(p uint32) uint8   { return b[p] }
func (b buf) u16(p uint32) uint16 { return binary.LittleEndian.Uint16(b[p:]) }
func (b buf) u32(p uint32) uint32 { return binary.LittleEndian.Uint32(b[p:]) }

//nolint:gosec // FlatBuffers stores signed scalars as two's-complement uint32 bits.
func (b buf) i32(p uint32) int32 {
	return int32(b.u32(p))
}
func (b buf) u64(p uint32) uint64 { return binary.LittleEndian.Uint64(b[p:]) }
func (b buf) f32(p uint32) float32 {
	return math.Float32frombits(b.u32(p))
}

type table struct {
	b   buf
	pos uint32
}

func root(b buf) table {
	return table{b, b.u32(0)}
}

// slot returns the offset of a field within the table, or zero when the field is absent.
func (t table) slot(id int) uint32 {
	// The soffset is signed and points backwards, and unsigned wraparound gets the subtraction
	// right for either sign.
	delta := t.b.i32(t.pos)
	if id < 0 {
		panic("tflite: invalid vtable offset")
	}
	vt := t.pos - uint32(delta) //nolint:gosec // signed wrap supports FlatBuffers vtables placed before or after a table.
	off := 4 + 2*uint32(id)     //nolint:gosec // id was checked nonnegative and schema slot counts are bounded by the buffer.
	if off >= uint32(t.b.u16(vt)) {
		return 0
	}
	return uint32(t.b.u16(vt + off))
}

func (t table) u8f(id int) uint8 {
	if o := t.slot(id); o != 0 {
		return t.b.u8(t.pos + o)
	}
	return 0
}

func (t table) i32f(id int, def int32) int32 {
	if o := t.slot(id); o != 0 {
		return t.b.i32(t.pos + o)
	}
	return def
}

func (t table) u32f(id int, def uint32) uint32 {
	if o := t.slot(id); o != 0 {
		return t.b.u32(t.pos + o)
	}
	return def
}

func (t table) u64f(id int, def uint64) uint64 {
	if o := t.slot(id); o != 0 {
		return t.b.u64(t.pos + o)
	}
	return def
}

func (t table) f32f(id int, def float32) float32 {
	if o := t.slot(id); o != 0 {
		return t.b.f32(t.pos + o)
	}
	return def
}

func (t table) boolf(id int) bool {
	if o := t.slot(id); o != 0 {
		return t.b.u8(t.pos+o) != 0
	}
	return false
}

func (t table) table(id int) (table, bool) {
	o := t.slot(id)
	if o == 0 {
		return table{}, false
	}
	p := t.pos + o
	return table{t.b, p + t.b.u32(p)}, true
}

func (t table) str(id int) string {
	o := t.slot(id)
	if o == 0 {
		return ""
	}
	p := t.pos + o
	p += t.b.u32(p)
	n := t.b.u32(p)
	return string(t.b[p+4 : p+4+n])
}

type vector struct {
	b   buf
	pos uint32
	n   int
}

func (t table) vec(id int) vector {
	o := t.slot(id)
	if o == 0 {
		return vector{}
	}
	p := t.pos + o
	p += t.b.u32(p)
	return vector{t.b, p + 4, int(t.b.u32(p))}
}

func (v vector) len() int { return v.n }

func (v vector) i32(i int) int32 {
	if i < 0 || i >= v.n {
		panic("tflite: vector index out of range")
	}
	return v.b.i32(v.pos + 4*uint32(i)) //nolint:gosec // i was range-checked against the parsed vector length.
}

func (v vector) table(i int) table {
	if i < 0 || i >= v.n {
		panic("tflite: vector index out of range")
	}
	p := v.pos + 4*uint32(i) //nolint:gosec // i was range-checked against the parsed vector length.
	return table{v.b, p + v.b.u32(p)}
}

func (v vector) bytes() []byte {
	if v.n == 0 {
		return nil
	}
	return v.b[v.pos : v.pos+uint32(v.n)] //nolint:gosec // parsed vector length is nonnegative and bounded by the recovered buffer access.
}

func (v vector) ints() []int {
	if v.n == 0 {
		return nil
	}
	out := make([]int, v.n)
	for i := range out {
		out[i] = int(v.i32(i))
	}
	return out
}
