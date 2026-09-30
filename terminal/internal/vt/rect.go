package vt

// Rectangular area operations (VT400): DECFRA, DECERA, DECSERA, DECCRA,
// plus column insert/delete and ECMA-48 horizontal shifts.

// rect reads Pt;Pl;Pb;Pr starting at param i, 1-based and origin-relative,
// and returns 0-based inclusive bounds clipped to the screen.
func (t *Terminal) rect(p *params, i int) (top, left, bottom, right int, ok bool) {
	top = p.getNZ(i, 1) - 1
	left = p.getNZ(i+1, 1) - 1
	bottom = p.getNZ(i+2, t.rows) - 1
	right = p.getNZ(i+3, t.cols) - 1
	if t.cur.origin {
		top += t.top
		bottom += t.top
		left += t.left
		right += t.left
		bottom = min(bottom, t.bottom)
		right = min(right, t.right)
	}
	bottom = min(bottom, t.rows-1)
	right = min(right, t.cols-1)
	return top, left, bottom, right, top <= bottom && left <= right
}

func (t *Terminal) fillRect(p *params) {
	ch := p.get(0, 0)
	if !(ch >= 32 && ch <= 126) && !(ch >= 160 && ch <= 255) {
		return
	}
	top, left, bottom, right, ok := t.rect(p, 1)
	if !ok {
		return
	}
	cell := Cell{R: rune(ch), Fg: t.cur.pen.fg, Bg: t.cur.pen.bg, A: t.cur.pen.attrs}
	for y := top; y <= bottom; y++ {
		l := t.line(y)
		fixWideEdges(l, left, right+1)
		for x := left; x <= right; x++ {
			l.set(x, cell)
		}
	}
	t.seq++
}

func (t *Terminal) eraseRect(p *params, selective bool) {
	top, left, bottom, right, ok := t.rect(p, 0)
	if !ok {
		return
	}
	for y := top; y <= bottom; y++ {
		t.eraseLineRange(y, left, right+1, selective)
	}
	t.seq++
}

func (t *Terminal) copyRect(p *params) {
	top, left, bottom, right, ok := t.rect(p, 0)
	if !ok {
		return
	}
	dt := p.getNZ(5, 1) - 1
	dl := p.getNZ(6, 1) - 1
	if t.cur.origin {
		dt += t.top
		dl += t.left
	}
	h := min(bottom-top+1, t.rows-dt)
	w := min(right-left+1, t.cols-dl)
	if h <= 0 || w <= 0 {
		return
	}
	// Copy through a snapshot so overlapping source and destination work.
	snap := make([]*Line, h)
	for i := 0; i < h; i++ {
		snap[i] = t.line(top + i).clone()
	}
	for i := 0; i < h; i++ {
		t.copyCells(t.line(dt+i), dl, snap[i], left, w)
	}
	t.seq++
}

// insertColumns implements DECIC within the scroll region and margins.
func (t *Terminal) insertColumns(n int) {
	if t.cur.y < t.top || t.cur.y > t.bottom || t.cur.x < t.left || t.cur.x > t.right {
		return
	}
	x := t.cur.x
	w := t.right - x + 1
	n = min(n, w)
	for y := t.top; y <= t.bottom; y++ {
		l := t.line(y)
		fixWideEdges(l, x, t.right+1)
		l.moveCells(x+n, x, w-n)
		l.fill(x, x+n, t.blank())
	}
	t.seq++
}

// deleteColumns implements DECDC.
func (t *Terminal) deleteColumns(n int) {
	if t.cur.y < t.top || t.cur.y > t.bottom || t.cur.x < t.left || t.cur.x > t.right {
		return
	}
	x := t.cur.x
	w := t.right - x + 1
	n = min(n, w)
	for y := t.top; y <= t.bottom; y++ {
		l := t.line(y)
		fixWideEdges(l, x, x+n)
		l.moveCells(x, x+n, w-n)
		l.fill(t.right+1-n, t.right+1, t.blank())
	}
	t.seq++
}

// shiftColumns implements SL (n < 0) and SR (n > 0) over the whole region.
func (t *Terminal) shiftColumns(n int) {
	w := t.right - t.left + 1
	for y := t.top; y <= t.bottom; y++ {
		l := t.line(y)
		if n < 0 {
			k := min(-n, w)
			fixWideEdges(l, t.left, t.left+k)
			l.moveCells(t.left, t.left+k, w-k)
			l.fill(t.right+1-k, t.right+1, t.blank())
		} else {
			k := min(n, w)
			fixWideEdges(l, t.left, t.right+1)
			l.moveCells(t.left+k, t.left, w-k)
			l.fill(t.left, t.left+k, t.blank())
		}
	}
	t.seq++
}
