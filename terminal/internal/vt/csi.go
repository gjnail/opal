package vt

import (
	"fmt"
	"strings"
)

func (t *Terminal) csiDispatch(p *params, private byte, inter []byte, final byte) {
	if final != 'b' {
		t.lastValid = false
	}
	switch {
	case private == 0 && len(inter) == 0:
		t.csiPlain(p, final)
	case private == '?' && len(inter) == 0:
		t.csiDEC(p, final)
	case private == '>' && len(inter) == 0:
		t.csiGT(p, final)
	case private == '<' && len(inter) == 0:
		if final == 'u' {
			t.kittyPop(p.getNZ(0, 1))
		}
	case private == '=' && len(inter) == 0:
		switch final {
		case 'c':
			if p.get(0, 0) == 0 {
				t.reply("\x1bP!|4F50414C\x1b\\") // DA3: unit id "OPAL"
			}
		case 'u':
			t.kittySet(p.get(0, 0), p.get(1, 1))
		}
	case len(inter) == 1:
		t.csiInter(p, private, inter[0], final)
	}
}

func (t *Terminal) csiPlain(p *params, final byte) {
	n := p.getNZ(0, 1)
	switch final {
	case '@':
		t.insertChars(n)
	case 'A':
		t.cursorUp(n)
	case 'B':
		t.cursorDown(n)
	case 'C':
		t.cursorForward(n)
	case 'D':
		t.cursorBack(n)
	case 'E':
		t.cursorDown(n)
		t.carriageReturn()
	case 'F':
		t.cursorUp(n)
		t.carriageReturn()
	case 'G', '`':
		t.setColumn(n - 1)
	case 'H', 'f':
		t.moveTo(p.getNZ(1, 1)-1, p.getNZ(0, 1)-1)
	case 'I':
		t.tabForward(n)
	case 'J':
		t.eraseDisplay(p.get(0, 0), false)
	case 'K':
		t.eraseLine(p.get(0, 0), false)
	case 'L':
		t.insertLines(n)
	case 'M':
		t.deleteLines(n)
	case 'P':
		t.deleteChars(n)
	case 'S':
		t.scrollUp(t.top, t.bottom, n)
	case 'T':
		if p.n <= 1 {
			t.scrollDown(t.top, t.bottom, n)
		}
	case 'X':
		t.eraseChars(n)
	case 'Z':
		t.tabBack(n)
	case 'a':
		t.cursorForward(n)
	case 'b':
		t.repeat(n)
	case 'c':
		if p.get(0, 0) == 0 {
			// VT525-class, with sixel (4), selective erase (6), ANSI color
			// (22), rectangular editing (28) and OSC 52 clipboard (52).
			t.reply("\x1b[?65;1;4;6;22;28;52c")
		}
	case 'd':
		t.setRow(n - 1)
	case 'e':
		t.cursorDown(n)
	case 'g':
		switch p.get(0, 0) {
		case 0:
			if t.cur.x < len(t.tabs) {
				t.tabs[t.cur.x] = false
			}
		case 3:
			clear(t.tabs)
		}
	case 'h', 'l':
		for i := 0; i < p.n; i++ {
			t.setMode(p.get(i, 0), false, final == 'h')
		}
	case 'm':
		t.sgr(p)
	case 'n':
		switch p.get(0, 0) {
		case 5:
			t.reply("\x1b[0n")
		case 6:
			x, y := t.cur.x, t.cur.y
			if t.cur.origin {
				x -= t.left
				y -= t.top
			}
			t.reply(fmt.Sprintf("\x1b[%d;%dR", y+1, x+1))
		}
	case 'r':
		t.setScrollRegion(p.get(0, 0), p.get(1, 0))
	case 's':
		if t.modes.get(modeLRMargins) {
			t.setLRMargins(p.get(0, 0), p.get(1, 0))
		} else {
			t.saveCursor()
		}
	case 't':
		t.windowOps(p)
	case 'u':
		t.restoreCursor()
	case 'x':
		switch p.get(0, 0) {
		case 0:
			t.reply("\x1b[2;1;1;112;112;1;0x")
		case 1:
			t.reply("\x1b[3;1;1;112;112;1;0x")
		}
	}
}

// setColumn implements CHA/HPA: absolute column, origin-relative.
func (t *Terminal) setColumn(x int) {
	if t.cur.origin {
		t.cur.x = clamp(x+t.left, t.left, t.right)
	} else {
		t.cur.x = clamp(x, 0, t.cols-1)
	}
	t.cur.pendingWrap = false
}

// setRow implements VPA.
func (t *Terminal) setRow(y int) {
	if t.cur.origin {
		t.cur.y = clamp(y+t.top, t.top, t.bottom)
	} else {
		t.cur.y = clamp(y, 0, t.rows-1)
	}
	t.cur.pendingWrap = false
}

func (t *Terminal) csiDEC(p *params, final byte) {
	switch final {
	case 'h', 'l':
		for i := 0; i < p.n; i++ {
			t.setMode(p.get(i, 0), true, final == 'h')
		}
	case 'J':
		t.eraseDisplay(p.get(0, 0), true)
	case 'K':
		t.eraseLine(p.get(0, 0), true)
	case 'n':
		switch p.get(0, 0) {
		case 6:
			x, y := t.cur.x, t.cur.y
			if t.cur.origin {
				x -= t.left
				y -= t.top
			}
			t.reply(fmt.Sprintf("\x1b[?%d;%d;1R", y+1, x+1))
		case 15:
			t.reply("\x1b[?13n") // no printer
		case 25:
			t.reply("\x1b[?21n") // UDKs locked
		case 26:
			t.reply("\x1b[?27;1;0;0n") // North American keyboard, ready
		case 996:
			t.reportColorScheme()
		}
	case 's':
		t.saveModes(p)
	case 'r':
		t.restoreModes(p)
	case 'u':
		t.reply(fmt.Sprintf("\x1b[?%du", t.kittyFlags()))
	case 'S':
		t.xtsmgraphics(p)
	case 'W':
		if p.get(0, 0) == 5 {
			t.resetTabs()
		}
	}
}

func (t *Terminal) csiGT(p *params, final byte) {
	switch final {
	case 'c':
		if p.get(0, 0) == 0 {
			t.reply(fmt.Sprintf("\x1b[>1;%d;0c", versionNumber(t.version)))
		}
	case 'q':
		if p.get(0, 0) == 0 {
			t.reply(fmt.Sprintf("\x1bP>|%s %s\x1b\\", t.name, t.version))
		}
	case 'm':
		// XTMODKEYS: only modifyOtherKeys (resource 4) matters to us.
		if p.n == 0 {
			t.modifyOtherKeys = 0
			return
		}
		if p.get(0, 0) == 4 {
			t.modifyOtherKeys = clamp(p.get(1, 0), 0, 2)
		}
	case 'n':
		if p.get(0, -1) == 4 {
			t.modifyOtherKeys = 0
		}
	case 'u':
		t.kittyPush(p.get(0, 0))
	}
}

func (t *Terminal) csiInter(p *params, private, inter, final byte) {
	switch {
	case inter == ' ' && final == 'q':
		t.cursorStyle = CursorStyle(clamp(p.get(0, 0), 0, 6))
		t.seq++
	case inter == '!' && final == 'p':
		t.softReset()
	case inter == '$' && final == 'p':
		mode := p.get(0, 0)
		if private == '?' {
			t.reply(fmt.Sprintf("\x1b[?%d;%d$y", mode, t.modeReport(mode, true)))
		} else {
			t.reply(fmt.Sprintf("\x1b[%d;%d$y", mode, t.modeReport(mode, false)))
		}
	case inter == '"' && final == 'q':
		if p.get(0, 0) == 1 {
			t.cur.pen.attrs |= AttrProtected
		} else {
			t.cur.pen.attrs &^= AttrProtected
		}
	case inter == '"' && final == 'p':
		t.softReset() // DECSCL
	case inter == '$' && final == 'x':
		t.fillRect(p)
	case inter == '$' && final == 'z':
		t.eraseRect(p, false)
	case inter == '$' && final == '{':
		t.eraseRect(p, true)
	case inter == '$' && final == 'v':
		t.copyRect(p)
	case inter == '\'' && final == '}':
		t.insertColumns(p.getNZ(0, 1))
	case inter == '\'' && final == '~':
		t.deleteColumns(p.getNZ(0, 1))
	case inter == ' ' && final == '@':
		t.shiftColumns(-p.getNZ(0, 1))
	case inter == ' ' && final == 'A':
		t.shiftColumns(p.getNZ(0, 1))
	case inter == '#' && final == '{':
		t.pushSGR()
	case inter == '#' && final == '}':
		t.popSGR()
	case inter == '#' && final == 'P':
		t.pushColors()
	case inter == '#' && final == 'Q':
		t.popColors()
	}
}

// windowOps implements the safe parts of XTWINOPS.
func (t *Terminal) windowOps(p *params) {
	switch p.get(0, 0) {
	case 11:
		t.reply("\x1b[1t") // not iconified
	case 13:
		t.reply("\x1b[3;0;0t")
	case 14:
		t.reply(fmt.Sprintf("\x1b[4;%d;%dt", t.rows*t.cellH, t.cols*t.cellW))
	case 16:
		t.reply(fmt.Sprintf("\x1b[6;%d;%dt", t.cellH, t.cellW))
	case 18:
		t.reply(fmt.Sprintf("\x1b[8;%d;%dt", t.rows, t.cols))
	case 19:
		t.reply(fmt.Sprintf("\x1b[9;%d;%dt", t.rows, t.cols))
	case 20, 21:
		// Reporting the title lets anything that can set it (a file you
		// cat) type into your shell, so answer with an empty one.
		if p.get(0, 0) == 20 {
			t.reply("\x1b]L\x1b\\")
		} else {
			t.reply("\x1b]l\x1b\\")
		}
	case 22:
		if len(t.titleStack) < 16 {
			t.titleStack = append(t.titleStack, t.title)
		}
	case 23:
		if n := len(t.titleStack); n > 0 {
			t.setTitle(t.titleStack[n-1])
			t.titleStack = t.titleStack[:n-1]
		}
	}
}

func (t *Terminal) reportInBandSize() {
	t.reply(fmt.Sprintf("\x1b[48;%d;%d;%d;%dt", t.rows, t.cols, t.rows*t.cellH, t.cols*t.cellW))
}

// xtsmgraphics answers sixel capability queries.
func (t *Terminal) xtsmgraphics(p *params) {
	item, action := p.get(0, 0), p.get(1, 0)
	switch item {
	case 1: // color registers
		switch action {
		case 1, 2, 4:
			t.reply("\x1b[?1;0;1024S")
		case 3:
			t.reply(fmt.Sprintf("\x1b[?1;0;%dS", clamp(p.get(2, 1024), 1, 1024)))
		default:
			t.reply("\x1b[?1;2;0S")
		}
	case 2: // sixel geometry
		switch action {
		case 1, 2, 4:
			t.reply(fmt.Sprintf("\x1b[?2;0;%d;%dS", t.cols*t.cellW, t.rows*t.cellH))
		default:
			t.reply("\x1b[?2;2;0S")
		}
	default:
		t.reply(fmt.Sprintf("\x1b[?%d;1;0S", item))
	}
}

// Private mode save/restore (XTSAVE/XTRESTORE).
func (t *Terminal) saveModes(p *params) {
	if t.savedModes == nil {
		t.savedModes = map[int]bool{}
	}
	for i := 0; i < p.n; i++ {
		n := p.get(i, 0)
		if md, ok := decModes[n]; ok {
			t.savedModes[n] = t.modes.get(md)
		}
	}
}

func (t *Terminal) restoreModes(p *params) {
	for i := 0; i < p.n; i++ {
		n := p.get(i, 0)
		if v, ok := t.savedModes[n]; ok {
			t.setMode(n, true, v)
		}
	}
}

// Kitty keyboard protocol flag stack.

func (t *Terminal) kittyFlags() int {
	if n := len(t.buf.kitty); n > 0 {
		return int(t.buf.kitty[n-1])
	}
	return 0
}

// KittyKeyboardFlags returns the active progressive enhancement flags.
func (t *Terminal) KittyKeyboardFlags() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.kittyFlags()
}

func (t *Terminal) kittyPush(flags int) {
	if len(t.buf.kitty) >= 16 {
		t.buf.kitty = t.buf.kitty[1:]
	}
	t.buf.kitty = append(t.buf.kitty, uint8(flags&0x1f))
}

func (t *Terminal) kittyPop(n int) {
	if n >= len(t.buf.kitty) {
		t.buf.kitty = t.buf.kitty[:0]
		return
	}
	t.buf.kitty = t.buf.kitty[:len(t.buf.kitty)-n]
}

func (t *Terminal) kittySet(flags, mode int) {
	cur := t.kittyFlags()
	switch mode {
	case 2:
		flags |= cur
	case 3:
		flags = cur &^ flags
	}
	if len(t.buf.kitty) == 0 {
		t.buf.kitty = append(t.buf.kitty, 0)
	}
	t.buf.kitty[len(t.buf.kitty)-1] = uint8(flags & 0x1f)
}

// ModifyOtherKeys returns the xterm modifyOtherKeys level (0-2).
func (t *Terminal) ModifyOtherKeys() int { return t.modifyOtherKeys }

// versionNumber turns "1.2.3" into 10203 for DA2.
func versionNumber(v string) int {
	var a, b, c int
	fmt.Sscanf(strings.TrimPrefix(v, "v"), "%d.%d.%d", &a, &b, &c)
	return a*10000 + b*100 + c
}

// SGR push/pop (XTPUSHSGR/XTPOPSGR).
func (t *Terminal) pushSGR() {
	if len(t.sgrStack) < 10 {
		t.sgrStack = append(t.sgrStack, t.cur.pen)
	}
}

func (t *Terminal) popSGR() {
	if n := len(t.sgrStack); n > 0 {
		t.cur.pen = t.sgrStack[n-1]
		t.sgrStack = t.sgrStack[:n-1]
	}
}

// Palette push/pop (XTPUSHCOLORS/XTPOPCOLORS).
func (t *Terminal) pushColors() {
	if len(t.colorStack) < 10 {
		t.colorStack = append(t.colorStack, t.pal)
	}
}

func (t *Terminal) popColors() {
	if n := len(t.colorStack); n > 0 {
		t.pal = t.colorStack[n-1]
		t.colorStack = t.colorStack[:n-1]
		t.seq++
	}
}
