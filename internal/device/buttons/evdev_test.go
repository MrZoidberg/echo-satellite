package buttons

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeTestEvent(at time.Time, eventType uint16, code Key, value int32) []byte {
	data := make([]byte, eventSize)
	seconds := at.Unix()
	microseconds := int64(at.Nanosecond()) / int64(time.Microsecond)
	_, _ = binary.Encode(data[0:8], binary.LittleEndian, seconds)
	_, _ = binary.Encode(data[8:16], binary.LittleEndian, microseconds)
	binary.LittleEndian.PutUint16(data[16:18], eventType)
	binary.LittleEndian.PutUint16(data[18:20], uint16(code))
	_, _ = binary.Encode(data[20:24], binary.LittleEndian, value)
	return data
}

func TestDecodeEvent_ParsesTypeCodeValueFrom24ByteRecord(t *testing.T) {
	at := time.Unix(123, 456000000)
	event, err := decodeEvent(encodeTestEvent(at, evTypeKey, KeyAction, 1))
	require.NoError(t, err)
	assert.Equal(t, at, event.at)
	assert.Equal(t, evTypeKey, event.type_)
	assert.Equal(t, uint16(KeyAction), event.code)
	assert.Equal(t, int32(1), event.value)
}

func TestDecodeEvent_RejectsShortRecordWithErrShortEvent(t *testing.T) {
	_, err := decodeEvent(make([]byte, eventSize-1))
	assert.ErrorIs(t, err, ErrShortEvent)
}

func TestReader_ReadsRecordsAndReportsEOF(t *testing.T) {
	r := NewReader(bytes.NewReader(encodeTestEvent(time.Unix(1, 0), evTypeKey, KeyMute, 0)))
	_, err := r.Read()
	require.NoError(t, err)
	_, err = r.Read()
	assert.ErrorIs(t, err, io.EOF)
}

func TestReader_WrapsTruncatedRecord(t *testing.T) {
	_, err := NewReader(bytes.NewReader([]byte{1})).Read()
	assert.ErrorIs(t, err, ErrShortEvent)
}
