package vt

import "testing"

func TestLegacyKeys(t *testing.T) {
	term := newTerm(80, 24)
	cases := []struct {
		ev   KeyEvent
		want string
	}{
		{KeyEvent{Key: KeyChar, Rune: 'a', Text: "a"}, "a"},
		{KeyEvent{Key: KeyChar, Rune: 'a', Text: "A", Mods: ModShift}, "A"},
		{KeyEvent{Key: KeyChar, Rune: 'c', Mods: ModCtrl}, "\x03"},
		{KeyEvent{Key: KeyChar, Rune: 'b', Text: "b", Mods: ModAlt}, "\x1bb"},
		{KeyEvent{Key: KeyChar, Rune: 'b', Text: "B", Mods: ModAlt | ModShift}, "\x1bB"},
		{KeyEvent{Key: KeyChar, Rune: '[', Mods: ModCtrl}, "\x1b"},
		{KeyEvent{Key: KeyChar, Rune: ' ', Mods: ModCtrl}, "\x00"},
		{KeyEvent{Key: KeyEnter}, "\r"},
		{KeyEvent{Key: KeyTab, Mods: ModShift}, "\x1b[Z"},
		{KeyEvent{Key: KeyBackspace}, "\x7f"},
		{KeyEvent{Key: KeyBackspace, Mods: ModCtrl}, "\x08"},
		{KeyEvent{Key: KeyBackspace, Mods: ModAlt}, "\x1b\x7f"},
		{KeyEvent{Key: KeyUp}, "\x1b[A"},
		{KeyEvent{Key: KeyLeft, Mods: ModCtrl}, "\x1b[1;5D"},
		{KeyEvent{Key: KeyHome, Mods: ModShift}, "\x1b[1;2H"},
		{KeyEvent{Key: KeyDelete}, "\x1b[3~"},
		{KeyEvent{Key: KeyPageUp, Mods: ModAlt}, "\x1b[5;3~"},
		{KeyEvent{Key: KeyF1}, "\x1bOP"},
		{KeyEvent{Key: KeyF5}, "\x1b[15~"},
		{KeyEvent{Key: KeyF12, Mods: ModCtrl | ModShift}, "\x1b[24;6~"},
		{KeyEvent{Key: KeyEnter, Action: KeyRelease}, ""},
	}
	for _, c := range cases {
		if got := string(term.EncodeKey(c.ev)); got != c.want {
			t.Errorf("%+v → %q, want %q", c.ev, got, c.want)
		}
	}
	term.WriteString("\x1b[?1h")
	if got := string(term.EncodeKey(KeyEvent{Key: KeyUp})); got != "\x1bOA" {
		t.Errorf("DECCKM up = %q", got)
	}
	term.WriteString("\x1b[>4;2m")
	if got := string(term.EncodeKey(KeyEvent{Key: KeyChar, Rune: '1', Text: "1", Mods: ModCtrl})); got != "\x1b[27;5;49~" {
		t.Errorf("modifyOtherKeys ctrl+1 = %q", got)
	}
}

func TestKittyKeys(t *testing.T) {
	term := newTerm(80, 24)
	term.WriteString("\x1b[>1u")
	cases := []struct {
		ev   KeyEvent
		want string
	}{
		{KeyEvent{Key: KeyChar, Rune: 'a', Text: "a"}, "a"},
		{KeyEvent{Key: KeyChar, Rune: 'c', Mods: ModCtrl}, "\x1b[99;5u"},
		{KeyEvent{Key: KeyEscape}, "\x1b[27u"},
		{KeyEvent{Key: KeyEnter}, "\r"},
		{KeyEvent{Key: KeyEnter, Mods: ModShift}, "\x1b[13;2u"},
		{KeyEvent{Key: KeyUp}, "\x1b[A"},
		{KeyEvent{Key: KeyUp, Mods: ModCtrl}, "\x1b[1;5A"},
		{KeyEvent{Key: KeyDelete}, "\x1b[3~"},
		{KeyEvent{Key: KeyF3}, "\x1b[13~"},
	}
	for _, c := range cases {
		if got := string(term.EncodeKey(c.ev)); got != c.want {
			t.Errorf("%+v → %q, want %q", c.ev, got, c.want)
		}
	}
	// Flags 1|2|8|16: everything is reported, with events and text.
	term.WriteString("\x1b[=27u")
	cases = []struct {
		ev   KeyEvent
		want string
	}{
		{KeyEvent{Key: KeyChar, Rune: 'a', Text: "a"}, "\x1b[97;1;97u"},
		{KeyEvent{Key: KeyChar, Rune: 'a', Text: "a", Action: KeyRepeat}, "\x1b[97;1:2;97u"},
		{KeyEvent{Key: KeyChar, Rune: 'a', Action: KeyRelease}, "\x1b[97;1:3u"},
		{KeyEvent{Key: KeyEnter}, "\x1b[13u"},
	}
	for _, c := range cases {
		if got := string(term.EncodeKey(c.ev)); got != c.want {
			t.Errorf("flags 27: %+v → %q, want %q", c.ev, got, c.want)
		}
	}
}

func TestMouseEncoding(t *testing.T) {
	term := newTerm(80, 24)
	press := MouseEvent{Button: MouseLeft, Action: MousePress, X: 9, Y: 4}
	if term.EncodeMouse(press) != nil {
		t.Fatal("no tracking: nothing should be sent")
	}
	term.WriteString("\x1b[?1000h")
	if got := string(term.EncodeMouse(press)); got != "\x1b[M *%" {
		t.Errorf("default press = %q", got)
	}
	if got := string(term.EncodeMouse(MouseEvent{Button: MouseLeft, Action: MouseRelease, X: 9, Y: 4})); got != "\x1b[M#*%" {
		t.Errorf("default release = %q", got)
	}
	if term.EncodeMouse(MouseEvent{Button: MouseLeft, Action: MouseMotion, X: 1, Y: 1}) != nil {
		t.Error("1000 shouldn't report motion")
	}
	term.WriteString("\x1b[?1006h\x1b[?1002h")
	if got := string(term.EncodeMouse(MouseEvent{Button: MouseRight, Action: MouseRelease, X: 0, Y: 0, Mods: ModCtrl})); got != "\x1b[<18;1;1m" {
		t.Errorf("sgr release = %q", got)
	}
	if got := string(term.EncodeMouse(MouseEvent{Button: MouseLeft, Action: MouseMotion, X: 2, Y: 3})); got != "\x1b[<32;3;4M" {
		t.Errorf("sgr drag = %q", got)
	}
	if got := string(term.EncodeMouse(MouseEvent{Button: MouseWheelDown, Action: MousePress, X: 0, Y: 0})); got != "\x1b[<65;1;1M" {
		t.Errorf("sgr wheel = %q", got)
	}
}

func TestPasteAndFocus(t *testing.T) {
	term := newTerm(80, 24)
	if got := string(term.Paste("a\nb")); got != "a\rb" {
		t.Errorf("plain paste = %q", got)
	}
	term.WriteString("\x1b[?2004h")
	if got := string(term.Paste("x\x1b[201~rm -rf /\n")); got != "\x1b[200~xrm -rf /\r\x1b[201~" {
		t.Errorf("bracketed paste = %q", got)
	}
	if term.FocusReport(true) != nil {
		t.Error("focus events are off by default")
	}
	term.WriteString("\x1b[?1004h")
	if got := string(term.FocusReport(false)); got != "\x1b[O" {
		t.Errorf("focus out = %q", got)
	}
}
