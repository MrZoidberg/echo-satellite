package wake

import (
	"errors"
	"fmt"
)

var (
	ErrStepLength       = errors.New("wake step has wrong length")
	ErrUnknownModelKind = errors.New("unknown wake model kind")
)

// Kind identifies a locally executable wake-model format.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindOpenWakeWord
	KindMicroWakeWord
)

func (k Kind) String() string {
	switch k {
	case KindOpenWakeWord:
		return "openwakeword"
	case KindMicroWakeWord:
		return "microwakeword"
	default:
		return "unknown"
	}
}

// ParseKind parses the stable model-kind strings used by metadata and configuration.
func ParseKind(value string) (Kind, error) {
	switch value {
	case "openwakeword":
		return KindOpenWakeWord, nil
	case "microwakeword":
		return KindMicroWakeWord, nil
	default:
		return KindUnknown, fmt.Errorf("%w: %q", ErrUnknownModelKind, value)
	}
}

// Engine scores one canonical 80 ms PCM step. Pipeline holds a slice of engines, so adding
// simultaneously active models later does not require changing this contract; microWakeWord will
// implement the same interface.
type Engine interface {
	ID() string
	Kind() Kind
	Score(step []int16) (float64, error)
	Reset()
	Close() error
}
