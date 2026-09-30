package vt

import (
	"regexp"
	"strings"
	"unicode"
)

// Pos is a cell position in absolute line numbers: history and screen
// together, counted from the first line the terminal ever kept (see
// FirstAbs). Absolute positions stay on the same text while output
// scrolls.
type Pos struct {
	Row int64
	Col int
}

// Before orders positions.
func (p Pos) Before(q Pos) bool { return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col) }

// AbsLine returns the line at an absolute row, or nil if it has scrolled
// out of history or doesn't exist yet. The caller holds the lock.
func (t *Terminal) AbsLine(row int64) *Line {
	i := row - t.buf.evicted
	if i < 0 || i >= int64(t.buf.count()) {
		return nil
	}
	return t.buf.line(int(i))
}

// ScreenTopAbs is the absolute row of the top of the screen.
func (t *Terminal) ScreenTopAbs() int64 { return t.buf.evicted + int64(t.buf.count()-t.rows) }

// LastAbs is the absolute row of the bottom line of the screen.
func (t *Terminal) LastAbs() int64 { return t.buf.evicted + int64(t.buf.count()) - 1 }

// Text extracts the text between two positions (inclusive of both ends).
// Soft-wrapped lines are joined; hard line breaks become "\n"; trailing
// blanks on each line are dropped. With rect set, it takes the same
// columns from every row instead (a block selection).
func (t *Terminal) Text(from, to Pos, rect bool) string {
	if to.Before(from) {
		from, to = to, from
	}
	var b strings.Builder
	if rect {
		c0, c1 := min(from.Col, to.Col), max(from.Col, to.Col)
		for row := from.Row; row <= to.Row; row++ {
			l := t.AbsLine(row)
			if l != nil {
				b.WriteString(strings.TrimRight(lineText(l, c0, c1+1), " "))
			}
			if row < to.Row {
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
	for row := from.Row; row <= to.Row; row++ {
		l := t.AbsLine(row)
		if l == nil {
			continue
		}
		c0, c1 := 0, len(l.Cells)
		if row == from.Row {
			c0 = from.Col
		}
		if row == to.Row {
			c1 = min(to.Col+1, len(l.Cells))
		}
		s := lineText(l, c0, c1)
		if row < to.Row && l.Wrapped {
			b.WriteString(s)
			continue
		}
		b.WriteString(strings.TrimRight(s, " "))
		if row < to.Row {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// lineText returns cells [c0, c1) of a line as text.
func lineText(l *Line, c0, c1 int) string {
	c0 = max(c0, 0)
	c1 = min(c1, len(l.Cells))
	var b strings.Builder
	for x := c0; x < c1; x++ {
		if l.Cells[x].A&(attrSpacer|attrPadding) != 0 {
			continue
		}
		b.WriteString(l.Text(x))
	}
	return b.String()
}

// WordBounds finds the word around p for double-click selection. Letters,
// digits and wordChars count as word characters; anything else selects
// just itself (or a run of the same character, like "=====").
func (t *Terminal) WordBounds(p Pos, wordChars string) (Pos, Pos) {
	l := t.AbsLine(p.Row)
	if l == nil || p.Col >= len(l.Cells) {
		return p, p
	}
	isWord := func(x int) bool {
		s := l.Text(x)
		r := []rune(s)[0]
		return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(wordChars, r) || r > 0x2e80
	}
	x := p.Col
	if l.Cells[x].A&attrSpacer != 0 && x > 0 {
		x--
	}
	same := func(a, b int) bool { return l.Text(a) == l.Text(b) }
	var match func(int) bool
	switch {
	case isWord(x):
		match = isWord
	case l.Text(x) == " ":
		match = func(i int) bool { return l.Text(i) == " " }
	default:
		match = func(i int) bool { return same(i, x) }
	}
	a, b := x, x
	for a > 0 && (l.Cells[a-1].A&attrSpacer != 0 || match(a-1)) {
		a--
	}
	for b+1 < len(l.Cells) && (l.Cells[b+1].A&attrSpacer != 0 || match(b+1)) {
		b++
	}
	return Pos{p.Row, a}, Pos{p.Row, b}
}

// LogicalLine returns the first and last rows of the soft-wrapped line
// containing row.
func (t *Terminal) LogicalLine(row int64) (int64, int64) {
	first, last := row, row
	for {
		prev := t.AbsLine(first - 1)
		if prev == nil || !prev.Wrapped {
			break
		}
		first--
	}
	for {
		l := t.AbsLine(last)
		if l == nil || !l.Wrapped || t.AbsLine(last+1) == nil {
			break
		}
		last++
	}
	return first, last
}

// Match is a search hit.
type Match struct{ Start, End Pos }

// Search finds every match of re in history and screen. Soft-wrapped lines
// are searched as one, so matches can span rows. At most limit matches are
// returned (0 = no limit).
func (t *Terminal) Search(re *regexp.Regexp, limit int) []Match {
	return t.SearchRange(re, t.buf.evicted, t.LastAbs(), limit)
}

// SearchRange is Search limited to the absolute rows [first, last].
func (t *Terminal) SearchRange(re *regexp.Regexp, first, last int64, limit int) []Match {
	var out []Match
	first = max(first, t.buf.evicted)
	last = min(last, t.LastAbs())
	// Start at the beginning of a wrapped line that runs into the range.
	for first > t.buf.evicted {
		if prev := t.AbsLine(first - 1); prev == nil || !prev.Wrapped {
			break
		}
		first--
	}
	for row := first; row <= last; {
		start, end := row, row
		for end < last {
			l := t.AbsLine(end)
			if l == nil || !l.Wrapped {
				break
			}
			end++
		}
		// Build the logical line's text with a map from byte offset back
		// to cell positions.
		var b strings.Builder
		var pos []Pos
		for r := start; r <= end; r++ {
			l := t.AbsLine(r)
			n := len(l.Cells)
			if r == end {
				n = l.contentEnd()
			}
			for x := 0; x < n; x++ {
				if l.Cells[x].A&(attrSpacer|attrPadding) != 0 {
					continue
				}
				s := l.Text(x)
				for i := 0; i < len(s); i++ {
					pos = append(pos, Pos{r, x})
				}
				b.WriteString(s)
			}
		}
		text := b.String()
		for _, m := range re.FindAllStringIndex(text, -1) {
			if m[0] == m[1] {
				continue
			}
			out = append(out, Match{Start: pos[m[0]], End: pos[m[1]-1]})
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
		row = end + 1
	}
	return out
}

// PromptRows lists the absolute rows where shell prompts start, oldest
// first.
func (t *Terminal) PromptRows() []int64 {
	var out []int64
	for i := 0; i < t.buf.count(); i++ {
		if t.buf.line(i).Prompt != nil {
			out = append(out, t.buf.evicted+int64(i))
		}
	}
	return out
}

// CommandOutput returns the rows of the output of the command whose prompt
// starts at promptRow: from where output began up to the next prompt.
func (t *Terminal) CommandOutput(promptRow int64) (from, to Pos, ok bool) {
	last := t.LastAbs()
	start := int64(-1)
	for row := promptRow; row <= last; row++ {
		l := t.AbsLine(row)
		if l == nil {
			break
		}
		if row > promptRow && l.Prompt != nil {
			if start < 0 {
				return Pos{}, Pos{}, false
			}
			end := row - 1
			for end > start {
				if el := t.AbsLine(end); el != nil && el.String() != "" {
					break
				}
				end--
			}
			return Pos{start, 0}, Pos{end, t.cols - 1}, true
		}
		if start < 0 {
			for _, c := range l.Cells {
				if c.A.Semantic() == SemanticOutput && row > promptRow {
					start = row
					break
				}
			}
		}
	}
	if start < 0 {
		return Pos{}, Pos{}, false
	}
	return Pos{start, 0}, Pos{last, t.cols - 1}, true
}

var urlRe = regexp.MustCompile(`(?:https?|ftp|file|ssh|git)://[^\s<>"'` + "`" + `\x00-\x1f]+|www\.[^\s<>"'` + "`" + `]+`)

// URLAt returns the URL (explicit OSC 8 link or detected in the text)
// under p.
func (t *Terminal) URLAt(p Pos) (string, Pos, Pos, bool) {
	l := t.AbsLine(p.Row)
	if l == nil {
		return "", Pos{}, Pos{}, false
	}
	if link := l.Link(p.Col); link != nil {
		a, b := p.Col, p.Col
		for a > 0 && l.Link(a-1) == link {
			a--
		}
		for b+1 < len(l.Cells) && l.Link(b+1) == link {
			b++
		}
		return link.URI, Pos{p.Row, a}, Pos{p.Row, b}, true
	}
	first, last := t.LogicalLine(p.Row)
	var b strings.Builder
	var pos []Pos
	for r := first; r <= last; r++ {
		l := t.AbsLine(r)
		for x := 0; x < len(l.Cells); x++ {
			if l.Cells[x].A&(attrSpacer|attrPadding) != 0 {
				continue
			}
			s := l.Text(x)
			for i := 0; i < len(s); i++ {
				pos = append(pos, Pos{r, x})
			}
			b.WriteString(s)
		}
	}
	text := b.String()
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		s, e := m[0], m[1]
		// Trailing punctuation usually belongs to the sentence.
		for e > s && strings.ContainsRune(".,;:!?)]}'\"", rune(text[e-1])) {
			if text[e-1] == ')' && strings.Count(text[s:e], "(") >= strings.Count(text[s:e], ")") {
				break
			}
			e--
		}
		if e <= s {
			continue
		}
		a, z := pos[s], pos[e-1]
		if !p.Before(a) && !z.Before(p) {
			u := text[s:e]
			if strings.HasPrefix(u, "www.") {
				u = "https://" + u
			}
			return u, a, z, true
		}
	}
	return "", Pos{}, Pos{}, false
}
