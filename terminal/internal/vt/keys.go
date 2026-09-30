package vt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Key identifies a non-text key. Text keys use KeyChar with Rune set.
type Key int

const (
	KeyChar Key = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyInsert
	KeyDelete
	KeyPageUp
	KeyPageDown
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyKPEnter
)

// Mods are keyboard modifiers.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// KeyAction distinguishes presses, repeats and releases (kitty flag 2).
type KeyAction uint8

const (
	KeyPress KeyAction = iota
	KeyRepeat
	KeyRelease
)

// KeyEvent is a key the user pressed.
type KeyEvent struct {
	Key Key
	// Rune is the key's unshifted character for KeyChar ('a' for both a
	// and A), used for control codes and kitty key numbers.
	Rune rune
	// Text is what the key types with its modifiers applied ("A" for
	// shift+a), if anything.
	Text   string
	Mods   Mods
	Action KeyAction
}

const (
	kittyDisambiguate = 1
	kittyEvents       = 2
	kittyAlternates   = 4
	kittyAllKeys      = 8
	kittyText         = 16
)

// EncodeKey returns the bytes a key sends to the program, honoring cursor
// key mode, keypad mode, modifyOtherKeys and the kitty keyboard protocol.
// It returns nil for events that send nothing (releases without kitty
// flag 2).
func (t *Terminal) EncodeKey(ev KeyEvent) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if flags := t.kittyFlags(); flags != 0 {
		return t.encodeKitty(ev, flags)
	}
	if ev.Action == KeyRelease {
		return nil
	}
	return []byte(t.encodeLegacy(ev))
}

// modParam is xterm's modifier parameter: 1 + shift + 2*alt + 4*ctrl + 8*super.
func modParam(m Mods) int {
	p := 1
	if m&ModShift != 0 {
		p++
	}
	if m&ModAlt != 0 {
		p += 2
	}
	if m&ModCtrl != 0 {
		p += 4
	}
	if m&ModSuper != 0 {
		p += 8
	}
	return p
}

// csiKey formats a key that uses a final letter (arrows, Home, F1-F4).
func csiKey(final byte, mods Mods, ss3 bool) string {
	if mods != 0 {
		return fmt.Sprintf("\x1b[1;%d%c", modParam(mods), final)
	}
	if ss3 {
		return "\x1bO" + string(final)
	}
	return "\x1b[" + string(final)
}

func tildeKey(n int, mods Mods) string {
	if mods != 0 {
		return fmt.Sprintf("\x1b[%d;%d~", n, modParam(mods))
	}
	return fmt.Sprintf("\x1b[%d~", n)
}

var fTilde = map[Key]int{KeyF5: 15, KeyF6: 17, KeyF7: 18, KeyF8: 19, KeyF9: 20, KeyF10: 21, KeyF11: 23, KeyF12: 24}

func (t *Terminal) encodeLegacy(ev KeyEvent) string {
	m := ev.Mods
	appCursor := t.modes.get(modeCursorKeys)
	alt := ""
	if m&ModAlt != 0 {
		alt = "\x1b"
	}
	switch ev.Key {
	case KeyEnter, KeyKPEnter:
		if ev.Key == KeyKPEnter && t.modes.get(modeKeypadApp) {
			return "\x1bOM"
		}
		if t.modes.get(modeLineFeedNewLine) {
			return alt + "\r\n"
		}
		return alt + "\r"
	case KeyTab:
		if m&ModShift != 0 {
			return "\x1b[Z"
		}
		return alt + "\t"
	case KeyBackspace:
		b := "\x7f"
		if t.modes.get(modeBackarrowBS) {
			b = "\x08"
		}
		if m&ModCtrl != 0 {
			// Ctrl+Backspace is the other byte, which shells map to
			// delete-word.
			if b == "\x7f" {
				b = "\x08"
			} else {
				b = "\x7f"
			}
		}
		return alt + b
	case KeyEscape:
		return alt + "\x1b"
	case KeyUp:
		return csiKey('A', m, appCursor)
	case KeyDown:
		return csiKey('B', m, appCursor)
	case KeyRight:
		return csiKey('C', m, appCursor)
	case KeyLeft:
		return csiKey('D', m, appCursor)
	case KeyHome:
		return csiKey('H', m, appCursor)
	case KeyEnd:
		return csiKey('F', m, appCursor)
	case KeyInsert:
		return tildeKey(2, m)
	case KeyDelete:
		return tildeKey(3, m)
	case KeyPageUp:
		return tildeKey(5, m)
	case KeyPageDown:
		return tildeKey(6, m)
	case KeyF1, KeyF2, KeyF3, KeyF4:
		final := byte('P' + int(ev.Key-KeyF1))
		if m != 0 {
			return fmt.Sprintf("\x1b[1;%d%c", modParam(m), final)
		}
		return "\x1bO" + string(final)
	}
	if n, ok := fTilde[ev.Key]; ok {
		return tildeKey(n, m)
	}

	// Text keys.
	r := ev.Rune
	if m&(ModCtrl|ModAlt|ModSuper) == 0 {
		return ev.Text
	}
	if m&ModCtrl != 0 {
		if c, ok := ctrlByte(r, m&ModShift != 0); ok && (t.modifyOtherKeys < 2 || isClassicCtrl(r, m)) {
			return alt + string(c)
		}
		if t.modifyOtherKeys > 0 {
			return fmt.Sprintf("\x1b[27;%d;%d~", modParam(m), shiftedRune(ev))
		}
		// No control code for this key: send the text, as xterm does.
		if ev.Text != "" {
			return alt + ev.Text
		}
		return ""
	}
	if m&ModAlt != 0 {
		text := ev.Text
		if text == "" && r != 0 {
			text = string(r)
			if m&ModShift != 0 {
				text = strings.ToUpper(text)
			}
		}
		return "\x1b" + text
	}
	return ev.Text
}

func shiftedRune(ev KeyEvent) rune {
	if ev.Mods&ModShift != 0 && ev.Text != "" {
		return []rune(ev.Text)[0]
	}
	return ev.Rune
}

// isClassicCtrl reports keys whose control code is unambiguous enough that
// modifyOtherKeys level 2 still sends it (Ctrl+letter without Shift).
func isClassicCtrl(r rune, m Mods) bool {
	return m&ModShift == 0 && r >= 'a' && r <= 'z'
}

// ctrlByte maps a Ctrl+key to its C0 control code.
func ctrlByte(r rune, shift bool) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r - 'a' + 1), true
	case r >= 'A' && r <= 'Z':
		return byte(r - 'A' + 1), true
	}
	switch r {
	case '@', ' ', '2', '`':
		return 0, true
	case '[', '3':
		return 0x1b, true
	case '\\', '4':
		return 0x1c, true
	case ']', '5':
		return 0x1d, true
	case '^', '6':
		return 0x1e, true
	case '_', '-', '7', '/':
		return 0x1f, true
	case '?', '8':
		return 0x7f, true
	}
	return 0, false
}

// Kitty keyboard protocol key numbers for functional keys.
var kittyFunctional = map[Key]struct {
	num   int
	final byte // 'u', '~', or a letter for legacy-style CSI 1;m X
}{
	KeyEscape: {27, 'u'}, KeyEnter: {13, 'u'}, KeyTab: {9, 'u'}, KeyBackspace: {127, 'u'},
	KeyInsert: {2, '~'}, KeyDelete: {3, '~'}, KeyPageUp: {5, '~'}, KeyPageDown: {6, '~'},
	KeyUp: {1, 'A'}, KeyDown: {1, 'B'}, KeyRight: {1, 'C'}, KeyLeft: {1, 'D'},
	KeyHome: {1, 'H'}, KeyEnd: {1, 'F'},
	KeyF1: {1, 'P'}, KeyF2: {1, 'Q'}, KeyF3: {13, '~'}, KeyF4: {1, 'S'},
	KeyF5: {15, '~'}, KeyF6: {17, '~'}, KeyF7: {18, '~'}, KeyF8: {19, '~'},
	KeyF9: {20, '~'}, KeyF10: {21, '~'}, KeyF11: {23, '~'}, KeyF12: {24, '~'},
	KeyKPEnter: {57414, 'u'},
}

func (t *Terminal) encodeKitty(ev KeyEvent, flags int) []byte {
	m := ev.Mods
	if ev.Action == KeyRelease && flags&kittyEvents == 0 {
		return nil
	}
	all := flags&kittyAllKeys != 0

	// Plain text with no modifiers stays plain unless every key is to be
	// reported (flag 8).
	if ev.Key == KeyChar && !all && m&(ModCtrl|ModAlt|ModSuper) == 0 {
		if ev.Action == KeyRelease {
			return nil
		}
		return []byte(ev.Text)
	}
	// Enter, Tab and Backspace keep their legacy bytes without modifiers
	// so shells that don't know the protocol still work (spec behavior).
	if !all && m == 0 && ev.Action != KeyRelease {
		switch ev.Key {
		case KeyEnter:
			return []byte("\r")
		case KeyTab:
			return []byte("\t")
		case KeyBackspace:
			return []byte("\x7f")
		}
	}

	var num int
	final := byte('u')
	if ev.Key == KeyChar {
		num = int(unicode.ToLower(ev.Rune))
	} else if f, ok := kittyFunctional[ev.Key]; ok {
		num, final = f.num, f.final
	} else {
		return nil
	}

	var b strings.Builder
	b.WriteString("\x1b[")
	keyPart := ""
	if final == 'u' || final == '~' {
		keyPart = strconv.Itoa(num)
		if flags&kittyAlternates != 0 && ev.Key == KeyChar && m&ModShift != 0 {
			if s := shiftedRune(ev); s != 0 && s != ev.Rune {
				keyPart += ":" + strconv.Itoa(int(s))
			}
		}
	} else if num != 1 {
		keyPart = strconv.Itoa(num)
	}

	mp := modParam(m)
	evPart := ""
	switch {
	case flags&kittyEvents != 0 && ev.Action == KeyRepeat:
		evPart = ":2"
	case flags&kittyEvents != 0 && ev.Action == KeyRelease:
		evPart = ":3"
	}
	textPart := ""
	if flags&kittyText != 0 && ev.Text != "" && ev.Action != KeyRelease && m&(ModCtrl|ModAlt|ModSuper) == 0 {
		var cps []string
		for _, r := range ev.Text {
			cps = append(cps, strconv.Itoa(int(r)))
		}
		textPart = strings.Join(cps, ":")
	}

	switch {
	case textPart != "":
		fmt.Fprintf(&b, "%s;%d%s;%s", keyPart, mp, evPart, textPart)
	case mp != 1 || evPart != "":
		if keyPart == "" {
			keyPart = "1"
		}
		fmt.Fprintf(&b, "%s;%d%s", keyPart, mp, evPart)
	default:
		b.WriteString(keyPart)
	}
	b.WriteByte(final)
	return []byte(b.String())
}

// Paste prepares clipboard text for the program: line endings become CR,
// and with bracketed paste on it's wrapped in markers after stripping any
// markers inside it (so pasted text can't end the paste early and run
// commands).
func (t *Terminal) Paste(text string) []byte {
	t.mu.Lock()
	bracketed := t.modes.get(modeBracketedPaste)
	t.mu.Unlock()
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")
	if !bracketed {
		return []byte(text)
	}
	text = strings.ReplaceAll(text, "\x1b[201~", "")
	text = strings.ReplaceAll(text, "\x1b[200~", "")
	return []byte("\x1b[200~" + text + "\x1b[201~")
}

// FocusReport returns the focus in/out sequence if the program asked for
// focus events (mode 1004).
func (t *Terminal) FocusReport(focused bool) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.modes.get(modeFocusEvents) {
		return nil
	}
	if focused {
		return []byte("\x1b[I")
	}
	return []byte("\x1b[O")
}
