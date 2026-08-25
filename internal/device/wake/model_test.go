package wake

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseSidecar_Valid(t *testing.T) {
	t.Parallel()
	raw := fmt.Appendf(nil, `{"schema":1,"id":"okay_nabu","kind":"openwakeword","phrase":"okay nabu","languages":["en"],"sample_rate":16000,"sha256":%q,"source":"local","license":"Apache-2.0"}`, testDigest)
	sidecar, err := ParseSidecar(raw)
	require.NoError(t, err)
	assert.Equal(t, "okay_nabu", sidecar.ID)
	assert.Equal(t, []string{"en"}, sidecar.Languages)
}

func TestParseSidecar_RejectsUnknownFields(t *testing.T) {
	t.Parallel()
	raw := fmt.Appendf(nil, `{"schema":1,"id":"okay_nabu","kind":"openwakeword","sha256":%q,"surprise":true}`, testDigest)
	_, err := ParseSidecar(raw)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidModel)
}

func TestParseSidecar_RequiresSchemaIDKindAndDigest(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"schema": fmt.Sprintf(`{"id":"model","kind":"openwakeword","sha256":%q}`, testDigest),
		"id":     fmt.Sprintf(`{"schema":1,"kind":"openwakeword","sha256":%q}`, testDigest),
		"kind":   fmt.Sprintf(`{"schema":1,"id":"model","sha256":%q}`, testDigest),
		"digest": `{"schema":1,"id":"model","kind":"openwakeword"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSidecar([]byte(raw))
			assert.Error(t, err)
		})
	}
}

func TestModelValidate_RejectsUnsafeID(t *testing.T) {
	t.Parallel()
	err := (Model{ID: "../escape", Kind: KindOpenWakeWord, SHA256: testDigest}).Validate()
	assert.ErrorIs(t, err, ErrInvalidModel)
}

func TestModelValidate_RejectsStoreReservedIDs(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"index", "melspectrogram", "embedding_model"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			err := (Model{ID: id, Kind: KindOpenWakeWord, SHA256: testDigest}).Validate()
			require.ErrorIs(t, err, ErrInvalidModel)
			assert.Contains(t, err.Error(), "reserved")
		})
	}
}
