package ui

import (
	"unsafe"

	"gioui.org/app"
	"golang.org/x/sys/windows"
)

var procFlashWindowEx = windows.NewLazySystemDLL("user32.dll").NewProc("FlashWindowEx")

type flashWInfo struct {
	size    uint32
	hwnd    uintptr
	flags   uint32
	count   uint32
	timeout uint32
}

const (
	flashwTray      = 0x2
	flashwTimerNoFG = 0xc // keep flashing until the window comes to the front
)

// nativeWindow remembers the HWND from Gio's view event.
func (w *Window) nativeWindow(e any) {
	if v, ok := e.(app.Win32ViewEvent); ok {
		w.hwnd = v.HWND
	}
}

// requestAttention flashes the taskbar button until the user switches to
// the window.
func (w *Window) requestAttention() {
	if w.hwnd == 0 || w.focused {
		return
	}
	fi := flashWInfo{hwnd: w.hwnd, flags: flashwTray | flashwTimerNoFG}
	fi.size = uint32(unsafe.Sizeof(fi))
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

var procMessageBeep = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBeep")

// beep plays the system's default sound, for bell = "sound".
func (w *Window) beep() { procMessageBeep.Call(0) }
