// Package render rasterizes terminal rows into RGBA images on the CPU.
// Each row is drawn independently so the UI can cache rows by content and
// only redraw the ones that changed.
package render

import (
	"encoding/binary"
	"hash/maphash"
	"image"
	"unsafe"

	"opal/terminal/internal/fonts"
	"opal/terminal/internal/vt"
)

// Options change how rows are drawn.
type Options struct {
	// BoldIsBright maps bold text in colors 0-7 to 8-15, as older
	// terminals did.
	BoldIsBright bool
	// MinContrast lifts text whose contrast ratio against its background
	// falls below this (1 disables; 4.5 is WCAG AA).
	MinContrast float64
	// TransparentBG leaves default-background cells transparent so a
	// translucent window shows through.
	TransparentBG bool
}

// CursorShape is how the cursor is drawn.
type CursorShape uint8

const (
	CursorNone CursorShape = iota
	CursorBlock
	CursorHollow // unfocused windows
	CursorBar
	CursorUnderline
)

// Row is everything needed to draw one line.
type Row struct {
	Line *vt.Line
	// Selected columns [SelFrom, SelTo).
	SelFrom, SelTo int
	// Cursor column, or -1.
	CursorX     int
	CursorShape CursorShape
	// BlinkOff hides blinking text (the "off" half of the blink cycle).
	BlinkOff bool
	// Reverse is DECSCNM: the whole screen in inverse video.
	Reverse bool
}

// Renderer draws rows with one font set and palette.
type Renderer struct {
	fonts *fonts.Set
	m     fonts.Metrics
	opts  Options
	pal   vt.Palette
	boxes map[boxKey]*image.Alpha
	seed  maphash.Seed

	fg, bg []vt.RGB // per-cell scratch
}

// New creates a renderer.
func New(f *fonts.Set, pal vt.Palette, opts Options) *Renderer {
	if opts.MinContrast < 1 {
		opts.MinContrast = 1
	}
	return &Renderer{fonts: f, m: f.Metrics(), opts: opts, pal: pal, boxes: map[boxKey]*image.Alpha{}, seed: maphash.MakeSeed()}
}

// Metrics returns the cell metrics.
func (r *Renderer) Metrics() fonts.Metrics { return r.m }

// SetPalette changes colors. Cached rows keyed with the old palette miss.
func (r *Renderer) SetPalette(p vt.Palette) { r.pal = p }

// Key identifies a row's appearance, for caching rendered rows.
func (r *Renderer) Key(row *Row) uint64 {
	var h maphash.Hash
	h.SetSeed(r.seed)
	l := row.Line
	if len(l.Cells) > 0 {
		b := unsafe.Slice((*byte)(unsafe.Pointer(&l.Cells[0])), len(l.Cells)*int(unsafe.Sizeof(vt.Cell{})))
		h.Write(b)
	}
	for x, c := range l.Cells {
		if c.HasExt() {
			h.WriteString(l.Text(x))
			var buf [8]byte
			binary.LittleEndian.PutUint32(buf[:], uint32(l.UnderlineColor(x)))
			h.Write(buf[:4])
		}
	}
	var buf [32]byte
	binary.LittleEndian.PutUint32(buf[0:], uint32(row.SelFrom))
	binary.LittleEndian.PutUint32(buf[4:], uint32(row.SelTo))
	binary.LittleEndian.PutUint32(buf[8:], uint32(row.CursorX))
	buf[12] = byte(row.CursorShape)
	buf[13] = boolByte(row.BlinkOff)
	buf[14] = boolByte(row.Reverse)
	buf[15] = byte(l.Attr)
	h.Write(buf[:16])
	pb := unsafe.Slice((*byte)(unsafe.Pointer(&r.pal)), unsafe.Sizeof(r.pal))
	h.Write(pb)
	return h.Sum64()
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// Size returns the pixel size of a row with cols cells.
func (r *Renderer) Size(cols int) image.Point {
	return image.Pt(cols*r.m.CellW, r.m.CellH)
}

// Draw renders row into dst, which must be at least Size(len(cells)).
func (r *Renderer) Draw(row *Row, dst *image.RGBA) {
	l := row.Line
	if l.Attr != vt.LineNormal {
		r.drawDoubled(row, dst)
		return
	}
	r.drawCells(row, dst)
}

func (r *Renderer) drawCells(row *Row, dst *image.RGBA) {
	l := row.Line
	n := len(l.Cells)
	cw, ch := r.m.CellW, r.m.CellH
	if cap(r.fg) < n {
		r.fg, r.bg = make([]vt.RGB, n), make([]vt.RGB, n)
	}
	fg, bg := r.fg[:n], r.bg[:n]
	defaultBG := make([]bool, n)
	for x := 0; x < n; x++ {
		c := l.Cells[x]
		if c.Spacer() && x > 0 {
			c = l.Cells[x-1] // the right half looks like the left half
		}
		sel := x >= row.SelFrom && x < row.SelTo
		cur := row.CursorShape == CursorBlock && (x == row.CursorX || (c.Spacer() && x-1 == row.CursorX))
		fg[x], bg[x], defaultBG[x] = r.colors(c, row.Reverse, sel, cur)
	}

	// Backgrounds, merged into runs.
	bgPal := r.pal.Background
	if row.Reverse {
		bgPal = r.pal.Foreground
	}
	for x := 0; x < n; {
		e := x + 1
		for e < n && bg[e] == bg[x] && defaultBG[e] == defaultBG[x] {
			e++
		}
		rect := image.Rect(x*cw, 0, e*cw, ch)
		if defaultBG[x] && r.opts.TransparentBG {
			fillRect(dst, rect, vt.RGB{}, 0)
		} else if defaultBG[x] {
			fillRect(dst, rect, bgPal, 255)
		} else {
			fillRect(dst, rect, bg[x], 255)
		}
		x = e
	}

	// Text.
	for x := 0; x < n; x++ {
		c := l.Cells[x]
		if c.Spacer() || c.A&vt.AttrHidden != 0 {
			continue
		}
		if row.BlinkOff && c.A&(vt.AttrBlink|vt.AttrRapidBlink) != 0 {
			continue
		}
		cells := 1
		if c.Wide() {
			cells = 2
		}
		if c.R == 0 || (c.R == ' ' && !c.HasExt()) {
			continue
		}
		ox := x * cw
		if mask := r.boxGlyph(c.R, cells); mask != nil {
			blendMask(dst, mask, image.Pt(ox, 0), fg[x])
			continue
		}
		var st fonts.Style
		if c.A&vt.AttrBold != 0 {
			st |= fonts.Bold
		}
		if c.A&vt.AttrItalic != 0 {
			st |= fonts.Italic
		}
		g := r.fonts.Glyph(l.Text(x), st, cells)
		if g.Empty() {
			continue
		}
		if g.Mask != nil {
			blendMask(dst, g.Mask, image.Pt(ox+g.X, g.Y), fg[x])
		} else {
			blendColor(dst, g.Color, image.Pt(ox+g.X, g.Y))
		}
	}

	// Decorations.
	for x := 0; x < n; x++ {
		c := l.Cells[x]
		if c.A&vt.AttrHidden != 0 {
			continue
		}
		ul := c.A.Underline()
		if ul == vt.UnderlineNone && c.A&(vt.AttrStrike|vt.AttrOverline) == 0 {
			continue
		}
		col := fg[x]
		x0, x1 := x*cw, (x+1)*cw
		if ul != vt.UnderlineNone {
			ucol := col
			hx := x
			if c.Spacer() && x > 0 {
				hx = x - 1
			}
			if uc := l.UnderlineColor(hx); !uc.IsDefault() {
				ucol = r.resolve(uc, col)
			}
			r.underline(dst, ul, x0, x1, ucol)
		}
		if c.A&vt.AttrStrike != 0 {
			fillRect(dst, image.Rect(x0, r.m.Strike, x1, r.m.Strike+r.m.StrikeThick), col, 255)
		}
		if c.A&vt.AttrOverline != 0 {
			fillRect(dst, image.Rect(x0, 0, x1, r.m.UnderlineThick), col, 255)
		}
	}

	// Non-block cursors draw over everything.
	if row.CursorX >= 0 && row.CursorX < n {
		x0 := row.CursorX * cw
		w := cw
		if l.Cells[row.CursorX].Wide() {
			w *= 2
		}
		cc := r.pal.Cursor
		t := max(1, r.m.UnderlineThick, cw/8)
		switch row.CursorShape {
		case CursorBar:
			fillRect(dst, image.Rect(x0, 0, x0+max(2, t), ch), cc, 255)
		case CursorUnderline:
			fillRect(dst, image.Rect(x0, ch-max(2, t), x0+w, ch), cc, 255)
		case CursorHollow:
			strokeRect(dst, image.Rect(x0, 0, x0+w, ch), 1, cc)
		}
	}
}

// underline draws one of the SGR 4:n styles across [x0, x1).
func (r *Renderer) underline(dst *image.RGBA, style, x0, x1 int, col vt.RGB) {
	y := r.m.Underline
	t := r.m.UnderlineThick
	ch := r.m.CellH
	switch style {
	case vt.UnderlineSingle:
		fillRect(dst, image.Rect(x0, y, x1, y+t), col, 255)
	case vt.UnderlineDouble:
		y2 := min(y+2*t+1, ch-t)
		if y2 <= y+t {
			y = max(0, y2-2*t-1)
		}
		fillRect(dst, image.Rect(x0, y, x1, y+t), col, 255)
		fillRect(dst, image.Rect(x0, y2, x1, y2+t), col, 255)
	case vt.UnderlineDotted:
		for x := x0; x < x1; x += 2 * t {
			fillRect(dst, image.Rect(x, y, min(x+t, x1), y+t), col, 255)
		}
	case vt.UnderlineDashed:
		dash := max(2, r.m.CellW/2-1)
		for x := x0; x < x1; x += 2 * dash {
			fillRect(dst, image.Rect(x, y, min(x+dash, x1), y+t), col, 255)
		}
	case vt.UnderlineCurly:
		r.curly(dst, x0, x1, col)
	}
}

// curly draws a wavy underline. Its phase follows the absolute x
// position, so the waves of neighbouring cells line up.
func (r *Renderer) curly(dst *image.RGBA, x0, x1 int, col vt.RGB) {
	amp := float64(max(1, r.m.UnderlineThick))
	mid := float64(r.m.Underline) + amp/2
	if mid+amp+1 > float64(r.m.CellH) {
		mid = float64(r.m.CellH) - amp - 1
	}
	period := float64(r.m.CellW)
	half := float64(r.m.UnderlineThick)/2 + 0.35
	for x := x0; x < x1; x++ {
		// Phase is taken from the absolute x so neighbours line up.
		yc := mid + amp*sin2pi(float64(x)/period)
		for y := int(yc - half - 1); y <= int(yc+half+1); y++ {
			d := abs(float64(y) + 0.5 - yc)
			a := clamp01(half + 0.5 - d)
			if a > 0 {
				blendPixel(dst, x, y, col, uint8(a*255))
			}
		}
	}
}

// drawDoubled handles DECDWL/DECDHL lines: render the left half of the
// line normally, then stretch it.
func (r *Renderer) drawDoubled(row *Row, dst *image.RGBA) {
	l := row.Line
	half := (len(l.Cells) + 1) / 2
	sub := *row
	sub.Line = &vt.Line{Cells: l.Cells[:half]}
	for x := 0; x < half; x++ {
		if l.Cells[x].HasExt() {
			sub.Line = l.Clone()
			sub.Line.Cells = sub.Line.Cells[:half]
			break
		}
	}
	sub.Line.Attr = vt.LineNormal
	tmp := image.NewRGBA(image.Rect(0, 0, half*r.m.CellW, r.m.CellH))
	r.drawCells(&sub, tmp)
	w, h := dst.Rect.Dx(), r.m.CellH
	for y := 0; y < h; y++ {
		sy := y
		switch l.Attr {
		case vt.LineDoubleTop:
			sy = y / 2
		case vt.LineDoubleBottom:
			sy = (y + h) / 2
		}
		for x := 0; x < w; x++ {
			sx := x / 2
			if sx >= tmp.Rect.Dx() {
				break
			}
			si := tmp.PixOffset(sx, sy)
			di := dst.PixOffset(dst.Rect.Min.X+x, dst.Rect.Min.Y+y)
			copy(dst.Pix[di:di+4], tmp.Pix[si:si+4])
		}
	}
}
