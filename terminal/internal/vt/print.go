package vt

import (
	"sync"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

var (
	bmpWidthOnce sync.Once
	bmpWidth     [0x10000]int8
)

// RuneWidth is the number of cells a single code point occupies on its own:
// 0 for combining marks and other zero-width characters, 2 for East Asian
// wide characters and emoji, 1 otherwise.
func RuneWidth(r rune) int {
	if r < 0x300 {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return 0
		}
		return 1
	}
	if r < 0x10000 {
		bmpWidthOnce.Do(fillBMPWidths)
		return int(bmpWidth[r])
	}
	return slowWidth(r)
}

func fillBMPWidths() {
	for r := rune(0); r < 0x10000; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			bmpWidth[r] = 1
			continue
		}
		bmpWidth[r] = int8(slowWidth(r))
	}
}

func slowWidth(r rune) int {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	_, _, w, _ := uniseg.FirstGraphemeCluster(b[:n], -1)
	return min(w, 2)
}

// ClusterWidth is the width of a whole grapheme cluster, as used in
// grapheme clustering mode (2027).
func ClusterWidth(s string) int {
	_, _, w, _ := uniseg.FirstGraphemeClusterInString(s, -1)
	return min(w, 2)
}

// joinsCluster reports whether r continues the grapheme cluster s.
func joinsCluster(s string, r rune) bool {
	if r < 0x80 {
		return false // ASCII is never Extend, ZWJ or SpacingMark
	}
	var b [64]byte
	buf := append(b[:0], s...)
	buf = utf8.AppendRune(buf, r)
	c, _, _, _ := uniseg.FirstGraphemeCluster(buf, -1)
	return len(c) == len(buf)
}

// printASCII is the hot path: plain ASCII with no charset tricks.
func (t *Terminal) printASCII(b []byte) {
	c := &t.cur
	if c.singleShift >= 0 || c.charsets[c.gl] != charsetASCII || t.modes.get(modeInsert) {
		for _, ch := range b {
			t.print(rune(ch))
		}
		return
	}
	autowrap := t.modes.get(modeAutowrap)
	sem := Attr(t.semantic) << attrSemanticShift
	cell := Cell{Fg: c.pen.fg, Bg: c.pen.bg, A: c.pen.attrs | sem}
	needExt := c.pen.ulColor != DefaultColor || t.link != nil
	left, right := t.marginsForCursor()
	l := t.line(c.y)
	for _, ch := range b {
		if c.pendingWrap {
			if autowrap {
				t.wrapLine(left, right)
				l = t.line(c.y)
			}
			c.pendingWrap = false
		}
		if t.pendingMark != nil {
			t.placePendingMark()
		}
		x := c.x
		cell.R = rune(ch)
		if l.Cells[x].A&(attrWide|attrSpacer|attrExt) != 0 {
			fixWideEdges(l, x, x+1)
			l.set(x, cell)
		} else {
			l.Cells[x] = cell
		}
		if needExt {
			l.setExt(x, &cellExt{ulColor: c.pen.ulColor, link: t.link})
		}
		if x >= right {
			if autowrap {
				c.pendingWrap = true
			}
		} else {
			c.x++
		}
	}
	if len(b) > 0 {
		t.lastRune = rune(b[len(b)-1])
		t.lastValid = true
	}
}

// marginsForCursor returns the horizontal bounds printing wraps within:
// the DECSLRM margins when the cursor is inside them, else the screen.
func (t *Terminal) marginsForCursor() (left, right int) {
	if t.modes.get(modeLRMargins) && t.cur.x >= t.left && t.cur.x <= t.right {
		return t.left, t.right
	}
	return 0, t.cols - 1
}

// wrapLine performs an autowrap: mark the line as continued and move to
// the start of the next one.
func (t *Terminal) wrapLine(left, right int) {
	if left == 0 && right == t.cols-1 {
		t.line(t.cur.y).Wrapped = true
	}
	t.index()
	t.cur.x = left
}

// print writes one character.
func (t *Terminal) print(r rune) {
	c := &t.cur
	if r < 0x80 {
		cs := c.charsets[c.gl]
		if c.singleShift >= 0 {
			cs = c.charsets[c.singleShift]
			c.singleShift = -1
		}
		r = cs.mapRune(r)
	}

	// Combining marks, ZWJ sequences, variation selectors, flags: extend
	// the previous cell instead of taking a new one.
	if r >= 0x300 {
		if px, py, ok := t.prevCell(); ok {
			l := t.line(py)
			prev := l.Text(px)
			var joins bool
			if t.modes.get(modeGraphemeCluster) {
				joins = joinsCluster(prev, r)
			} else {
				joins = RuneWidth(r) == 0
			}
			if joins {
				t.extendCluster(l, px, py, prev+string(r))
				return
			}
		}
	}

	w := RuneWidth(r)
	if w == 0 {
		return // nothing to attach it to
	}
	t.putChar(r, w)
}

// prevCell finds the cell holding the character just before the cursor.
func (t *Terminal) prevCell() (x, y int, ok bool) {
	x, y = t.cur.x, t.cur.y
	if !t.cur.pendingWrap {
		x--
	}
	if x < 0 {
		return 0, 0, false
	}
	l := t.line(y)
	if l.Cells[x].A&attrSpacer != 0 {
		x--
		if x < 0 {
			return 0, 0, false
		}
	}
	if l.Cells[x].R == 0 {
		return 0, 0, false
	}
	return x, y, true
}

// extendCluster stores the grown cluster and, in grapheme mode, widens the
// cell when the cluster became double-width (e.g. an emoji + VS16).
func (t *Terminal) extendCluster(l *Line, x, y int, cluster string) {
	e := l.getExt(x)
	ne := &cellExt{grapheme: cluster}
	if e != nil {
		ne.ulColor, ne.link = e.ulColor, e.link
	}
	l.setExt(x, ne)
	t.seq++
	if !t.modes.get(modeGraphemeCluster) || l.Cells[x].A&attrWide != 0 {
		return
	}
	if ClusterWidth(cluster) < 2 {
		return
	}
	_, right := t.marginsForCursor()
	if x+1 > right {
		return // no room to grow at the margin; stays narrow
	}
	fixWideEdges(l, x+1, x+2)
	head := l.Cells[x]
	l.Cells[x].A |= attrWide
	l.set(x+1, Cell{Fg: head.Fg, Bg: head.Bg, A: head.A&attrSemanticMask | attrSpacer})
	// The cursor sat just after the narrow cell; step past the spacer.
	if y == t.cur.y {
		if x+2 > right {
			t.cur.x = right
			if t.modes.get(modeAutowrap) {
				t.cur.pendingWrap = true
			}
		} else {
			t.cur.x = x + 2
		}
	}
}

// putChar places a character of width w (1 or 2) at the cursor.
func (t *Terminal) putChar(r rune, w int) {
	c := &t.cur
	autowrap := t.modes.get(modeAutowrap)
	left, right := t.marginsForCursor()

	if c.pendingWrap {
		if autowrap {
			t.wrapLine(left, right)
		}
		c.pendingWrap = false
	}
	if w == 2 && c.x >= right {
		if !autowrap || right == left {
			return // a wide character can't fit
		}
		// Leave the last column empty and continue on the next line.
		l := t.line(c.y)
		fixWideEdges(l, c.x, c.x+1)
		l.set(c.x, Cell{Bg: c.pen.bg, A: attrPadding})
		t.wrapLine(left, right)
	}
	if t.modes.get(modeInsert) {
		t.insertChars(w)
	}
	if t.pendingMark != nil {
		t.placePendingMark()
	}

	l := t.line(c.y)
	x := c.x
	fixWideEdges(l, x, x+w)
	sem := Attr(t.semantic) << attrSemanticShift
	cell := Cell{R: r, Fg: c.pen.fg, Bg: c.pen.bg, A: c.pen.attrs | sem}
	if w == 2 {
		cell.A |= attrWide
	}
	l.set(x, cell)
	if c.pen.ulColor != DefaultColor || t.link != nil {
		l.setExt(x, &cellExt{ulColor: c.pen.ulColor, link: t.link})
	}
	if w == 2 {
		l.set(x+1, Cell{Fg: c.pen.fg, Bg: c.pen.bg, A: c.pen.attrs | sem | attrSpacer})
		if c.pen.ulColor != DefaultColor || t.link != nil {
			l.setExt(x+1, &cellExt{ulColor: c.pen.ulColor, link: t.link})
		}
	}
	t.lastRune = r
	t.lastValid = true
	t.seq++

	if x+w > right {
		c.x = right
		if autowrap {
			c.pendingWrap = true
		}
	} else {
		c.x = x + w
	}
}

// repeat implements REP: print the last graphic character n more times.
func (t *Terminal) repeat(n int) {
	if !t.lastValid {
		return
	}
	n = min(n, t.cols*t.rows)
	r := t.lastRune
	w := RuneWidth(r)
	if w == 0 {
		return
	}
	for i := 0; i < n; i++ {
		t.putChar(r, w)
	}
}
