package buttons

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateFixtures = flag.Bool("update-fixtures", false, "rewrite generated button fixtures")

func buttonFixtures() map[string][]byte {
	start := time.Unix(100, 0)
	return map[string][]byte{
		"action_tap.bin": bytes.Join([][]byte{
			encodeTestEvent(start, evTypeKey, KeyAction, 1),
			encodeTestEvent(start.Add(250*time.Millisecond), evTypeKey, KeyAction, 0),
		}, nil),
	}
}

func TestFixtures_MatchGenerator(t *testing.T) {
	for name, want := range buttonFixtures() {
		got, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "buttons", name)) //nolint:gosec // Generated fixture name is fixed by the test.
		require.NoError(t, err)
		assert.Equal(t, want, got, name)
	}
}

func TestFixtures_Regenerate(t *testing.T) {
	if !*updateFixtures {
		t.Skip("pass -update-fixtures to rewrite fixtures")
	}
	dir := filepath.Join("..", "..", "..", "testdata", "buttons")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	for name, data := range buttonFixtures() {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
}
