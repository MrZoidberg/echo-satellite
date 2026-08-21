package wake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKind(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kind Kind
		text string
	}{
		{KindOpenWakeWord, "openwakeword"},
		{KindMicroWakeWord, "microwakeword"},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			got, err := ParseKind(test.text)
			require.NoError(t, err)
			assert.Equal(t, test.kind, got)
			assert.Equal(t, test.text, got.String())
		})
	}

	_, err := ParseKind("other")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownModelKind)
}
