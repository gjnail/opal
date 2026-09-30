package vt

import "time"

var nowFunc = time.Now

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lrActive reports whether left/right margins are narrower than the screen.
func (t *Terminal) lrActive() bool {
	return t.modes.get(modeLRMargins) && (t.left > 0 || t.right < t.cols-1)
}

func (t *Terminal) line(y int) *Line { return t.buf.screenLine(y, t.rows) }

// moveTo positions the cursor, honoring origin mode. x and y are 0-based.
func (t *Terminal) moveTo(x, y int) {
	if t.cur.origin {
		t.cur.x = clamp(x+t.left, t.left, t.right)
		t.cur.y = clamp(y+t.top, t.top, t.bottom)
	} else {
		t.cur.x = clamp(x, 0, t.cols-1)
		t.cur.y = clamp(y, 0, t.rows-1)
	}
	t.cur.pendingWrap = false
}

func (t *Terminal) cursorUp(n int) {
	lo := 0
	if t.cur.y >= t.top {
		lo = t.top
	}
	t.cur.y = clamp(t.cur.y-n, lo, t.rows-1)
	t.cur.pendingWrap = false
}

func (t *Terminal) cursorDown(n int) {
	hi := t.rows - 1
	if t.cur.y <= t.bottom {
		hi = t.bottom
	}
	t.cur.y = clamp(t.cur.y+n, 0, hi)
	t.cur.pendingWrap = false
}

func (t *Terminal) cursorForward(n int) {
	hi := t.cols - 1
	if t.cur.x <= t.right {
		hi = t.right
	}
	t.cur.x = clamp(t.cur.x+n, 0, hi)
	t.cur.pendingWrap = false
}

func (t *Terminal) cursorBack(n int) {
	lo := 0
	if t.cur.x >= t.left {
		lo = t.left
	}
	t.cur.x = clamp(t.cur.x-n, lo, t.cols-1)
	t.cur.pendingWrap = false
}

func (t *Terminal) carriageReturn() {
	if t.cur.x >= t.left {
		t.cur.x = t.left
	} else {
		t.cur.x = 0
	}
	t.cur.pendingWrap = false
}

func (t *Terminal) backspace() {
	if t.cur.pendingWrap {
		t.cur.pendingWrap = false
	}
	lo := 0
	if t.cur.x >= t.left {
		lo = t.left
	}
	if t.cur.x > lo {
		t.cur.x--
		return
	}
	// Reverse wraparound: back up onto the end of the previous line.
	if t.modes.get(modeReverseWrap) && t.modes.get(modeAutowrap) {
		top := 0
		if t.cur.y >= t.top {
			top = t.top
		}
		if t.cur.y > top {
			t.cur.y--
			t.cur.x = t.right
		}
	}
}

// index moves down one line, scrolling at the bottom margin (IND, LF).
func (t *Terminal) index() {
	t.cur.pendingWrap = false
	if t.cur.y == t.bottom {
		if !t.lrActive() || (t.cur.x >= t.left && t.cur.x <= t.right) {
			t.scrollUp(t.top, t.bottom, 1)
		}
		return
	}
	if t.cur.y < t.rows-1 {
		t.cur.y++
	}
}

// reverseIndex moves up one line, scrolling at the top margin (RI).
func (t *Terminal) reverseIndex() {
	t.cur.pendingWrap = false
	if t.cur.y == t.top {
		if !t.lrActive() || (t.cur.x >= t.left && t.cur.x <= t.right) {
			t.scrollDown(t.top, t.bottom, 1)
		}
		return
	}
	if t.cur.y > 0 {
		t.cur.y--
	}
}

func (t *Terminal) linefeed() {
	t.index()
	if t.modes.get(modeLineFeedNewLine) {
		t.carriageReturn()
	}
}

// scrollUp moves the content of rows top..bottom up by n. When the region
// starts at the top of the primary screen, lines leave into history.
func (t *Terminal) scrollUp(top, bottom, n int) {
	if n <= 0 {
		return
	}
	if n > bottom-top+1 {
		n = bottom - top + 1
	}
	t.seq++
	if t.lrActive() {
		t.scrollUpRect(top, bottom, t.left, t.right, n)
		return
	}
	fill := t.blank()
	if t.buf.history && top == 0 && t.scrollbackMax > 0 {
		t.images.scrolled(n)
		for i := 0; i < n; i++ {
			l := t.newLine(fill)
			if bottom == t.rows-1 {
				t.buf.push(l)
			} else {
				t.buf.insert(t.buf.screenBase(t.rows)+bottom+1, l)
			}
		}
		t.trimHistory()
		return
	}
	base := t.buf.screenBase(t.rows) + t.buf.start
	lines := t.buf.lines[base+top : base+bottom+1]
	for i := 0; i < n; i++ {
		t.recycle(lines[i])
	}
	copy(lines, lines[n:])
	for i := len(lines) - n; i < len(lines); i++ {
		lines[i] = t.newLine(fill)
	}
}

// scrollDown moves the content of rows top..bottom down by n.
func (t *Terminal) scrollDown(top, bottom, n int) {
	if n <= 0 {
		return
	}
	if n > bottom-top+1 {
		n = bottom - top + 1
	}
	t.seq++
	if t.lrActive() {
		t.scrollDownRect(top, bottom, t.left, t.right, n)
		return
	}
	fill := t.blank()
	base := t.buf.screenBase(t.rows) + t.buf.start
	lines := t.buf.lines[base+top : base+bottom+1]
	for i := len(lines) - n; i < len(lines); i++ {
		t.recycle(lines[i])
	}
	copy(lines[n:], lines[:len(lines)-n])
	for i := 0; i < n; i++ {
		lines[i] = t.newLine(fill)
	}
}

// scrollUpRect scrolls only columns left..right (DECLRMM regions).
func (t *Terminal) scrollUpRect(top, bottom, left, right, n int) {
	w := right - left + 1
	for y := top; y <= bottom; y++ {
		dst := t.line(y)
		if y+n <= bottom {
			src := t.line(y + n)
			t.copyCells(dst, left, src, left, w)
		} else {
			t.eraseCells(dst, left, right+1)
		}
	}
}

func (t *Terminal) scrollDownRect(top, bottom, left, right, n int) {
	w := right - left + 1
	for y := bottom; y >= top; y-- {
		dst := t.line(y)
		if y-n >= top {
			src := t.line(y - n)
			t.copyCells(dst, left, src, left, w)
		} else {
			t.eraseCells(dst, left, right+1)
		}
	}
}

// copyCells copies w cells between lines, carrying extra data.
func (t *Terminal) copyCells(dst *Line, dx int, src *Line, sx, w int) {
	if dst == src {
		dst.moveCells(dx, sx, w)
		return
	}
	for i := 0; i < w; i++ {
		dst.set(dx+i, src.Cells[sx+i])
		if src.Cells[sx+i].A&attrExt != 0 {
			dst.Cells[dx+i].A &^= attrExt
			dst.setExt(dx+i, src.getExt(sx+i))
		}
	}
	fixWideEdges(dst, dx, dx+w)
}

// eraseCells blanks [from, to) on a line with the erase cell.
func (t *Terminal) eraseCells(l *Line, from, to int) {
	fixWideEdges(l, from, to)
	l.fill(from, to, t.blank())
	t.images.eraseCells(t, l, from, to)
}

// fixWideEdges repairs double-width characters cut in half by an operation
// on [from, to): an orphaned left or right half becomes a blank.
func fixWideEdges(l *Line, from, to int) {
	if from > 0 && from < len(l.Cells) && l.Cells[from].A&attrSpacer != 0 {
		l.set(from-1, Cell{Bg: l.Cells[from-1].Bg})
		l.set(from, Cell{Bg: l.Cells[from].Bg})
	}
	if to > 0 && to < len(l.Cells) && l.Cells[to-1].A&attrWide != 0 {
		l.set(to, Cell{Bg: l.Cells[to].Bg})
	}
	if to > 0 && to <= len(l.Cells) && l.Cells[to-1].A&attrWide != 0 && (to == len(l.Cells)) {
		l.set(to-1, Cell{Bg: l.Cells[to-1].Bg})
	}
}

// trimHistory enforces the scrollback limit.
func (t *Terminal) trimHistory() {
	if extra := t.buf.count() - t.rows - t.scrollbackMax; extra > 0 {
		t.buf.dropFront(extra, t.recycle)
		t.images.evicted(t.buf.evicted)
	}
}

// eraseDisplay implements ED (and DECSED when selective).
func (t *Terminal) eraseDisplay(mode int, selective bool) {
	t.seq++
	x, y := t.cur.x, t.cur.y
	switch mode {
	case 0:
		t.eraseLineRange(y, x, t.cols, selective)
		for i := y + 1; i < t.rows; i++ {
			t.eraseLineRange(i, 0, t.cols, selective)
		}
	case 1:
		for i := 0; i < y; i++ {
			t.eraseLineRange(i, 0, t.cols, selective)
		}
		t.eraseLineRange(y, 0, x+1, selective)
	case 2:
		// On the primary screen, keep what was there by scrolling it into
		// history instead of wiping it: `clear` shouldn't destroy output
		// you might want to scroll back to.
		if !selective && t.buf.history && t.scrollbackMax > 0 && !t.lrActive() {
			last := -1
			for i := t.rows - 1; i >= 0; i-- {
				if !t.line(i).isBlank() {
					last = i
					break
				}
			}
			if last >= 0 {
				savedTop, savedBottom := t.top, t.bottom
				t.scrollUp(0, t.rows-1, last+1)
				t.top, t.bottom = savedTop, savedBottom
			}
		}
		for i := 0; i < t.rows; i++ {
			t.eraseLineRange(i, 0, t.cols, selective)
		}
	case 3:
		t.clearHistory()
	}
	t.cur.pendingWrap = false
}

// ClearScrollback drops the history above the screen.
func (t *Terminal) ClearScrollback() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearHistory()
	t.seq++
}

// clearHistory drops all scrollback (ED 3).
func (t *Terminal) clearHistory() {
	if t.buf.count() > t.rows {
		n := t.buf.count() - t.rows
		t.buf.dropFront(n, t.recycle)
		t.images.evicted(t.buf.evicted)
		t.generation++
	}
}

// eraseLine implements EL (and DECSEL when selective).
func (t *Terminal) eraseLine(mode int, selective bool) {
	t.seq++
	switch mode {
	case 0:
		t.eraseLineRange(t.cur.y, t.cur.x, t.cols, selective)
	case 1:
		t.eraseLineRange(t.cur.y, 0, t.cur.x+1, selective)
	case 2:
		t.eraseLineRange(t.cur.y, 0, t.cols, selective)
	}
	t.cur.pendingWrap = false
}

func (t *Terminal) eraseLineRange(y, from, to int, selective bool) {
	l := t.line(y)
	if !selective {
		t.eraseCells(l, from, to)
		if from == 0 && to >= t.cols {
			l.Wrapped = false
			if y > 0 {
				// The previous line can't wrap into a line that no longer
				// holds its continuation.
				t.line(y - 1).Wrapped = false
			}
		}
		return
	}
	fixWideEdges(l, from, to)
	blank := t.blank()
	for x := max(from, 0); x < min(to, len(l.Cells)); x++ {
		if l.Cells[x].A&AttrProtected == 0 {
			l.set(x, blank)
		}
	}
}

// eraseChars implements ECH: blank n cells from the cursor.
func (t *Terminal) eraseChars(n int) {
	t.seq++
	l := t.line(t.cur.y)
	t.eraseCells(l, t.cur.x, min(t.cur.x+n, t.cols))
	t.cur.pendingWrap = false
}

// insertChars implements ICH within the right margin.
func (t *Terminal) insertChars(n int) {
	x := t.cur.x
	right := t.cols - 1
	if t.modes.get(modeLRMargins) {
		if x < t.left || x > t.right {
			return
		}
		right = t.right
	}
	t.seq++
	l := t.line(t.cur.y)
	w := right - x + 1
	if n > w {
		n = w
	}
	fixWideEdges(l, x, right+1)
	l.moveCells(x+n, x, w-n)
	l.fill(x, x+n, t.blank())
	fixWideEdges(l, right+1, right+1)
	if right+1 <= len(l.Cells) && right >= 0 && l.Cells[right].A&attrWide != 0 {
		l.set(right, Cell{Bg: l.Cells[right].Bg})
	}
	t.cur.pendingWrap = false
}

// deleteChars implements DCH within the right margin.
func (t *Terminal) deleteChars(n int) {
	x := t.cur.x
	right := t.cols - 1
	if t.modes.get(modeLRMargins) {
		if x < t.left || x > t.right {
			return
		}
		right = t.right
	}
	t.seq++
	l := t.line(t.cur.y)
	w := right - x + 1
	if n > w {
		n = w
	}
	fixWideEdges(l, x, x+n)
	fixWideEdges(l, right+1, right+1)
	l.moveCells(x, x+n, w-n)
	l.fill(right+1-n, right+1, t.blank())
	if x < len(l.Cells) && l.Cells[x].A&attrSpacer != 0 {
		l.set(x, Cell{Bg: l.Cells[x].Bg})
	}
	t.cur.pendingWrap = false
}

// insertLines implements IL: blank lines pushed in at the cursor.
func (t *Terminal) insertLines(n int) {
	if t.cur.y < t.top || t.cur.y > t.bottom {
		return
	}
	if t.lrActive() {
		if t.cur.x < t.left || t.cur.x > t.right {
			return
		}
	}
	t.scrollDown(t.cur.y, t.bottom, n)
	t.carriageReturn()
}

// deleteLines implements DL.
func (t *Terminal) deleteLines(n int) {
	if t.cur.y < t.top || t.cur.y > t.bottom {
		return
	}
	if t.lrActive() {
		if t.cur.x < t.left || t.cur.x > t.right {
			return
		}
	}
	// Deleted lines never go to history.
	hist := t.buf.history
	t.buf.history = false
	t.scrollUp(t.cur.y, t.bottom, n)
	t.buf.history = hist
	t.carriageReturn()
}

// saveCursor implements DECSC.
func (t *Terminal) saveCursor() {
	c := &t.cur
	t.buf.saved = savedCursor{
		valid: true, x: c.x, y: c.y, pendingWrap: c.pendingWrap, pen: c.pen,
		charsets: c.charsets, gl: c.gl, origin: c.origin,
	}
}

// restoreCursor implements DECRC. Without a saved state it homes the
// cursor and resets the pen, as the VT510 manual says.
func (t *Terminal) restoreCursor() {
	s := t.buf.saved
	if !s.valid {
		t.cur.pen = pen{}
		t.cur.resetCharsets()
		t.cur.origin = false
		t.modes.set(modeOrigin, false)
		t.moveTo(0, 0)
		return
	}
	t.cur.pen = s.pen
	t.cur.charsets = s.charsets
	t.cur.gl = s.gl
	t.cur.origin = s.origin
	t.modes.set(modeOrigin, s.origin)
	t.cur.x = clamp(s.x, 0, t.cols-1)
	t.cur.y = clamp(s.y, 0, t.rows-1)
	t.cur.pendingWrap = s.pendingWrap
}

// switchScreen moves between the primary and alternate screens.
func (t *Terminal) switchScreen(alt, clearOnSwitch, clearOnEnter bool) {
	target := t.primary
	if alt {
		target = t.alt
	}
	if t.buf == target {
		return
	}
	t.seq++
	if !alt && clearOnSwitch {
		// 1047: clear the alternate screen when leaving it.
		t.clearBuffer(t.alt)
	}
	t.buf = target
	if alt && clearOnEnter {
		t.clearBuffer(t.alt)
	}
	t.lastValid = false
	t.images.screenSwitched(alt)
}

func (t *Terminal) clearBuffer(b *buffer) {
	fill := t.blank()
	for i := 0; i < b.count(); i++ {
		b.line(i).reset(t.cols, fill)
	}
}

// tabForward moves to the nth next tab stop.
func (t *Terminal) tabForward(n int) {
	right := t.cols - 1
	if t.cur.x <= t.right {
		right = t.right
	}
	for ; n > 0 && t.cur.x < right; n-- {
		t.cur.x++
		for t.cur.x < right && !t.tabs[t.cur.x] {
			t.cur.x++
		}
	}
	t.cur.pendingWrap = false
}

func (t *Terminal) tabBack(n int) {
	left := 0
	if t.cur.x >= t.left {
		left = t.left
	}
	for ; n > 0 && t.cur.x > left; n-- {
		t.cur.x--
		for t.cur.x > left && !t.tabs[t.cur.x] {
			t.cur.x--
		}
	}
	t.cur.pendingWrap = false
}

// setScrollRegion implements DECSTBM (1-based, inclusive).
func (t *Terminal) setScrollRegion(top, bottom int) {
	if bottom == 0 || bottom > t.rows {
		bottom = t.rows
	}
	if top == 0 {
		top = 1
	}
	if top >= bottom {
		return
	}
	t.top, t.bottom = top-1, bottom-1
	t.moveTo(0, 0)
}

// setLRMargins implements DECSLRM.
func (t *Terminal) setLRMargins(left, right int) {
	if right == 0 || right > t.cols {
		right = t.cols
	}
	if left == 0 {
		left = 1
	}
	if left >= right {
		return
	}
	t.left, t.right = left-1, right-1
	t.moveTo(0, 0)
}

// fullReset implements RIS.
func (t *Terminal) fullReset() {
	t.primary = newBuffer(t.cols, t.rows, true)
	t.alt = newBuffer(t.cols, t.rows, false)
	t.buf = t.primary
	t.resetState()
	t.images.reset()
	t.title, t.iconTitle = "", ""
	t.emit(EvTitle{""})
	t.generation++
	t.seq++
}

// softReset implements DECSTR.
func (t *Terminal) softReset() {
	t.modes.set(modeCursorVisible, true)
	t.modes.set(modeInsert, false)
	t.modes.set(modeOrigin, false)
	t.modes.set(modeAutowrap, true)
	t.modes.set(modeKeypadApp, false)
	t.modes.set(modeCursorKeys, false)
	t.modes.set(modeLRMargins, false)
	t.cur.origin = false
	t.top, t.bottom = 0, t.rows-1
	t.left, t.right = 0, t.cols-1
	t.cur.pen = pen{}
	t.cur.resetCharsets()
	t.cur.pendingWrap = false
	t.buf.saved = savedCursor{}
	t.cursorStyle = CursorDefault
}

// alignmentTest implements DECALN: fill the screen with E.
func (t *Terminal) alignmentTest() {
	t.top, t.bottom = 0, t.rows-1
	t.left, t.right = 0, t.cols-1
	t.modes.set(modeLRMargins, false)
	for y := 0; y < t.rows; y++ {
		l := t.line(y)
		l.reset(t.cols, Cell{R: 'E'})
	}
	t.cur.origin = false
	t.modes.set(modeOrigin, false)
	t.moveTo(0, 0)
	t.seq++
}
