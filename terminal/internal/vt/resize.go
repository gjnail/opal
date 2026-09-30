package vt

// Resize changes the grid. The primary screen's text is reflowed: lines
// that were soft-wrapped are rejoined and wrapped again at the new width,
// and the cursor stays on the character it was on. The alternate screen is
// cropped or padded, since full-screen programs redraw it anyway.
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cols, rows = max(cols, 2), max(rows, 1)
	if cols == t.cols && rows == t.rows {
		return
	}
	oldCols, oldRows := t.cols, t.rows

	// Cursors to carry through the reflow: the live cursor on whichever
	// screen is active, and the primary screen's saved cursor.
	onPrimary := t.buf == t.primary
	var pts []reflowPoint
	if onPrimary {
		pts = append(pts, reflowPoint{y: t.cur.y, x: t.cur.x})
	}
	if t.primary.saved.valid {
		pts = append(pts, reflowPoint{y: t.primary.saved.y, x: t.primary.saved.x})
	}
	pts = t.reflowBuffer(t.primary, oldCols, oldRows, cols, rows, pts)
	if onPrimary {
		t.cur.x, t.cur.y = pts[0].x, pts[0].y
		pts = pts[1:]
	}
	if t.primary.saved.valid {
		t.primary.saved.x, t.primary.saved.y = pts[0].x, pts[0].y
	}

	t.cropBuffer(t.alt, oldRows, cols, rows)
	if !onPrimary {
		t.cur.x = clamp(t.cur.x, 0, cols-1)
		t.cur.y = clamp(t.cur.y, 0, rows-1)
	}
	t.alt.saved.x = clamp(t.alt.saved.x, 0, cols-1)
	t.alt.saved.y = clamp(t.alt.saved.y, 0, rows-1)

	t.cols, t.rows = cols, rows
	t.top, t.bottom = 0, rows-1
	t.left, t.right = 0, cols-1
	old := t.tabs
	t.resetTabs()
	copy(t.tabs, old)
	if t.cur.x >= cols-1 && t.cur.pendingWrap {
		t.cur.x = cols - 1
	} else {
		t.cur.pendingWrap = false
	}
	t.freeLines = nil
	t.lastValid = false
	t.generation++
	t.seq++
	t.images.resized(t)
	if t.modes.get(modeInBandResize) {
		t.reportInBandSize()
	}
}

type reflowPoint struct{ x, y int }

// cropBuffer resizes a buffer without reflow.
func (t *Terminal) cropBuffer(b *buffer, oldRows, cols, rows int) {
	lines := b.live()
	for _, l := range lines {
		resizeLine(l, cols)
	}
	switch {
	case rows < oldRows:
		lines = lines[oldRows-rows:]
	case rows > oldRows:
		for i := oldRows; i < rows; i++ {
			lines = append(lines, newLine(cols, Cell{}))
		}
	}
	b.replace(append([]*Line(nil), lines...))
}

func resizeLine(l *Line, cols int) {
	n := len(l.Cells)
	if cols < n {
		for x := cols; x < n; x++ {
			if l.Cells[x].A&attrExt != 0 && l.ext != nil {
				delete(l.ext, x)
			}
		}
		l.Cells = l.Cells[:cols]
		if cols > 0 && l.Cells[cols-1].A&attrWide != 0 {
			l.set(cols-1, Cell{Bg: l.Cells[cols-1].Bg})
		}
		return
	}
	for x := n; x < cols; x++ {
		l.Cells = append(l.Cells, Cell{})
	}
}

// reflowBuffer rewraps the primary screen and history. Points are screen
// positions going in and coming out.
func (t *Terminal) reflowBuffer(b *buffer, oldCols, oldRows, cols, rows int, pts []reflowPoint) []reflowPoint {
	lines := b.live()
	base := len(lines) - oldRows

	// Absolute indexes of the tracked points.
	type tracked struct{ idx, x int }
	tp := make([]tracked, len(pts))
	maxIdx := base
	for i, p := range pts {
		tp[i] = tracked{base + p.y, p.x}
		maxIdx = max(maxIdx, base+p.y)
	}

	// Blank lines under the cursor are just unused screen: drop them so
	// shrinking the window doesn't push real output into history.
	end := len(lines)
	for end > maxIdx+1 && lines[end-1].isBlank() {
		end--
	}
	lines = lines[:end]

	if cols != oldCols {
		idx := make([]int, len(tp))
		xs := make([]int, len(tp))
		for i, p := range tp {
			idx[i], xs[i] = p.idx, p.x
		}
		lines = rewrap(lines, oldCols, cols, idx, xs)
		for i := range tp {
			tp[i] = tracked{idx[i], xs[i]}
		}
	} else {
		for _, l := range lines {
			resizeLine(l, cols)
		}
	}

	// Choose the new screen: the last `rows` lines, pulling history back
	// in when the window grew, but never letting the cursor fall off.
	total := len(lines)
	top := max(0, total-rows)
	if len(tp) > 0 && tp[0].idx < top {
		top = max(0, tp[0].idx-min(pts[0].y, rows-1))
		lines = lines[:min(total, top+rows)]
	}
	for len(lines)-top < rows {
		lines = append(lines, newLine(cols, Cell{}))
	}

	out := make([]reflowPoint, len(tp))
	for i, p := range tp {
		out[i] = reflowPoint{x: clamp(p.x, 0, cols-1), y: clamp(p.idx-top, 0, rows-1)}
	}

	// Anything above the new screen is history, subject to the limit.
	hist := top
	if drop := hist - t.scrollbackMax; drop > 0 {
		lines = lines[drop:]
		b.evicted += int64(drop)
	}
	b.replace(lines)
	return out
}

// rewrap joins soft-wrapped runs of lines and splits them again at the new
// width. idx/xs are tracked positions (line index, column), updated in place.
func rewrap(lines []*Line, oldCols, cols int, idx, xs []int) []*Line {
	out := make([]*Line, 0, len(lines))
	type cellRef struct {
		c   Cell
		ext *cellExt
	}
	var buf []cellRef
	for i := 0; i < len(lines); {
		j := i
		for j < len(lines)-1 && lines[j].Wrapped {
			j++
		}
		// Flatten the logical line, skipping padding left where a wide
		// character wrapped early, and remember where tracked points and
		// prompt marks sat in it.
		buf = buf[:0]
		type anchor struct {
			off   int
			which int
		}
		var anchors []anchor
		var marks []struct {
			off  int
			mark *PromptMark
		}
		var imgs []struct {
			off  int
			imgs []*Placement
		}
		for k := i; k <= j; k++ {
			l := lines[k]
			n := len(l.Cells)
			if k == j {
				n = l.contentEnd()
			}
			if l.Prompt != nil {
				marks = append(marks, struct {
					off  int
					mark *PromptMark
				}{len(buf), l.Prompt})
			}
			if len(l.Images) > 0 {
				imgs = append(imgs, struct {
					off  int
					imgs []*Placement
				}{len(buf), l.Images})
			}
			start := len(buf)
			for x := 0; x < n; x++ {
				c := l.Cells[x]
				if c.A&attrPadding != 0 {
					continue
				}
				buf = append(buf, cellRef{c, l.getExt(x)})
			}
			for w := range idx {
				if idx[w] == k {
					// Columns past the content (a cursor after a trailing
					// space) still count; buf is padded out to them below.
					anchors = append(anchors, anchor{start + xs[w], w})
				}
			}
		}
		// Make sure tracked points past the end have cells to land on.
		need := len(buf)
		for _, a := range anchors {
			need = max(need, a.off)
		}
		for len(buf) < need {
			buf = append(buf, cellRef{})
		}

		// Split into lines of the new width.
		first := len(out)
		line := newLine(cols, Cell{})
		x := 0
		lineOf := make([]int, len(buf)+1) // physical line index for each offset
		colOf := make([]int, len(buf)+1)
		for off := 0; off < len(buf); off++ {
			cr := buf[off]
			w := 1
			if cr.c.A&attrWide != 0 {
				w = 2
			}
			if cr.c.A&attrSpacer != 0 {
				// Placed along with its wide head.
				lineOf[off], colOf[off] = len(out), max(x-1, 0)
				continue
			}
			if x+w > cols {
				if w == 2 && x < cols {
					line.Cells[x] = Cell{A: attrPadding}
				}
				line.Wrapped = true
				out = append(out, line)
				line = newLine(cols, Cell{})
				x = 0
			}
			lineOf[off], colOf[off] = len(out), x
			line.Cells[x] = cr.c
			if cr.ext != nil {
				line.setExt(x, cr.ext)
			}
			if w == 2 && x+1 < cols {
				sp := cr.c
				sp.A = sp.A&^(attrWide|attrExt) | attrSpacer
				line.Cells[x+1] = sp
			}
			x += w
		}
		lineOf[len(buf)], colOf[len(buf)] = len(out), x
		out = append(out, line)

		for _, m := range marks {
			li := lineOf[min(m.off, len(buf))]
			if out[li].Prompt == nil {
				out[li].Prompt = m.mark
			}
		}
		// Images keep their column; they move down with the text above them.
		for _, im := range imgs {
			li := lineOf[min(im.off, len(buf))]
			out[li].Images = append(out[li].Images, im.imgs...)
		}
		for _, a := range anchors {
			li, cx := lineOf[a.off], colOf[a.off]
			if cx >= cols {
				// Just past the end of a full line: stay at its last
				// column (a pending wrap), like the terminal would.
				cx = cols - 1
			}
			idx[a.which], xs[a.which] = li, cx
		}
		if len(out) == first {
			out = append(out, newLine(cols, Cell{}))
		}
		i = j + 1
	}
	return out
}
