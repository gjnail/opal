package vt

// buffer is a screen plus (for the primary screen) its scrollback. Lines
// live in one slice: history first, then the rows of the screen, so
// scrolling into history is just appending a line.
type buffer struct {
	lines []*Line
	start int // lines[start:] are live; the prefix is garbage awaiting compaction
	// evicted counts lines that have fallen off the top of history, which
	// makes absolute line numbers (evicted + index) stable as output scrolls.
	evicted int64

	history bool
	saved   savedCursor
	kitty   []uint8 // kitty keyboard flag stack, one per screen
}

func newBuffer(cols, rows int, history bool) *buffer {
	b := &buffer{history: history}
	for i := 0; i < rows; i++ {
		b.lines = append(b.lines, newLine(cols, Cell{}))
	}
	return b
}

func (b *buffer) count() int              { return len(b.lines) - b.start }
func (b *buffer) line(i int) *Line        { return b.lines[b.start+i] }
func (b *buffer) live() []*Line           { return b.lines[b.start:] }
func (b *buffer) screenBase(rows int) int { return b.count() - rows }

func (b *buffer) screenLine(y, rows int) *Line {
	return b.lines[len(b.lines)-rows+y]
}

func (b *buffer) push(l *Line) { b.lines = append(b.lines, l) }

// insert puts l at live index i.
func (b *buffer) insert(i int, l *Line) {
	i += b.start
	b.lines = append(b.lines, nil)
	copy(b.lines[i+1:], b.lines[i:])
	b.lines[i] = l
}

// dropFront evicts n lines from the top of history and returns them for
// recycling.
func (b *buffer) dropFront(n int, recycle func(*Line)) {
	if n > b.count() {
		n = b.count()
	}
	for i := 0; i < n; i++ {
		recycle(b.lines[b.start+i])
		b.lines[b.start+i] = nil
	}
	b.start += n
	b.evicted += int64(n)
	// Compact once the dead prefix is as big as the live part.
	if b.start > 1024 && b.start > b.count() {
		n := copy(b.lines, b.lines[b.start:])
		clear(b.lines[n:])
		b.lines = b.lines[:n]
		b.start = 0
	}
}

// replace swaps in a whole new set of live lines (reflow, clear history).
func (b *buffer) replace(lines []*Line) {
	b.lines = lines
	b.start = 0
}

// cursor is the active position plus everything DECSC saves with it.
type cursor struct {
	x, y        int
	pendingWrap bool
	pen         pen
	charsets    [4]charset
	gl          int // which of G0-G3 is mapped into GL
	singleShift int // G2/G3 for the next character only, or -1
	origin      bool
}

func (c *cursor) resetCharsets() {
	c.charsets = [4]charset{}
	c.gl = 0
	c.singleShift = -1
}

// pen is the current graphic rendition.
type pen struct {
	fg, bg  Color
	attrs   Attr
	ulColor Color
}

type savedCursor struct {
	valid       bool
	x, y        int
	pendingWrap bool
	pen         pen
	charsets    [4]charset
	gl          int
	origin      bool
}

type charset uint8

const (
	charsetASCII charset = iota
	charsetDECSpecial
	charsetUK
	charsetDECTechnical
)

// decSpecial maps 0x5f-0x7e in the DEC Special Graphics set (line drawing).
var decSpecial = [...]rune{
	' ', '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼',
	'⎺', '⎻', '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
}

func (cs charset) mapRune(r rune) rune {
	switch cs {
	case charsetDECSpecial:
		if r >= 0x5f && r <= 0x7e {
			return decSpecial[r-0x5f]
		}
	case charsetUK:
		if r == '#' {
			return '£'
		}
	}
	return r
}
