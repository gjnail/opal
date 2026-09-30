//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleWindow           = kernel32.NewProc("GetConsoleWindow")
)

// consoleInfo is CONSOLE_SCREEN_BUFFER_INFO.
type consoleInfo struct {
	size, cursor             [2]int16
	attributes               uint16
	left, top, right, bottom int16
	maxSize                  [2]int16
}

// TermWidth returns the console's width in cells, or 0 when no standard
// stream is a console (mintty, pipes).
func TermWidth() int {
	for _, h := range []syscall.Handle{syscall.Stdout, syscall.Stderr} {
		var ci consoleInfo
		if ok, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&ci))); ok != 0 {
			return int(ci.right-ci.left) + 1
		}
	}
	return 0
}

// ttyID is the console window: each Windows Terminal tab or pane and each
// conhost window has its own, and shells nested inside one share it.
func ttyID() string {
	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		return fmt.Sprintf("con:%x", hwnd)
	}
	return ""
}
