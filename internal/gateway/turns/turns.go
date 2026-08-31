// Package turns validates and receives the device-owned command-audio window.
package turns

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const maxFrameBytes = 64 << 10

var (
	ErrTurnActive     = errors.New("gateway turns: turn already active")
	ErrNoTurn         = errors.New("gateway turns: no active turn")
	ErrAudioNotOpen   = errors.New("gateway turns: audio window is not open")
	ErrInvalidAudio   = errors.New("gateway turns: audio must be mono 16-kHz pcm_s16le")
	ErrFrameTooLarge  = errors.New("gateway turns: frame exceeds 64 KiB")
	ErrUnevenPCMFrame = errors.New("gateway turns: PCM frame must contain whole samples")
)

// Receiver accepts one active device turn. Directory is optional: without it,
// PCM is counted and discarded rather than retained.
type Receiver struct{ Directory string }

// Turn is the completed turn metadata. WAVPath is set only when diagnostics
// were explicitly enabled and audio ended successfully.
type Turn struct {
	ID      string
	Start   protocol.TurnStart
	Stop    protocol.AudioStop
	Bytes   int64
	WAVPath string
	Started time.Time
}

// Active is a mutable receiver for one turn.
type Active struct {
	turn  Turn
	open  bool
	file  *os.File
	stage string
}

// AudioOpen reports whether binary PCM is currently permitted for this turn.
func (a *Active) AudioOpen() bool { return a != nil && a.open }

// Begin starts a turn. A caller owns the returned Active until Stop or Abort.
func (r Receiver) Begin(id string, start protocol.TurnStart, now time.Time) (*Active, error) {
	if strings.TrimSpace(id) == "" || !start.Trigger.Valid() {
		return nil, errors.New("gateway turns: invalid turn start")
	}
	return &Active{turn: Turn{ID: id, Start: start, Started: now}}, nil
}

// StartAudio opens the PCM window with the sole Milestone 2 input format.
func (r Receiver) StartAudio(active *Active, id string, audio protocol.AudioStart) error {
	if active == nil {
		return ErrNoTurn
	}
	if active.open || active.turn.ID != id {
		return ErrTurnActive
	}
	if audio.SampleRate != 16000 || audio.Channels != 1 || audio.Format != protocol.AudioFormatPCMS16LE {
		return ErrInvalidAudio
	}
	active.open = true
	if r.Directory == "" {
		return nil
	}
	if err := os.MkdirAll(r.Directory, 0o750); err != nil {
		return fmt.Errorf("create diagnostic WAV directory: %w", err)
	}
	f, err := os.CreateTemp(r.Directory, ".turn-*.part")
	if err != nil {
		return fmt.Errorf("create staged diagnostic WAV: %w", err)
	}
	if err = writeWAVHeader(f, 0); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	active.file, active.stage = f, f.Name()
	return nil
}

// Write accepts one binary frame. Callers must not buffer binary data outside
// an audio window.
func (r Receiver) Write(active *Active, pcm []byte) error {
	if active == nil {
		return ErrNoTurn
	}
	if !active.open {
		return ErrAudioNotOpen
	}
	if len(pcm) > maxFrameBytes {
		return ErrFrameTooLarge
	}
	if len(pcm)%2 != 0 {
		return ErrUnevenPCMFrame
	}
	if active.file != nil {
		if _, err := active.file.Write(pcm); err != nil {
			return fmt.Errorf("write staged diagnostic WAV: %w", err)
		}
	}
	active.turn.Bytes += int64(len(pcm))
	return nil
}

// Stop closes a valid audio window and atomically publishes optional
// diagnostics. No output is promoted for incomplete or invalid turns.
func (r Receiver) Stop(active *Active, id string, stop protocol.AudioStop) (Turn, error) {
	if active == nil {
		return Turn{}, ErrNoTurn
	}
	if !active.open || active.turn.ID != id {
		return Turn{}, ErrAudioNotOpen
	}
	if err := stop.Validate(); err != nil {
		return Turn{}, fmt.Errorf("validate audio stop: %w", err)
	}
	active.open, active.turn.Stop = false, stop
	if active.file == nil {
		return active.turn, nil
	}
	if err := finalizeWAV(active.file, active.turn.Bytes); err != nil {
		active.Abort()
		return Turn{}, err
	}
	active.file = nil
	final := filepath.Join(r.Directory, diagnosticName(active.turn.ID))
	// Link first: unlike Rename, it never replaces an existing diagnostic if a
	// random-name collision occurs. Both paths are in one directory, so this is
	// an atomic publish on the supported local filesystems.
	if err := os.Link(active.stage, final); err != nil {
		_ = os.Remove(active.stage)
		return Turn{}, fmt.Errorf("promote diagnostic WAV: %w", err)
	}
	if err := os.Remove(active.stage); err != nil {
		return Turn{}, fmt.Errorf("remove staged diagnostic WAV: %w", err)
	}
	if err := syncDirectory(r.Directory); err != nil {
		return Turn{}, err
	}
	active.stage, active.turn.WAVPath = "", final
	return active.turn, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // G304: path is the receiver's operator-configured output directory.
	if err != nil {
		return fmt.Errorf("open diagnostic WAV directory: %w", err)
	}
	if err = directory.Sync(); err != nil {
		_ = directory.Close()
		return fmt.Errorf("sync diagnostic WAV directory: %w", err)
	}
	if err = directory.Close(); err != nil {
		return fmt.Errorf("close diagnostic WAV directory: %w", err)
	}
	return nil
}

// Abort removes staged diagnostic audio for an interrupted or invalid turn.
func (a *Active) Abort() {
	if a == nil {
		return
	}
	if a.file != nil {
		_ = a.file.Close()
		a.file = nil
	}
	if a.stage != "" {
		_ = os.Remove(a.stage)
		a.stage = ""
	}
}

func finalizeWAV(f *os.File, pcmBytes int64) error {
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("seek staged diagnostic WAV: %w", err)
	}
	if err := writeWAVHeader(f, pcmBytes); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync staged diagnostic WAV: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close staged diagnostic WAV: %w", err)
	}
	return nil
}

func writeWAVHeader(f *os.File, pcmBytes int64) error {
	if pcmBytes > int64(^uint32(0))-36 {
		return errors.New("gateway turns: diagnostic WAV is too large")
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(pcmBytes+36)) //nolint:gosec // G115: bounded above by MaxUint32 - 36.
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], 16000)
	binary.LittleEndian.PutUint32(h[28:], 32000)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(pcmBytes)) //nolint:gosec // G115: bounded above by MaxUint32 - 36.
	if _, err := f.Write(h); err != nil {
		return fmt.Errorf("write diagnostic WAV header: %w", err)
	}
	return nil
}

func diagnosticName(id string) string {
	var suffix [8]byte
	_, _ = rand.Read(suffix[:])
	return fmt.Sprintf("turn-%s-%x.wav", strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, id), suffix)
}
