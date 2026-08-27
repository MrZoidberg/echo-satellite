//go:build !linux

package mixer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_UnsupportedOnNonLinuxReturnsErrUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	m, err := Open(0)

	require.Error(t, err)
	assert.Nil(t, m)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}
