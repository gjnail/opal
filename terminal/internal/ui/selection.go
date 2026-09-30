package ui

import "opal/terminal/internal/vt"

type selMode uint8

const (
	selChar selMode = iota
	selWord
	selLine
	selBlock
)

// selection is in absolute rows so it stays on its text while output
// scrolls.
type selection struct {
	active bool
	mode   selMode
	// anchor is where the drag started; head follows the mouse. For word
	// and line modes the anchor is expanded to whole words or lines.
	anchor, head vt.Pos
	anchorEnd    vt.Pos
}

func (s *selection) clear() { *s = selection{} }

// bounds returns the ordered, mode-expanded start and end.
func (s *selection) bounds() (vt.Pos, vt.Pos) {
	a, b := s.anchor, s.head
	if s.mode == selBlock {
		return a, b
	}
	if b.Before(a) {
		return b, s.anchorEnd
	}
	if s.anchorEnd.Before(b) {
		return a, b
	}
	return a, s.anchorEnd
}

// columns returns the selected column range [from, to) on row.
func (s *selection) columns(row int64, width int) (int, int) {
	if !s.active {
		return 0, 0
	}
	a, b := s.bounds()
	if s.mode == selBlock {
		lo, hi := min(a.Row, b.Row), max(a.Row, b.Row)
		if row < lo || row > hi {
			return 0, 0
		}
		return min(a.Col, b.Col), max(a.Col, b.Col) + 1
	}
	if row < a.Row || row > b.Row {
		return 0, 0
	}
	from, to := 0, width
	if row == a.Row {
		from = a.Col
	}
	if row == b.Row {
		to = b.Col + 1
	}
	return from, to
}

// text copies the selection out of the terminal. The caller holds the
// terminal lock.
func (s *selection) text(t *vt.Terminal) string {
	if !s.active {
		return ""
	}
	a, b := s.bounds()
	return t.Text(a, b, s.mode == selBlock)
}

// start begins a selection at p. Word and line modes expand immediately.
func (s *selection) start(t *vt.Terminal, p vt.Pos, mode selMode, wordChars string) {
	s.active = true
	s.mode = mode
	switch mode {
	case selWord:
		s.anchor, s.anchorEnd = t.WordBounds(p, wordChars)
		s.head = s.anchorEnd
	case selLine:
		first, last := t.LogicalLine(p.Row)
		cols, _ := t.Size()
		s.anchor, s.anchorEnd = vt.Pos{Row: first, Col: 0}, vt.Pos{Row: last, Col: cols - 1}
		s.head = s.anchorEnd
	default:
		s.anchor, s.anchorEnd, s.head = p, p, p
	}
}

// extend moves the head, snapping to words or lines in those modes.
func (s *selection) extend(t *vt.Terminal, p vt.Pos, wordChars string) {
	switch s.mode {
	case selWord:
		a, b := t.WordBounds(p, wordChars)
		if p.Before(s.anchor) {
			s.head = a
		} else {
			s.head = b
		}
	case selLine:
		first, last := t.LogicalLine(p.Row)
		cols, _ := t.Size()
		if p.Before(s.anchor) {
			s.head = vt.Pos{Row: first, Col: 0}
		} else {
			s.head = vt.Pos{Row: last, Col: cols - 1}
		}
	default:
		s.head = p
	}
}
