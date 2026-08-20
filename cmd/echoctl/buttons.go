package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/buttons"
)

func buttonsTest(w io.Writer, command buttonsTestCommand) error {
	if command.Seconds <= 0 {
		return errors.New("seconds must be greater than zero")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(command.Seconds*float64(time.Second)))
	defer cancel()
	if command.FromFile != "" {
		file, err := os.Open(command.FromFile)
		if err != nil {
			return fmt.Errorf("open button event file: %w", err)
		}
		defer func() { _ = file.Close() }()
		return watchButtons(ctx, w, []buttonReader{{ReadCloser: file}})
	}
	devices, err := buttons.FindControlDevices(command.InputDir, command.SysClassDir)
	if err != nil {
		return fmt.Errorf("discover button devices: %w", err)
	}
	readers := make([]buttonReader, 0, len(devices))
	for _, device := range devices {
		file, openErr := os.Open(device.Path)
		if openErr != nil {
			closeReaders(readers)
			return fmt.Errorf("open input device %s: %w", device.Path, openErr)
		}
		readers = append(readers, buttonReader{ReadCloser: file, keys: device.Keys})
	}
	defer closeReaders(readers)
	return watchButtons(ctx, w, readers)
}

type buttonReader struct {
	io.ReadCloser
	keys []buttons.Key
}

func watchButtons(ctx context.Context, w io.Writer, readers []buttonReader) error {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	presses := make(chan buttons.Press)
	results := make(chan error, len(readers))
	for _, reader := range readers {
		go func() { results <- buttons.NewWatcher(reader, reader.keys...).Run(watchCtx, presses) }()
	}
	remaining := len(readers)
	var firstErr error
	ctxDone := ctx.Done()
	for remaining > 0 {
		select {
		case press := <-presses:
			if _, err := fmt.Fprintf(w, "%s %s held=%s\n", press.Key, press.Action, press.Held.Round(time.Millisecond)); err != nil {
				cancel()
				closeReaders(readers)
				return fmt.Errorf("write button report: %w", err)
			}
		case err := <-results:
			remaining--
			if err != nil && firstErr == nil {
				firstErr = err
				cancel()
				closeReaders(readers)
			}
		case <-ctxDone:
			cancel()
			closeReaders(readers)
			ctxDone = nil
		}
	}
	if firstErr != nil {
		return fmt.Errorf("watch buttons: %w", firstErr)
	}
	return nil
}

func closeReaders(readers []buttonReader) {
	for _, reader := range readers {
		_ = reader.Close()
	}
}
