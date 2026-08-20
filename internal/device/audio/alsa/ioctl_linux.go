//go:build linux

package alsa

// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

const xferSize = 24

var (
	ioctlHWParams = ioctlNumber(3, 0x11, hwParamsSize)
	ioctlPrepare  = ioctlNumber(0, 0x40, 0)
	ioctlStart    = ioctlNumber(0, 0x42, 0)
	ioctlDrop     = ioctlNumber(0, 0x43, 0)
	ioctlWriteI   = ioctlNumber(1, 0x50, xferSize)
	ioctlReadI    = ioctlNumber(2, 0x51, xferSize)
)

func ioctlNumber(direction, number, size uintptr) uintptr {
	return direction<<30 | size<<16 | uintptr('A')<<8 | number
}

func ioctl(fd, request uintptr, argument unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(argument))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlNoArg(fd, request uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
