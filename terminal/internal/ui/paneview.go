package ui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"opal/terminal/internal/render"
	"opal/terminal/internal/vt"
)

// markInfo is a prompt mark visible in (or scrolled past) the view.
type markInfo struct {
	abs  int64
	mark *vt.PromptMark
}

// draw paints the pane: rows from the cache (rendering the ones that
// changed), then overlays.
func (p *Pane) draw(gtx layout.Context, focused bool) {
	w := p.win
	rend := w.rend
	m := rend.Metrics()
	p.frame++
	size := rend.Size(p.cols)

	type job struct {
		key uint64
		row render.Row
	}
	var jobs []job
	queued := map[uint64]bool{}
	keys := make([]uint64, p.rows)
	var marks []markInfo
	var hist, top int
	var firstAbs int64

	p.term.Lock()
	top = p.viewTopRow()
	hist = p.term.HistoryLen()
	firstAbs = p.term.FirstAbs()
	cx, cy := p.term.CursorPos()
	rev := p.term.ReverseVideo()
	rend.SetPalette(p.term.Palette())
	shape := p.cursorShape(focused, w.blinkOn)
	for y := 0; y < p.rows; y++ {
		idx := top + y
		line := p.term.LineAt(idx)
		if line == nil {
			continue
		}
		row := render.Row{Line: line, CursorX: -1, Reverse: rev}
		abs := firstAbs + int64(idx)
		row.SelFrom, row.SelTo = p.sel.columns(abs, len(line.Cells))
		if idx == hist+cy && shape != render.CursorNone {
			row.CursorX, row.CursorShape = cx, shape
		}
		if !w.blinkOn && hasBlink(line) {
			row.BlinkOff = true
		}
		k := rend.Key(&row)
		keys[y] = k
		if line.Prompt != nil {
			marks = append(marks, markInfo{abs, line.Prompt})
		}
		if _, ok := p.cache[k]; !ok && !queued[k] {
			queued[k] = true
			row.Line = line.Clone()
			jobs = append(jobs, job{k, row})
		}
	}
	allMarks := p.scrollbarMarks(hist)
	var imgs []imgDraw
	p.term.VisibleImages(top, p.rows, func(line int, pl *vt.Placement) {
		imgs = append(imgs, imgDraw{row: line - top, pl: pl})
	})
	p.term.Unlock()

	for _, j := range jobs {
		ri := p.newRowImage(size)
		rend.Draw(&j.row, ri.img)
		ri.op = paint.NewImageOp(ri.img)
		ri.op.Filter = paint.FilterNearest
		p.cache[j.key] = ri
	}
	for y, k := range keys {
		ri := p.cache[k]
		if ri == nil {
			continue
		}
		ri.used = p.frame
		off := op.Offset(p.grid.Add(image.Pt(0, y*m.CellH))).Push(gtx.Ops)
		cl := clip.Rect{Max: size}.Push(gtx.Ops)
		ri.op.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
		off.Pop()
	}
	p.evict()
	p.drawImages(gtx, imgs)

	p.drawGutter(gtx, marks, firstAbs, top)
	p.drawSearchHits(gtx, firstAbs, top)
	p.drawHover(gtx, firstAbs, top)
	p.drawScrollbar(gtx, hist, allMarks)
	if since := time.Since(p.bellAt); since < 150*time.Millisecond {
		fillRect(gtx, p.rect, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(40 * (1 - float64(since)/float64(150*time.Millisecond)))})
		gtx.Execute(op.InvalidateCmd{})
	}
}

type imgDraw struct {
	row int // view row of the image's top line (may be negative)
	pl  *vt.Placement
}

type imgEntry struct {
	op   paint.ImageOp
	used uint64
}

// drawImages paints inline images over the text, clipped to the grid.
func (p *Pane) drawImages(gtx layout.Context, imgs []imgDraw) {
	if len(imgs) == 0 {
		return
	}
	m := p.win.rend.Metrics()
	grid := image.Rectangle{Min: p.grid, Max: p.grid.Add(image.Pt(p.cols*m.CellW, p.rows*m.CellH))}
	cl := clip.Rect(grid).Push(gtx.Ops)
	defer cl.Pop()
	for _, d := range imgs {
		pl := d.pl
		src := pl.Src
		if src.Empty() {
			continue
		}
		w := float32(pl.Cols * m.CellW)
		h := float32(pl.Rows * m.CellH)
		if pl.W > 0 && pl.H > 0 {
			w, h = pl.W*float32(m.CellW), pl.H*float32(m.CellH)
		}
		x := float32(p.grid.X + pl.Col*m.CellW + pl.OffX)
		y := float32(p.grid.Y + d.row*m.CellH + pl.OffY)
		tr := f32.Affine2D{}.
			Offset(f32.Pt(-float32(src.Min.X), -float32(src.Min.Y))).
			Scale(f32.Point{}, f32.Pt(w/float32(src.Dx()), h/float32(src.Dy()))).
			Offset(f32.Pt(x, y))
		st := op.Affine(tr).Push(gtx.Ops)
		ic := clip.Rect(src).Push(gtx.Ops)
		p.imageOp(pl.Image).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		ic.Pop()
		st.Pop()
	}
}

// imageOp returns the GPU image for img, uploading it the first time.
func (p *Pane) imageOp(img *vt.Image) paint.ImageOp {
	if p.images == nil {
		p.images = map[uint64]*imgEntry{}
	}
	if e := p.images[img.Version]; e != nil {
		e.used = p.frame
		return e.op
	}
	o := paint.NewImageOp(img.Pix)
	o.Filter = paint.FilterLinear
	p.images[img.Version] = &imgEntry{op: o, used: p.frame}
	for v, e := range p.images {
		if e.used+120 < p.frame {
			delete(p.images, v)
		}
	}
	return o
}

func hasBlink(l *vt.Line) bool {
	for _, c := range l.Cells {
		if c.A&(vt.AttrBlink|vt.AttrRapidBlink) != 0 {
			return true
		}
	}
	return false
}

func (p *Pane) newRowImage(size image.Point) *rowImage {
	// Reuse an image retired at least two frames ago: by then the GPU has
	// uploaded whatever texture was made from it.
	for i, ri := range p.retired {
		if ri.used+2 < p.frame && ri.img.Rect.Size() == size {
			p.retired[i] = p.retired[len(p.retired)-1]
			p.retired = p.retired[:len(p.retired)-1]
			return &rowImage{img: ri.img}
		}
	}
	return &rowImage{img: image.NewRGBA(image.Rectangle{Max: size})}
}

// evict drops cached rows that weren't drawn recently.
func (p *Pane) evict() {
	if len(p.cache) <= 3*p.rows+8 {
		return
	}
	for k, ri := range p.cache {
		if ri.used+1 < p.frame {
			delete(p.cache, k)
			p.retired = append(p.retired, ri)
		}
	}
	if len(p.retired) > 2*p.rows+8 {
		p.retired = p.retired[len(p.retired)-(2*p.rows+8):]
	}
}

// scrollbarMarks lists every prompt in history+screen with its position.
// The caller holds the lock.
func (p *Pane) scrollbarMarks(hist int) []markInfo {
	if hist == 0 {
		return nil
	}
	var out []markInfo
	first := p.term.FirstAbs()
	for _, row := range p.term.PromptRows() {
		if l := p.term.AbsLine(row); l != nil {
			out = append(out, markInfo{row - first, l.Prompt})
		}
	}
	return out
}

// markColor picks a command's status color: green for success, red for
// failure, dim while running or unknown.
func (p *Pane) markColor(m *vt.PromptMark) color.NRGBA {
	c := p.win.chrome
	switch {
	case m.Aborted:
		return c.dim
	case !m.Finished.IsZero() && m.Exit == 0:
		return c.ok
	case !m.Finished.IsZero() && m.Exit > 0:
		return c.bad
	}
	return c.dim
}

// drawGutter marks prompt lines in the left padding with the command's
// status.
func (p *Pane) drawGutter(gtx layout.Context, marks []markInfo, firstAbs int64, top int) {
	m := p.win.rend.Metrics()
	pad := p.grid.X - p.rect.Min.X
	if pad < 3 {
		return
	}
	wBar := max(2, pad/3)
	for _, mk := range marks {
		y := int(mk.abs-firstAbs) - top
		if y < 0 || y >= p.rows {
			continue
		}
		col := p.markColor(mk.mark)
		x0 := p.rect.Min.X + (pad-wBar)/2
		y0 := p.grid.Y + y*m.CellH + 2
		fillRect(gtx, image.Rect(x0, y0, x0+wBar, y0+m.CellH-4), col)
	}
}

// drawScrollbar shows the position in history, with ticks for prompts.
func (p *Pane) drawScrollbar(gtx layout.Context, hist int, marks []markInfo) {
	if hist == 0 {
		return
	}
	c := p.win.chrome
	total := hist + p.rows
	h := p.rect.Dy()
	barW := max(3, p.win.dp(4))
	x0 := p.rect.Max.X - barW - 1
	thumbH := max(p.win.dp(16), h*p.rows/total)
	thumbY := p.rect.Min.Y + (h-thumbH)*(hist-p.scroll)/max(hist, 1)
	visible := p.scroll > 0 || p.hover.inScrollbar
	if visible {
		fillRect(gtx, image.Rect(x0, thumbY, x0+barW, thumbY+thumbH), c.scrollThumb)
	}
	for _, mk := range marks {
		if mk.mark.Finished.IsZero() || mk.mark.Exit == 0 {
			if !visible {
				continue
			}
		}
		y := p.rect.Min.Y + int(mk.abs)*h/total
		fillRect(gtx, image.Rect(x0-1, y, x0+barW+1, y+2), p.markColor(mk.mark))
	}
}

// drawSearchHits outlines matches in view; the current one is filled.
func (p *Pane) drawSearchHits(gtx layout.Context, firstAbs int64, top int) {
	s := p.search
	if s == nil || len(s.matches) == 0 {
		return
	}
	m := p.win.rend.Metrics()
	c := p.win.chrome
	viewFirst := firstAbs + int64(top)
	viewLast := viewFirst + int64(p.rows) - 1
	for i, mt := range s.matches {
		if mt.End.Row < viewFirst || mt.Start.Row > viewLast {
			continue
		}
		col := c.searchHit
		if i == s.current {
			col = c.searchCurrent
		}
		for row := mt.Start.Row; row <= mt.End.Row; row++ {
			if row < viewFirst || row > viewLast {
				continue
			}
			c0, c1 := 0, p.cols
			if row == mt.Start.Row {
				c0 = mt.Start.Col
			}
			if row == mt.End.Row {
				c1 = mt.End.Col + 1
			}
			y := p.grid.Y + int(row-viewFirst)*m.CellH
			fillRect(gtx, image.Rect(p.grid.X+c0*m.CellW, y, p.grid.X+c1*m.CellW, y+m.CellH), col)
		}
	}
}

// drawHover underlines the link under the mouse while Ctrl is held.
func (p *Pane) drawHover(gtx layout.Context, firstAbs int64, top int) {
	h := p.hover
	if !h.link || h.url == "" {
		return
	}
	m := p.win.rend.Metrics()
	viewFirst := firstAbs + int64(top)
	for row := h.from.Row; row <= h.to.Row; row++ {
		y := int(row - viewFirst)
		if y < 0 || y >= p.rows {
			continue
		}
		c0, c1 := 0, p.cols
		if row == h.from.Row {
			c0 = h.from.Col
		}
		if row == h.to.Row {
			c1 = h.to.Col + 1
		}
		yy := p.grid.Y + y*m.CellH + m.Underline
		fillRect(gtx, image.Rect(p.grid.X+c0*m.CellW, yy, p.grid.X+c1*m.CellW, yy+max(1, m.UnderlineThick)), p.win.chrome.accent)
	}
}

func fillRect(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	if r.Empty() || c.A == 0 {
		return
	}
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}
