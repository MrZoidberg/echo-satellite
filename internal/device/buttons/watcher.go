package buttons

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type Watcher struct {
	reader     *Reader
	stream     io.ReadCloser
	recognizer Recognizer
}

func NewWatcher(r io.ReadCloser) *Watcher { return &Watcher{reader: NewReader(r), stream: r} }

// Run decodes key events until EOF or cancellation. It owns and closes the
// stream so cancellation interrupts an in-flight device read.
func (w *Watcher) Run(ctx context.Context, out chan<- Press) error {
	events := make(chan rawEvent)
	readErr := make(chan error, 1)
	go w.readEvents(ctx, events, readErr)
	defer func() { _ = w.stream.Close() }()
	holdDue := make(chan Key, 2)
	timers := make(map[Key]*time.Timer)
	defer stopTimers(timers)
	for {
		select {
		case <-ctx.Done():
			_ = w.stream.Close()
			return nil
		case err := <-readErr:
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read input event: %w", err)
		case key := <-holdDue:
			delete(timers, key)
			if !sendPresses(ctx, out, w.recognizer.startHold(key)) {
				return nil
			}
		case event := <-events:
			if event.type_ != evTypeKey {
				continue
			}
			key := Key(event.code)
			if event.value == 0 {
				stopTimer(timers, key)
			}
			if !sendPresses(ctx, out, w.recognizer.Feed(key, event.value, event.at)) {
				return nil
			}
			if event.value == 1 && (key == KeyAction || key == KeyMute) {
				stopTimer(timers, key)
				timers[key] = time.AfterFunc(HoldThreshold, func() {
					select {
					case holdDue <- key:
					case <-ctx.Done():
					}
				})
			}
		}
	}
}

func (w *Watcher) readEvents(ctx context.Context, events chan<- rawEvent, readErr chan<- error) {
	for {
		event, err := w.reader.Read()
		if err != nil {
			readErr <- err
			return
		}
		select {
		case events <- event:
		case <-ctx.Done():
			return
		}
	}
}

func sendPresses(ctx context.Context, out chan<- Press, presses []Press) bool {
	for _, press := range presses {
		select {
		case out <- press:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func stopTimer(timers map[Key]*time.Timer, key Key) {
	if timer, ok := timers[key]; ok {
		timer.Stop()
		delete(timers, key)
	}
}

func stopTimers(timers map[Key]*time.Timer) {
	for _, timer := range timers {
		timer.Stop()
	}
}
