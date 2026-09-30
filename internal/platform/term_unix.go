//go:build linux || darwin || freebsd || netbsd || dragonfly

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

// winsize is struct winsize from <sys/ioctl.h>.
type winsize struct{ rows, cols, xpixel, ypixel uint16 }

// winSize asks the terminal on fd for its size; ok is false when fd isn't a terminal.
func winSize(fd int) (winsize, bool) {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	return ws, errno == 0
}

// TermWidth returns the terminal's width in cells, or 0 when no standard
// stream is a terminal.
func TermWidth() int {
	for _, fd := range []int{1, 2, 0} {
		if ws, ok := winSize(fd); ok && ws.cols > 0 {
			return int(ws.cols)
		}
	}
	return 0
}

// ttyID is the terminal's device number: every window, tab, pane and ssh
// session gets its own pty, and shells nested inside one share it.
func ttyID() string {
	for _, fd := range []int{0, 2, 1} {
		if _, ok := winSize(fd); !ok {
			continue
		}
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil {
			return fmt.Sprintf("tty:%d", st.Rdev)
		}
	}
	return ""
}
