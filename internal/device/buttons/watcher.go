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
	allowed    map[Key]struct{}
}

type timerDue struct {
	key        Key
	generation uint64
}

func NewWatcher(r io.ReadCloser, allowed ...Key) *Watcher {
	filter := make(map[Key]struct{}, len(allowed))
	for _, key := range allowed {
		filter[key] = struct{}{}
	}
	return &Watcher{reader: NewReader(r), stream: r, allowed: filter}
}

// Run decodes key events until EOF or cancellation. It owns and closes the
// stream so cancellation interrupts an in-flight device read.
func (w *Watcher) Run(ctx context.Context, out chan<- Press) error {
	events := make(chan rawEvent)
	readErr := make(chan error, 1)
	go w.readEvents(ctx, events, readErr)
	defer func() { _ = w.stream.Close() }()
	holdDue := make(chan timerDue, 2)
	repeatDue := make(chan timerDue, 2)
	timers := make(map[Key]*time.Timer)
	repeatHeld := make(map[Key]time.Duration)
	generations := make(map[Key]uint64)
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
		case due := <-holdDue:
			if !w.handleHold(ctx, out, timers, generations, due) {
				return nil
			}
		case due := <-repeatDue:
			if !w.handleRepeat(ctx, out, repeatDue, timers, repeatHeld, generations, due) {
				return nil
			}
		case event := <-events:
			if !w.handleEvent(ctx, out, holdDue, repeatDue, timers, repeatHeld, generations, event) {
				return nil
			}
		}
	}
}

func (w *Watcher) handleHold(
	ctx context.Context,
	out chan<- Press,
	timers map[Key]*time.Timer,
	generations map[Key]uint64,
	due timerDue,
) bool {
	if generations[due.key] != due.generation {
		return true
	}
	delete(timers, due.key)
	return sendPresses(ctx, out, w.recognizer.startHold(due.key))
}

func (w *Watcher) handleRepeat(
	ctx context.Context,
	out chan<- Press,
	repeatDue chan<- timerDue,
	timers map[Key]*time.Timer,
	repeatHeld map[Key]time.Duration,
	generations map[Key]uint64,
	due timerDue,
) bool {
	if generations[due.key] != due.generation {
		return true
	}
	delete(timers, due.key)
	repeatHeld[due.key] += RepeatInterval
	presses := w.recognizer.repeat(due.key, repeatHeld[due.key])
	if !sendPresses(ctx, out, presses) {
		return false
	}
	if len(presses) != 0 {
		timers[due.key] = repeatTimer(ctx, repeatDue, due)
	}
	return true
}

func (w *Watcher) handleEvent(
	ctx context.Context,
	out chan<- Press,
	holdDue, repeatDue chan<- timerDue,
	timers map[Key]*time.Timer,
	repeatHeld map[Key]time.Duration,
	generations map[Key]uint64,
	event rawEvent,
) bool {
	if event.type_ != evTypeKey {
		return true
	}
	key := Key(event.code)
	if len(w.allowed) != 0 {
		if _, ok := w.allowed[key]; !ok {
			return true
		}
	}
	if event.value == 0 {
		generations[key]++
		stopTimer(timers, key)
		delete(repeatHeld, key)
	}
	if event.value == 2 && (key == KeyVolumeDown || key == KeyVolumeUp) {
		generations[key]++
		stopTimer(timers, key)
		delete(repeatHeld, key)
	}
	if !sendPresses(ctx, out, w.recognizer.Feed(key, event.value, event.at)) {
		return false
	}
	if event.value == 1 && (key == KeyAction || key == KeyMute) {
		generations[key]++
		due := timerDue{key: key, generation: generations[key]}
		stopTimer(timers, key)
		timers[key] = time.AfterFunc(HoldThreshold, func() {
			select {
			case holdDue <- due:
			case <-ctx.Done():
			}
		})
	}
	if event.value == 1 && (key == KeyVolumeDown || key == KeyVolumeUp) {
		generations[key]++
		due := timerDue{key: key, generation: generations[key]}
		stopTimer(timers, key)
		repeatHeld[key] = 0
		timers[key] = repeatTimer(ctx, repeatDue, due)
	}
	return true
}

func repeatTimer(ctx context.Context, due chan<- timerDue, event timerDue) *time.Timer {
	return time.AfterFunc(RepeatInterval, func() {
		select {
		case due <- event:
		case <-ctx.Done():
		}
	})
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
