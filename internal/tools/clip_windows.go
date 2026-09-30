//go:build windows

package tools

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Direct Win32 clipboard access: instant, Unicode-correct, and no PowerShell
// process to spin up.
var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	getClipboardData = user32.NewProc("GetClipboardData")
	setClipboardData = user32.NewProc("SetClipboardData")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalFree       = kernel32.NewProc("GlobalFree")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	lstrlenW         = kernel32.NewProc("lstrlenW")
	moveMemory       = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

func openClip() error {
	// Another app may hold the clipboard briefly; retry for up to a second.
	deadline := time.Now().Add(time.Second)
	for {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("clipboard is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func winCopy(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClip(); err != nil {
		return err
	}
	defer closeClipboard.Call()
	if r, _, err := emptyClipboard.Call(); r == 0 {
		return err
	}
	data := append(utf16.Encode([]rune(text)), 0)
	size := uintptr(len(data) * 2)
	h, _, err := globalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return err
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return err
	}
	moveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), size)
	globalUnlock.Call(h)
	if r, _, err := setClipboardData.Call(cfUnicodeText, h); r == 0 {
		globalFree.Call(h)
		return err
	}
	return nil
}

func winPaste() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClip(); err != nil {
		return "", err
	}
	defer closeClipboard.Call()
	h, _, _ := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // empty, or not text
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		return "", err
	}
	defer globalUnlock.Call(h)
	n, _, _ := lstrlenW.Call(p)
	if n == 0 {
		return "", nil
	}
	buf := make([]uint16, n)
	moveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, n*2)
	return string(utf16.Decode(buf)), nil
}
