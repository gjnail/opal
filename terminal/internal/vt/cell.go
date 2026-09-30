package vt

import (
	"strings"
	"time"
)

// Attr holds a cell's rendition flags plus some bookkeeping bits.
type Attr uint32

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrBlink
	AttrRapidBlink
	AttrInverse
	AttrHidden
	AttrStrike
	AttrOverline
	AttrProtected // DECSCA: selective erase leaves it alone

	// attrWide marks the left half of a double-width character.
	attrWide
	// attrSpacer marks the right half of a double-width character.
	attrSpacer
	// attrExt means the line's ext map has an entry for this cell.
	attrExt
	// attrPadding marks the empty last column left when a wide character
	// wrapped early. Reflow drops it.
	attrPadding

	attrUnderlineShift = 16
	attrUnderlineMask  = Attr(7) << attrUnderlineShift

	attrSemanticShift = 20
	attrSemanticMask  = Attr(3) << attrSemanticShift

	// penAttrs are the bits SGR controls; the rest are structural.
	penAttrs = AttrBold | AttrDim | AttrItalic | AttrBlink | AttrRapidBlink | AttrInverse |
		AttrHidden | AttrStrike | AttrOverline | AttrProtected | attrUnderlineMask
)

// Underline styles (SGR 4:n).
const (
	UnderlineNone = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Underline returns the underline style.
func (a Attr) Underline() int { return int(a&attrUnderlineMask) >> attrUnderlineShift }

func (a Attr) withUnderline(style int) Attr {
	return a&^attrUnderlineMask | Attr(style&7)<<attrUnderlineShift
}

// Semantic zones, from shell integration (OSC 133).
const (
	SemanticOutput = iota
	SemanticPrompt
	SemanticInput
)

// Semantic returns which part of a command block the cell belongs to.
func (a Attr) Semantic() int { return int(a&attrSemanticMask) >> attrSemanticShift }

// Cell is one character cell. Blank cells have R == 0.
type Cell struct {
	R  rune
	Fg Color
	Bg Color
	A  Attr
}

// Wide reports whether the cell holds the left half of a double-width character.
func (c Cell) Wide() bool { return c.A&attrWide != 0 }

// Spacer reports whether the cell is the right half of a double-width character.
func (c Cell) Spacer() bool { return c.A&attrSpacer != 0 }

// Blank reports whether the cell shows nothing but its background.
func (c Cell) Blank() bool { return (c.R == 0 || c.R == ' ') && c.A&attrExt == 0 }

// Hyperlink is an OSC 8 link.
type Hyperlink struct {
	ID  string
	URI string
}

// cellExt carries the rarely used, larger parts of a cell.
type cellExt struct {
	grapheme string // full cluster when it's more than one rune
	ulColor  Color
	link     *Hyperlink
}

// LineAttr is the DEC double-width/double-height line attribute.
type LineAttr uint8

const (
	LineNormal LineAttr = iota
	LineDoubleWidth
	LineDoubleTop
	LineDoubleBottom
)

// Line is one row of cells.
type Line struct {
	Cells []Cell
	// Wrapped means the text continues on the next line: it was broken by
	// autowrap, not by a newline. Reflow and selection rely on it.
	Wrapped bool
	Attr    LineAttr
	// Prompt is set on the line where a shell prompt starts (OSC 133;A).
	Prompt *PromptMark
	ext    map[int]*cellExt
}

// PromptMark records one command's life, as reported by shell integration.
type PromptMark struct {
	Started  time.Time // prompt drawn
	Executed time.Time // output began (OSC 133;C)
	Finished time.Time // command ended (OSC 133;D)
	// Exit is the command's exit status, or -1 while it's unknown.
	Exit    int
	Command string // from OSC 633;E, when the shell sends it
	Aborted bool   // the prompt was abandoned (Ctrl+C, empty line)
}

// Duration is how long the command ran, if it has finished.
func (m *PromptMark) Duration() (time.Duration, bool) {
	if m == nil || m.Executed.IsZero() || m.Finished.IsZero() {
		return 0, false
	}
	return m.Finished.Sub(m.Executed), true
}

func newLine(cols int, fill Cell) *Line {
	l := &Line{Cells: make([]Cell, cols)}
	if fill != (Cell{}) {
		for i := range l.Cells {
			l.Cells[i] = fill
		}
	}
	return l
}

// reset clears a line for reuse at the given width.
func (l *Line) reset(cols int, fill Cell) {
	if cap(l.Cells) >= cols {
		l.Cells = l.Cells[:cols]
	} else {
		l.Cells = make([]Cell, cols)
	}
	for i := range l.Cells {
		l.Cells[i] = fill
	}
	l.Wrapped = false
	l.Attr = LineNormal
	l.Prompt = nil
	l.ext = nil
}

func (l *Line) getExt(x int) *cellExt {
	if l.ext == nil {
		return nil
	}
	return l.ext[x]
}

func (l *Line) setExt(x int, e *cellExt) {
	if e == nil || (e.grapheme == "" && e.ulColor == 0 && e.link == nil) {
		if l.ext != nil {
			delete(l.ext, x)
		}
		l.Cells[x].A &^= attrExt
		return
	}
	if l.ext == nil {
		l.ext = map[int]*cellExt{}
	}
	l.ext[x] = e
	l.Cells[x].A |= attrExt
}

// set stores c at x, dropping whatever extra data the old cell had.
func (l *Line) set(x int, c Cell) {
	if l.Cells[x].A&attrExt != 0 && l.ext != nil {
		delete(l.ext, x)
	}
	l.Cells[x] = c
}

// fill blanks cells [from, to).
func (l *Line) fill(from, to int, c Cell) {
	if from < 0 {
		from = 0
	}
	if to > len(l.Cells) {
		to = len(l.Cells)
	}
	for x := from; x < to; x++ {
		l.set(x, c)
	}
}

// moveCells copies n cells from src to dst within the line, carrying ext data.
func (l *Line) moveCells(dst, src, n int) {
	if n <= 0 || dst == src {
		return
	}
	var moved map[int]*cellExt
	if l.ext != nil {
		for x, e := range l.ext {
			if x >= src && x < src+n {
				if moved == nil {
					moved = map[int]*cellExt{}
				}
				moved[x-src+dst] = e
			}
		}
		for x := range l.ext {
			if (x >= src && x < src+n) || (x >= dst && x < dst+n) {
				delete(l.ext, x)
			}
		}
	}
	copy(l.Cells[dst:dst+n], l.Cells[src:src+n])
	for x, e := range moved {
		l.ext[x] = e
	}
}

// Text returns the cell's text: its whole grapheme cluster, or a space for
// a blank cell. Spacers return "".
func (l *Line) Text(x int) string {
	c := l.Cells[x]
	if c.A&attrSpacer != 0 {
		return ""
	}
	if c.A&attrExt != 0 {
		if e := l.getExt(x); e != nil && e.grapheme != "" {
			return e.grapheme
		}
	}
	if c.R == 0 {
		return " "
	}
	return string(c.R)
}

// Link returns the OSC 8 hyperlink at x, if any.
func (l *Line) Link(x int) *Hyperlink {
	if x < 0 || x >= len(l.Cells) || l.Cells[x].A&attrExt == 0 {
		return nil
	}
	if e := l.getExt(x); e != nil {
		return e.link
	}
	return nil
}

// UnderlineColor returns the SGR 58 color at x.
func (l *Line) UnderlineColor(x int) Color {
	if x < 0 || x >= len(l.Cells) || l.Cells[x].A&attrExt == 0 {
		return DefaultColor
	}
	if e := l.getExt(x); e != nil {
		return e.ulColor
	}
	return DefaultColor
}

// String returns the line's text with trailing blanks removed.
func (l *Line) String() string {
	var b strings.Builder
	end := l.contentEnd()
	for x := 0; x < end; x++ {
		b.WriteString(l.Text(x))
	}
	return strings.TrimRight(b.String(), " ")
}

// contentEnd is one past the last non-blank cell.
func (l *Line) contentEnd() int {
	for x := len(l.Cells) - 1; x >= 0; x-- {
		c := l.Cells[x]
		if c.R != 0 || c.A&(attrExt|attrSpacer) != 0 {
			return x + 1
		}
	}
	return 0
}

// isBlank reports whether the line holds nothing worth keeping.
func (l *Line) isBlank() bool {
	if l.Wrapped || l.Prompt != nil {
		return false
	}
	for _, c := range l.Cells {
		if c != (Cell{}) {
			return false
		}
	}
	return true
}

// Clone copies the line, so a renderer can work on it without holding
// the terminal's lock.
func (l *Line) Clone() *Line { return l.clone() }

// HasExt reports whether the cell carries a grapheme cluster, hyperlink
// or underline color.
func (c Cell) HasExt() bool { return c.A&attrExt != 0 }

func (l *Line) clone() *Line {
	n := &Line{Cells: append([]Cell(nil), l.Cells...), Wrapped: l.Wrapped, Attr: l.Attr, Prompt: l.Prompt}
	if len(l.ext) > 0 {
		n.ext = make(map[int]*cellExt, len(l.ext))
		for k, v := range l.ext {
			n.ext[k] = v
		}
	}
	return n
}
