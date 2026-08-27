//go:build linux

package mixer

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	controlNameLength     = 44
	controlTypeBoolean    = 1
	controlTypeEnumerated = 3
)

var (
	ioctlElementInfo  = controlIOCTL(3, 0x11, unsafe.Sizeof(elementInfo{}))
	ioctlElementRead  = controlIOCTL(3, 0x12, unsafe.Sizeof(elementValue{}))
	ioctlElementWrite = controlIOCTL(3, 0x13, unsafe.Sizeof(elementValue{}))
)

type elementID struct {
	NumID     uint32
	Interface int32
	Device    uint32
	Subdevice uint32
	Name      [controlNameLength]byte
	Index     uint32
}

type elementInfo struct {
	ID       elementID
	Type     int32
	Access   uint32
	Count    uint32
	Owner    int32
	Value    [128]byte
	Reserved [64]byte
}

type elementValue struct {
	ID       elementID
	Indirect uint32
	_        uint32
	Value    [1024]byte
	Reserved [128]byte
}

// Mixer controls ALSA mixer elements on one sound card.
type Mixer struct {
	file *os.File
}

// Open opens the ALSA control device for card.
func Open(card int) (*Mixer, error) {
	if card < 0 {
		return nil, fmt.Errorf("open ALSA mixer: invalid card %d", card)
	}
	path := fmt.Sprintf("/dev/snd/controlC%d", card)
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // G304: a validated integer constructs the fixed ALSA device path.
	if err != nil {
		return nil, fmt.Errorf("open ALSA mixer %s: %w", path, err)
	}
	return &Mixer{file: file}, nil
}

// Get reads a named mixer control. A caller that temporarily changes a control
// must save this value and restore it when finished, including on error paths.
func (m *Mixer) Get(name string) (string, error) {
	info, err := m.lookup(name)
	if err != nil {
		return "", err
	}
	value := elementValue{ID: info.ID}
	if err := mixerIOCTL(m.file.Fd(), ioctlElementRead, unsafe.Pointer(&value)); err != nil { //nolint:gosec // G103: ioctl requires the UAPI structure address.
		return "", fmt.Errorf("read ALSA mixer control %q: %w", name, err)
	}
	switch info.Type {
	case controlTypeBoolean:
		if nativeLong(value.Value[:]) == 0 {
			return ValueOff, nil
		}
		return ValueOn, nil
	case controlTypeEnumerated:
		return m.enumeratedName(info, nativeUint32(value.Value[:]))
	default:
		return "", fmt.Errorf("read ALSA mixer control %q: unsupported type %d", name, info.Type)
	}
}

// Set writes a named mixer control. A caller that temporarily changes a
// control must first call Get and restore that prior value when finished.
func (m *Mixer) Set(name, requested string) error {
	info, err := m.lookup(name)
	if err != nil {
		return err
	}
	value := elementValue{ID: info.ID}
	switch info.Type {
	case controlTypeBoolean:
		var enabled uintptr
		switch requested {
		case ValueOff:
		case ValueOn:
			enabled = 1
		default:
			return fmt.Errorf("set ALSA mixer control %q: invalid Boolean value %q", name, requested)
		}
		putNativeLong(value.Value[:], enabled)
	case controlTypeEnumerated:
		item, findErr := m.enumeratedItem(info, requested)
		if findErr != nil {
			return findErr
		}
		putUint32(value.Value[:], item)
	default:
		return fmt.Errorf("set ALSA mixer control %q: unsupported type %d", name, info.Type)
	}
	if err := mixerIOCTL(m.file.Fd(), ioctlElementWrite, unsafe.Pointer(&value)); err != nil { //nolint:gosec // G103: ioctl requires the UAPI structure address.
		return fmt.Errorf("write ALSA mixer control %q: %w", name, err)
	}
	return nil
}

// Close closes the mixer control device.
func (m *Mixer) Close() error {
	if m == nil || m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	if err != nil {
		return fmt.Errorf("close ALSA mixer: %w", err)
	}
	return nil
}

func (m *Mixer) lookup(name string) (elementInfo, error) {
	if m == nil || m.file == nil {
		return elementInfo{}, errors.New("ALSA mixer is closed")
	}
	if name == "" || len(name) >= controlNameLength {
		return elementInfo{}, fmt.Errorf("%w: %q", ErrControlNotFound, name)
	}
	var info elementInfo
	info.ID.Interface = 2 // SNDRV_CTL_ELEM_IFACE_MIXER.
	copy(info.ID.Name[:], name)
	if err := mixerIOCTL(m.file.Fd(), ioctlElementInfo, unsafe.Pointer(&info)); err != nil { //nolint:gosec // G103: ioctl requires the UAPI structure address.
		if errors.Is(err, unix.ENOENT) {
			return elementInfo{}, fmt.Errorf("%w: %q", ErrControlNotFound, name)
		}
		return elementInfo{}, fmt.Errorf("inspect ALSA mixer control %q: %w", name, err)
	}
	return info, nil
}

func (m *Mixer) enumeratedName(info elementInfo, item uint32) (string, error) {
	putUint32(info.Value[4:], item)
	if err := mixerIOCTL(m.file.Fd(), ioctlElementInfo, unsafe.Pointer(&info)); err != nil { //nolint:gosec // G103: ioctl requires the UAPI structure address.
		return "", fmt.Errorf("read ALSA mixer enumeration %q item %d: %w", stringName(info.ID.Name[:]), item, err)
	}
	return stringName(info.Value[8:72]), nil
}

func (m *Mixer) enumeratedItem(info elementInfo, requested string) (uint32, error) {
	items := nativeUint32(info.Value[:])
	for item := range items {
		name, err := m.enumeratedName(info, item)
		if err != nil {
			return 0, err
		}
		if name == requested {
			return item, nil
		}
	}
	return 0, fmt.Errorf("set ALSA mixer control %q: invalid enumerated value %q", stringName(info.ID.Name[:]), requested)
}

func controlIOCTL(direction, number, size uintptr) uintptr {
	return direction<<30 | size<<16 | uintptr('U')<<8 | number
}

func mixerIOCTL(fd, request uintptr, argument unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(argument))
	runtime.KeepAlive(argument)
	if errno != 0 {
		return errno
	}
	return nil
}

func nativeUint32(value []byte) uint32 {
	return *(*uint32)(unsafe.Pointer(&value[0])) //nolint:gosec // G103: decoding an aligned native UAPI field.
}

func putUint32(value []byte, number uint32) {
	*(*uint32)(unsafe.Pointer(&value[0])) = number //nolint:gosec // G103: encoding an aligned native UAPI field.
}

func nativeLong(value []byte) uintptr {
	return *(*uintptr)(unsafe.Pointer(&value[0])) //nolint:gosec // G103: Linux long matches uintptr on supported targets.
}

func putNativeLong(value []byte, number uintptr) {
	*(*uintptr)(unsafe.Pointer(&value[0])) = number //nolint:gosec // G103: Linux long matches uintptr on supported targets.
}

func stringName(value []byte) string {
	for index, character := range value {
		if character == 0 {
			return string(value[:index])
		}
	}
	return string(value)
}
