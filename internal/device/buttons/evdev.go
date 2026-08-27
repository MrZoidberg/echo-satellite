package buttons

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// Linux and Darwin builds for this repository are both 64-bit, so Linux's
// input_event record has two 64-bit kernel-long timeval fields and is 24 bytes.
// Reader accepts any io.Reader, making recorded events a complete test double.
const eventSize = 24

// ErrShortEvent identifies a truncated evdev input_event record.
var ErrShortEvent = errors.New("short input event")

type rawEvent struct {
	at    time.Time
	type_ uint16
	code  uint16
	value int32
}

func decodeEvent(data []byte) (rawEvent, error) {
	if len(data) < eventSize {
		return rawEvent{}, fmt.Errorf("%w: got %d bytes, need %d", ErrShortEvent, len(data), eventSize)
	}
	var wire struct {
		Seconds      int64
		Microseconds int64
		Type         uint16
		Code         uint16
		Value        int32
	}
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &wire); err != nil {
		return rawEvent{}, fmt.Errorf("decode input event: %w", err)
	}
	return rawEvent{
		at:    time.Unix(wire.Seconds, wire.Microseconds*int64(time.Microsecond)),
		type_: wire.Type,
		code:  wire.Code,
		value: wire.Value,
	}, nil
}

// Reader reads 64-bit Linux input_event records.
type Reader struct{ r io.Reader }

// NewReader creates an event reader over a device or fixture stream.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

func (r *Reader) Read() (rawEvent, error) {
	data := make([]byte, eventSize)
	n, err := io.ReadFull(r.r, data)
	if err != nil {
		if errors.Is(err, io.EOF) && n == 0 {
			return rawEvent{}, io.EOF
		}
		return rawEvent{}, fmt.Errorf("%w: got %d bytes, need %d: %w", ErrShortEvent, n, eventSize, err)
	}
	return decodeEvent(data)
}
