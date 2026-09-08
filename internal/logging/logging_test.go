package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigure_TextAndJSON(t *testing.T) {
	for _, format := range []string{FormatText, FormatJSON} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "command.log")
			closeLog, err := Configure(Options{Format: format, File: path, MaxBytes: 3_000, Debug: true})
			require.NoError(t, err)
			slog.Debug("safe lifecycle", "type", "hello", "bytes", 12)
			require.NoError(t, closeLog())
			contents, err := os.ReadFile(path) //nolint:gosec // Test reads a fixed path under its private temporary directory.
			require.NoError(t, err)
			assert.Contains(t, string(contents), "safe lifecycle")
			if format == FormatJSON {
				assert.True(t, bytes.HasPrefix(contents, []byte("{")))
			}
		})
	}
}

func TestConfigure_RejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	_, err := Configure(Options{Format: "xml", MaxBytes: 1})
	require.Error(t, err)
	_, err = Configure(Options{Format: FormatText, File: "command.log", MaxBytes: 2})
	require.Error(t, err)
}

func TestConfigure_AllowsStderrOnlyWithDefaultCapacity(t *testing.T) {
	t.Parallel()

	closeLog, err := Configure(Options{Format: FormatText})
	require.NoError(t, err)
	require.NoError(t, closeLog())
}
