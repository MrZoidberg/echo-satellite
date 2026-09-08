// Package logging configures the bounded, safe operational logs shared by the
// command binaries. Callers must keep event attributes to an explicit safe
// metadata allowlist; this package deliberately has no payload formatter.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/MrZoidberg/echo-satellite/internal/device/system"
)

const (
	FormatText = "text"
	FormatJSON = "json"
)

// Options is the common command logging configuration.
type Options struct {
	Format   string
	File     string
	MaxBytes int64
	Debug    bool
}

// Configure installs the default logger and returns its cleanup function.
// When File is configured each record is written to stderr and to the bounded
// rotating file, so a failed disk sink never hides diagnostics from an
// interactive operator.
func Configure(opts Options) (func() error, error) {
	if opts.Format == "" {
		opts.Format = FormatText
	}
	var writer io.Writer = os.Stderr
	closeLog := func() error { return nil }
	if opts.File != "" {
		if opts.MaxBytes < 3 {
			return nil, fmt.Errorf("configure logging: max bytes must be at least %d", 3)
		}
		rotating, err := system.NewRotatingWriter(opts.File, opts.MaxBytes, 3)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		writer = io.MultiWriter(os.Stderr, rotating)
		closeLog = rotating.Close
	}
	level := slog.LevelInfo
	if opts.Debug {
		level = slog.LevelDebug
	}
	handlerOpts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch opts.Format {
	case FormatText:
		handler = slog.NewTextHandler(writer, handlerOpts)
	case FormatJSON:
		handler = slog.NewJSONHandler(writer, handlerOpts)
	default:
		_ = closeLog()
		return nil, fmt.Errorf("configure logging: unknown format %q", opts.Format)
	}
	slog.SetDefault(slog.New(handler))
	return closeLog, nil
}
