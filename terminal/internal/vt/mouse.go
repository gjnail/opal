package vt

import (
	"fmt"
	"unicode/utf8"
)

// MouseButton numbers follow xterm's button codes before modifiers.
type MouseButton int

const (
	MouseLeft MouseButton = iota
	MouseMiddle
	MouseRight
	MouseNoButton // motion with nothing held
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
	MouseBack
	MouseForward
)

// MouseAction says what happened.
type MouseAction int

const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// MouseEvent is a mouse event over the grid. X and Y are 0-based cells;
// PX and PY are pixels from the top-left of the grid.
type MouseEvent struct {
	Button MouseButton
	Action MouseAction
	Mods   Mods
	X, Y   int
	PX, PY int
}

func (b MouseButton) code() int {
	switch b {
	case MouseLeft, MouseMiddle, MouseRight:
		return int(b)
	case MouseNoButton:
		return 3
	case MouseWheelUp, MouseWheelDown, MouseWheelLeft, MouseWheelRight:
		return 64 + int(b-MouseWheelUp)
	case MouseBack, MouseForward:
		return 128 + int(b-MouseBack)
	}
	return 3
}

func (b MouseButton) wheel() bool { return b >= MouseWheelUp && b <= MouseWheelRight }

// EncodeMouse returns the report for ev, or nil when the program didn't
// ask for this kind of event.
func (t *Terminal) EncodeMouse(ev MouseEvent) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	mode := t.MouseMode()
	switch mode {
	case MouseTrackNone:
		return nil
	case MouseTrackX10:
		if ev.Action != MousePress {
			return nil
		}
		ev.Mods = 0
	case MouseTrackNormal:
		if ev.Action == MouseMotion {
			return nil
		}
	case MouseTrackButton:
		if ev.Action == MouseMotion && ev.Button == MouseNoButton {
			return nil
		}
	}
	if ev.Button.wheel() && ev.Action == MouseRelease {
		return nil
	}

	code := ev.Button.code()
	if ev.Mods&ModShift != 0 {
		code += 4
	}
	if ev.Mods&ModAlt != 0 {
		code += 8
	}
	if ev.Mods&ModCtrl != 0 {
		code += 16
	}
	if ev.Action == MouseMotion {
		code += 32
	}

	x, y := ev.X+1, ev.Y+1
	switch {
	case t.modes.get(modeMouseSGRPixels):
		return sgrMouse(code, ev.PX+1, ev.PY+1, ev.Action == MouseRelease)
	case t.modes.get(modeMouseSGR):
		return sgrMouse(code, x, y, ev.Action == MouseRelease)
	case t.modes.get(modeMouseURXVT):
		if ev.Action == MouseRelease {
			code = 3 | (code &^ 3 & 0x1c)
		}
		return []byte(fmt.Sprintf("\x1b[%d;%d;%dM", code+32, x, y))
	}
	// X10-style byte encoding: releases don't say which button.
	if ev.Action == MouseRelease {
		code = 3 | (code & 0x1c)
	}
	if t.modes.get(modeMouseUTF8) {
		if x > 2015 || y > 2015 {
			return nil
		}
		b := []byte("\x1b[M")
		b = utf8.AppendRune(b, rune(code+32))
		b = utf8.AppendRune(b, rune(x+32))
		b = utf8.AppendRune(b, rune(y+32))
		return b
	}
	if x > 223 || y > 223 {
		return nil // can't be encoded
	}
	return []byte{0x1b, '[', 'M', byte(code + 32), byte(x + 32), byte(y + 32)}
}

func sgrMouse(code, x, y int, release bool) []byte {
	final := 'M'
	if release {
		final = 'm'
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", code, x, y, final))
}

// AlternateScroll reports whether wheel events should become arrow keys:
// the alternate screen is up, mode 1007 is on and the program isn't
// tracking the mouse itself.
func (t *Terminal) AlternateScroll() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf == t.alt && t.modes.get(modeAltScroll) && t.MouseMode() == MouseTrackNone
}

// MouseTracking reports whether the program wants mouse events, taking
// the terminal's lock (MouseMode assumes it is already held).
func (t *Terminal) MouseTracking() MouseTracking {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.MouseMode()
}

// CursorKeysApp reports DECCKM, for synthesizing arrow keys.
func (t *Terminal) CursorKeysApp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.modes.get(modeCursorKeys)
}
