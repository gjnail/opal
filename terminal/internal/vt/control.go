package vt

// execute handles C0 control characters.
func (t *Terminal) execute(b byte) {
	t.lastValid = false
	switch b {
	case 0x07: // BEL
		t.emit(EvBell{})
	case 0x08: // BS
		t.backspace()
	case 0x09: // HT
		t.tabForward(1)
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		t.linefeed()
	case 0x0d: // CR
		t.carriageReturn()
	case 0x0e: // SO: G1 into GL
		t.cur.gl = 1
	case 0x0f: // SI: G0 into GL
		t.cur.gl = 0
	case 0x05: // ENQ: empty answerback
	}
}

// escDispatch handles ESC sequences that aren't CSI, OSC, DCS or strings.
func (t *Terminal) escDispatch(inter []byte, final byte) {
	t.lastValid = false
	if len(inter) == 0 {
		switch final {
		case '7':
			t.saveCursor()
		case '8':
			t.restoreCursor()
		case 'D':
			t.index()
		case 'E':
			t.index()
			t.carriageReturn()
		case 'H':
			if t.cur.x < len(t.tabs) {
				t.tabs[t.cur.x] = true
			}
		case 'M':
			t.reverseIndex()
		case 'N':
			t.cur.singleShift = 2
		case 'O':
			t.cur.singleShift = 3
		case 'n':
			t.cur.gl = 2
		case 'o':
			t.cur.gl = 3
		case 'c':
			t.fullReset()
		case '=':
			t.modes.set(modeKeypadApp, true)
		case '>':
			t.modes.set(modeKeypadApp, false)
		case '\\':
			// ST on its own: nothing to terminate.
		}
		return
	}
	switch inter[0] {
	case '(', ')', '*', '+':
		g := int(inter[0] - '(')
		var cs charset
		switch final {
		case '0':
			cs = charsetDECSpecial
		case 'A':
			cs = charsetUK
		case '>':
			cs = charsetDECTechnical
		default:
			cs = charsetASCII
		}
		t.cur.charsets[g] = cs
	case '#':
		switch final {
		case '8':
			t.alignmentTest()
		case '3':
			t.line(t.cur.y).Attr = LineDoubleTop
			t.seq++
		case '4':
			t.line(t.cur.y).Attr = LineDoubleBottom
			t.seq++
		case '5':
			t.line(t.cur.y).Attr = LineNormal
			t.seq++
		case '6':
			t.line(t.cur.y).Attr = LineDoubleWidth
			t.seq++
		}
	case '%':
		// Character set selection (UTF-8 vs ISO 2022): we're always UTF-8.
	case ' ':
		// S7C1T/S8C1T: we only ever send 7-bit controls.
	}
}
