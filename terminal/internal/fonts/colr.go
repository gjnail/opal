package fonts

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sort"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/vector"
)

func tablesGID(g font.GID) tables.GlyphID { return tables.GlyphID(g) }

func f214(c tables.Coord) float32 { return float32(c) / 16384 }

// colr renders a COLR paint tree (version 0 layers or the version 1 paint
// graph) into an RGBA image in pixel space.
type colr struct {
	face  *font.Face
	pal   []tables.ColorRecord
	rect  image.Rectangle // canvas bounds in pixel space
	depth int
}

// renderCOLR draws paint with m mapping font units to pixels.
func renderCOLR(face *font.Face, paint tables.PaintTable, m affine) *image.RGBA {
	c := &colr{face: face}
	if len(face.CPAL) > 0 {
		c.pal = face.CPAL[0]
	}
	b, ok := c.bounds(paint, m, 0)
	if !ok || b.Empty() {
		return nil
	}
	c.rect = b
	dst := image.NewRGBA(b)
	c.paint(paint, m, nil, dst)
	return dst
}

// bounds is the pixel box of every outline the paint tree uses.
func (c *colr) bounds(p tables.PaintTable, m affine, depth int) (image.Rectangle, bool) {
	if depth > 32 {
		return image.Rectangle{}, false
	}
	var out image.Rectangle
	add := func(r image.Rectangle, ok bool) {
		if ok {
			out = out.Union(r)
		}
	}
	glyphBox := func(gid tables.GlyphID, m affine) (image.Rectangle, bool) {
		o, ok := c.face.GlyphDataOutline(font.GID(gid))
		if !ok {
			return image.Rectangle{}, false
		}
		x0, y0, x1, y1, ok := outlineBounds(o.Segments, m)
		if !ok {
			return image.Rectangle{}, false
		}
		return image.Rect(int(math.Floor(float64(x0))), int(math.Floor(float64(y0))),
			int(math.Ceil(float64(x1))), int(math.Ceil(float64(y1)))), true
	}
	switch p := p.(type) {
	case tables.PaintColrLayersResolved:
		for _, l := range p {
			add(glyphBox(l.GlyphID, m))
		}
	case tables.PaintColrLayers:
		if layers, err := c.face.COLR.LayerList.Resolve(p); err == nil {
			for _, l := range layers {
				add(c.bounds(l, m, depth+1))
			}
		}
	case tables.PaintGlyph:
		add(glyphBox(p.GlyphID, m))
	case tables.PaintColrGlyph:
		if sub, ok := c.face.COLR.Search(p.GlyphID); ok {
			add(c.bounds(sub, m, depth+1))
		}
	case tables.PaintComposite:
		add(c.bounds(p.SourcePaint, m, depth+1))
		add(c.bounds(p.BackdropPaint, m, depth+1))
	default:
		if child, t, ok := transformOf(p); ok {
			add(c.bounds(child, m.mul(t), depth+1))
		}
	}
	return out, !out.Empty()
}

// transformOf unwraps the transform paints into (child, matrix in font
// units). Variable versions are drawn at their default instance.
func transformOf(p tables.PaintTable) (tables.PaintTable, affine, bool) {
	id := affine{a: 1, d: 1}
	around := func(t affine, cx, cy int16) affine {
		x, y := float32(cx), float32(cy)
		return affine{a: 1, d: 1, e: x, f: y}.mul(t).mul(affine{a: 1, d: 1, e: -x, f: -y})
	}
	scale := func(sx, sy float32) affine { return affine{a: sx, d: sy} }
	rot := func(turns float32) affine {
		a := float64(turns) * math.Pi
		s, cs := float32(math.Sin(a)), float32(math.Cos(a))
		return affine{a: cs, b: s, c: -s, d: cs}
	}
	skew := func(xa, ya float32) affine {
		return affine{a: 1, b: float32(math.Tan(float64(ya) * math.Pi)), c: -float32(math.Tan(float64(xa) * math.Pi)), d: 1}
	}
	switch p := p.(type) {
	case tables.PaintTransform:
		t := p.Transform
		return p.Paint, affine{a: t.Xx, b: t.Yx, c: t.Xy, d: t.Yy, e: t.Dx, f: t.Dy}, true
	case tables.PaintVarTransform:
		t := p.Transform
		return p.Paint, affine{a: t.Xx, b: t.Yx, c: t.Xy, d: t.Yy, e: t.Dx, f: t.Dy}, true
	case tables.PaintTranslate:
		return p.Paint, affine{a: 1, d: 1, e: float32(p.Dx), f: float32(p.Dy)}, true
	case tables.PaintVarTranslate:
		return p.Paint, affine{a: 1, d: 1, e: float32(p.Dx), f: float32(p.Dy)}, true
	case tables.PaintScale:
		return p.Paint, scale(f214(p.ScaleX), f214(p.ScaleY)), true
	case tables.PaintVarScale:
		return p.Paint, scale(f214(p.ScaleX), f214(p.ScaleY)), true
	case tables.PaintScaleAroundCenter:
		return p.Paint, around(scale(f214(p.ScaleX), f214(p.ScaleY)), p.CenterX, p.CenterY), true
	case tables.PaintVarScaleAroundCenter:
		return p.Paint, around(scale(f214(p.ScaleX), f214(p.ScaleY)), p.CenterX, p.CenterY), true
	case tables.PaintScaleUniform:
		return p.Paint, scale(f214(p.Scale), f214(p.Scale)), true
	case tables.PaintVarScaleUniform:
		return p.Paint, scale(f214(p.Scale), f214(p.Scale)), true
	case tables.PaintScaleUniformAroundCenter:
		return p.Paint, around(scale(f214(p.Scale), f214(p.Scale)), p.CenterX, p.CenterY), true
	case tables.PaintVarScaleUniformAroundCenter:
		return p.Paint, around(scale(f214(p.Scale), f214(p.Scale)), p.CenterX, p.CenterY), true
	case tables.PaintRotate:
		return p.Paint, rot(f214(p.Angle)), true
	case tables.PaintVarRotate:
		return p.Paint, rot(f214(p.Angle)), true
	case tables.PaintRotateAroundCenter:
		return p.Paint, around(rot(f214(p.Angle)), p.CenterX, p.CenterY), true
	case tables.PaintVarRotateAroundCenter:
		return p.Paint, around(rot(f214(p.Angle)), p.CenterX, p.CenterY), true
	case tables.PaintSkew:
		return p.Paint, skew(f214(p.XSkewAngle), f214(p.YSkewAngle)), true
	case tables.PaintVarSkew:
		return p.Paint, skew(f214(p.XSkewAngle), f214(p.YSkewAngle)), true
	case tables.PaintSkewAroundCenter:
		return p.Paint, around(skew(f214(p.XSkewAngle), f214(p.YSkewAngle)), p.CenterX, p.CenterY), true
	case tables.PaintVarSkewAroundCenter:
		return p.Paint, around(skew(f214(p.XSkewAngle), f214(p.YSkewAngle)), p.CenterX, p.CenterY), true
	}
	return nil, id, false
}

// glyphMask rasterizes an outline over the canvas, intersected with clip.
func (c *colr) glyphMask(gid tables.GlyphID, m affine, clip *image.Alpha) *image.Alpha {
	o, ok := c.face.GlyphDataOutline(font.GID(gid))
	if !ok {
		return nil
	}
	w, h := c.rect.Dx(), c.rect.Dy()
	r := vector.NewRasterizer(w, h)
	drawOutline(r, o.Segments, m, float32(c.rect.Min.X), float32(c.rect.Min.Y))
	mask := image.NewAlpha(c.rect)
	r.Draw(mask, mask.Rect, image.Opaque, image.Point{})
	if clip != nil {
		for i := range mask.Pix {
			mask.Pix[i] = uint8(uint32(mask.Pix[i]) * uint32(clip.Pix[i]) / 255)
		}
	}
	return mask
}

// color resolves a palette entry; 0xFFFF means the text color, which we
// don't know here, so emoji fonts get a light neutral.
func (c *colr) color(idx uint16, alpha float32) [4]float32 {
	var r, g, b, a float32 = 0.85, 0.85, 0.85, 1
	if int(idx) < len(c.pal) {
		cr := c.pal[idx]
		r, g, b, a = float32(cr.Red)/255, float32(cr.Green)/255, float32(cr.Blue)/255, float32(cr.Alpha)/255
	}
	a *= alpha
	return [4]float32{r * a, g * a, b * a, a} // premultiplied
}

// fillMask composites a per-pixel color source over dst through mask.
func fillMask(dst *image.RGBA, mask *image.Alpha, src func(x, y int) [4]float32) {
	b := dst.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cov := float32(255)
			if mask != nil {
				cov = float32(mask.Pix[mask.PixOffset(x, y)])
				if cov == 0 {
					continue
				}
			}
			col := src(x, y)
			k := cov / 255
			sa := col[3] * k
			if sa <= 0 {
				continue
			}
			i := dst.PixOffset(x, y)
			inv := 1 - sa
			dst.Pix[i] = clamp8(col[0]*k*255 + float32(dst.Pix[i])*inv)
			dst.Pix[i+1] = clamp8(col[1]*k*255 + float32(dst.Pix[i+1])*inv)
			dst.Pix[i+2] = clamp8(col[2]*k*255 + float32(dst.Pix[i+2])*inv)
			dst.Pix[i+3] = clamp8(sa*255 + float32(dst.Pix[i+3])*inv)
		}
	}
}

func clamp8(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func (c *colr) paint(p tables.PaintTable, m affine, clip *image.Alpha, dst *image.RGBA) {
	c.depth++
	defer func() { c.depth-- }()
	if c.depth > 32 {
		return
	}
	switch p := p.(type) {
	case tables.PaintColrLayersResolved:
		for _, l := range p {
			if mask := c.glyphMask(l.GlyphID, m, clip); mask != nil {
				col := c.color(l.PaletteIndex, 1)
				fillMask(dst, mask, func(int, int) [4]float32 { return col })
			}
		}
	case tables.PaintColrLayers:
		layers, err := c.face.COLR.LayerList.Resolve(p)
		if err != nil {
			return
		}
		for _, l := range layers {
			c.paint(l, m, clip, dst)
		}
	case tables.PaintGlyph:
		if mask := c.glyphMask(p.GlyphID, m, clip); mask != nil {
			c.paint(p.Paint, m, mask, dst)
		}
	case tables.PaintColrGlyph:
		if sub, ok := c.face.COLR.Search(p.GlyphID); ok {
			c.paint(sub, m, clip, dst)
		}
	case tables.PaintSolid:
		col := c.color(p.PaletteIndex, f214(p.Alpha))
		fillMask(dst, clip, func(int, int) [4]float32 { return col })
	case tables.PaintVarSolid:
		col := c.color(p.PaletteIndex, f214(p.Alpha))
		fillMask(dst, clip, func(int, int) [4]float32 { return col })
	case tables.PaintLinearGradient:
		c.linear(p.ColorLine, p.X0, p.Y0, p.X1, p.Y1, p.X2, p.Y2, m, clip, dst)
	case tables.PaintVarLinearGradient:
		c.linear(varLine(p.ColorLine), p.X0, p.Y0, p.X1, p.Y1, p.X2, p.Y2, m, clip, dst)
	case tables.PaintRadialGradient:
		c.radial(p.ColorLine, p.X0, p.Y0, p.Radius0, p.X1, p.Y1, p.Radius1, m, clip, dst)
	case tables.PaintVarRadialGradient:
		c.radial(varLine(p.ColorLine), p.X0, p.Y0, p.Radius0, p.X1, p.Y1, p.Radius1, m, clip, dst)
	case tables.PaintSweepGradient:
		c.sweep(p.ColorLine, p.CenterX, p.CenterY, f214(p.StartAngle), f214(p.EndAngle), m, clip, dst)
	case tables.PaintVarSweepGradient:
		c.sweep(varLine(p.ColorLine), p.CenterX, p.CenterY, f214(p.StartAngle), f214(p.EndAngle), m, clip, dst)
	case tables.PaintComposite:
		src := image.NewRGBA(c.rect)
		back := image.NewRGBA(c.rect)
		c.paint(p.SourcePaint, m, clip, src)
		c.paint(p.BackdropPaint, m, clip, back)
		composite(back, src, p.CompositeMode)
		draw.Draw(dst, dst.Rect, back, dst.Rect.Min, draw.Over)
	default:
		if child, t, ok := transformOf(p); ok {
			c.paint(child, m.mul(t), clip, dst)
		}
	}
}

func varLine(v tables.VarColorLine) tables.ColorLine {
	out := tables.ColorLine{Extend: v.Extend}
	for _, s := range v.ColorStops {
		out.ColorStops = append(out.ColorStops, tables.ColorStop{StopOffset: s.StopOffset, PaletteIndex: s.PaletteIndex, Alpha: s.Alpha})
	}
	return out
}

// colorAt evaluates a color line at t, honoring its extend mode.
func (c *colr) colorAt(cl tables.ColorLine, stops []tables.ColorStop, t float32) [4]float32 {
	if len(stops) == 0 {
		return [4]float32{}
	}
	lo, hi := f214(stops[0].StopOffset), f214(stops[len(stops)-1].StopOffset)
	if span := hi - lo; span > 0 {
		switch cl.Extend {
		case tables.ExtendRepeat:
			t = lo + float32(math.Mod(float64(t-lo), float64(span)))
			if t < lo {
				t += span
			}
		case tables.ExtendReflect:
			u := float32(math.Mod(float64(t-lo), float64(2*span)))
			if u < 0 {
				u += 2 * span
			}
			if u > span {
				u = 2*span - u
			}
			t = lo + u
		}
	}
	if t <= lo {
		return c.color(stops[0].PaletteIndex, f214(stops[0].Alpha))
	}
	if t >= hi {
		s := stops[len(stops)-1]
		return c.color(s.PaletteIndex, f214(s.Alpha))
	}
	for i := 1; i < len(stops); i++ {
		o1 := f214(stops[i].StopOffset)
		if t <= o1 {
			o0 := f214(stops[i-1].StopOffset)
			a := c.color(stops[i-1].PaletteIndex, f214(stops[i-1].Alpha))
			b := c.color(stops[i].PaletteIndex, f214(stops[i].Alpha))
			k := float32(0)
			if o1 > o0 {
				k = (t - o0) / (o1 - o0)
			}
			return [4]float32{a[0] + (b[0]-a[0])*k, a[1] + (b[1]-a[1])*k, a[2] + (b[2]-a[2])*k, a[3] + (b[3]-a[3])*k}
		}
	}
	s := stops[len(stops)-1]
	return c.color(s.PaletteIndex, f214(s.Alpha))
}

func sortedStops(cl tables.ColorLine) []tables.ColorStop {
	s := append([]tables.ColorStop(nil), cl.ColorStops...)
	sort.SliceStable(s, func(i, j int) bool { return s[i].StopOffset < s[j].StopOffset })
	return s
}

// gradient runs fn for each covered pixel with the pixel center mapped
// back into font units.
func (c *colr) gradient(m affine, clip *image.Alpha, dst *image.RGBA, fn func(x, y float32) [4]float32) {
	inv, ok := m.invert()
	if !ok {
		return
	}
	fillMask(dst, clip, func(px, py int) [4]float32 {
		x, y := inv.apply(float32(px)+0.5, float32(py)+0.5)
		return fn(x, y)
	})
}

func (c *colr) linear(cl tables.ColorLine, x0, y0, x1, y1, x2, y2 int16, m affine, clip *image.Alpha, dst *image.RGBA) {
	stops := sortedStops(cl)
	p0x, p0y := float32(x0), float32(y0)
	// The color line runs from p0 along p0→p1, rotated so that it is
	// perpendicular to p0→p2 (the spec's "p3" construction).
	dx, dy := float32(x1-x0), float32(y1-y0)
	px, py := float32(y2-y0), -float32(x2-x0) // perpendicular to p0→p2
	if pl := px*px + py*py; pl > 0 {
		k := (dx*px + dy*py) / pl
		dx, dy = px*k, py*k
	}
	l2 := dx*dx + dy*dy
	c.gradient(m, clip, dst, func(x, y float32) [4]float32 {
		t := float32(0)
		if l2 > 0 {
			t = ((x-p0x)*dx + (y-p0y)*dy) / l2
		}
		return c.colorAt(cl, stops, t)
	})
}

func (c *colr) radial(cl tables.ColorLine, x0, y0 int16, r0 uint16, x1, y1 int16, r1 uint16, m affine, clip *image.Alpha, dst *image.RGBA) {
	stops := sortedStops(cl)
	c0x, c0y, rr0 := float64(x0), float64(y0), float64(r0)
	cdx, cdy, dr := float64(x1-x0), float64(y1-y0), float64(r1)-float64(r0)
	a := cdx*cdx + cdy*cdy - dr*dr
	c.gradient(m, clip, dst, func(x, y float32) [4]float32 {
		// Solve for the largest t where the point lies on the circle
		// interpolated between the two (a two-point conical gradient).
		pdx, pdy := float64(x)-c0x, float64(y)-c0y
		b := pdx*cdx + pdy*cdy + rr0*dr
		cc := pdx*pdx + pdy*pdy - rr0*rr0
		var t float64
		if math.Abs(a) < 1e-9 {
			if b == 0 {
				return [4]float32{}
			}
			t = cc / (2 * b)
		} else {
			disc := b*b - a*cc
			if disc < 0 {
				return [4]float32{}
			}
			sq := math.Sqrt(disc)
			t = (b + sq) / a
			if rr0+t*dr < 0 {
				t = (b - sq) / a
			}
		}
		return c.colorAt(cl, stops, float32(t))
	})
}

func (c *colr) sweep(cl tables.ColorLine, cx, cy int16, start, end float32, m affine, clip *image.Alpha, dst *image.RGBA) {
	stops := sortedStops(cl)
	// Angles are counter-clockwise, (value + 1) * 180°.
	a0, a1 := float64(start+1)*180, float64(end+1)*180
	c.gradient(m, clip, dst, func(x, y float32) [4]float32 {
		ang := math.Atan2(float64(y)-float64(cy), float64(x)-float64(cx)) * 180 / math.Pi
		if ang < 0 {
			ang += 360
		}
		t := float32(0)
		if a1 != a0 {
			t = float32((ang - a0) / (a1 - a0))
		}
		return c.colorAt(cl, stops, t)
	})
}

// composite applies src onto dst (the backdrop) with a COLR composite mode.
// Blend modes other than the Porter-Duff ones fall back to source-over,
// which is what emoji fonts use in practice.
func composite(dst, src *image.RGBA, mode tables.CompositeMode) {
	for i := 0; i < len(dst.Pix); i += 4 {
		sa := float32(src.Pix[i+3]) / 255
		da := float32(dst.Pix[i+3]) / 255
		var fs, fd float32
		switch mode {
		case tables.CompositeClear:
			fs, fd = 0, 0
		case tables.CompositeSrc:
			fs, fd = 1, 0
		case tables.CompositeDest:
			fs, fd = 0, 1
		case tables.CompositeDestOver:
			fs, fd = 1-da, 1
		case tables.CompositeSrcIn:
			fs, fd = da, 0
		case tables.CompositeDestIn:
			fs, fd = 0, sa
		case tables.CompositeSrcOut:
			fs, fd = 1-da, 0
		case tables.CompositeDestOut:
			fs, fd = 0, 1-sa
		case tables.CompositeSrcAtop:
			fs, fd = da, 1-sa
		case tables.CompositeDestAtop:
			fs, fd = 1-da, sa
		case tables.CompositeXor:
			fs, fd = 1-da, 1-sa
		case tables.CompositePlus:
			fs, fd = 1, 1
		default:
			fs, fd = 1, 1-sa
		}
		for k := 0; k < 4; k++ {
			dst.Pix[i+k] = clamp8(float32(src.Pix[i+k])*fs + float32(dst.Pix[i+k])*fd)
		}
	}
}

// fillOutline draws an outline in a solid color.
func fillOutline(segs []opentype.Segment, m affine, col [4]uint8) *image.RGBA {
	x0, y0, x1, y1, ok := outlineBounds(segs, m)
	if !ok {
		return nil
	}
	rect := image.Rect(int(math.Floor(float64(x0))), int(math.Floor(float64(y0))), int(math.Ceil(float64(x1))), int(math.Ceil(float64(y1))))
	if rect.Empty() {
		return nil
	}
	r := vector.NewRasterizer(rect.Dx(), rect.Dy())
	drawOutline(r, segs, m, float32(rect.Min.X), float32(rect.Min.Y))
	mask := image.NewAlpha(rect)
	r.Draw(mask, mask.Rect, image.Opaque, image.Point{})
	out := image.NewRGBA(rect)
	for i, a := range mask.Pix {
		k := uint32(a)
		out.Pix[i*4] = uint8(uint32(col[0]) * k / 255)
		out.Pix[i*4+1] = uint8(uint32(col[1]) * k / 255)
		out.Pix[i*4+2] = uint8(uint32(col[2]) * k / 255)
		out.Pix[i*4+3] = uint8(uint32(col[3]) * k / 255)
	}
	return out
}

// decodeBitmap turns an sbix/CBDT PNG strike into an image positioned in
// pixel space, scaled from the strike's ppem to ours.
func decodeBitmap(bm font.GlyphBitmap, face *font.Face, g placed, scale float32) *image.RGBA {
	if bm.Format != font.PNG {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(bm.Data))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Rect, img, b.Min, draw.Src)
	// Place the bitmap on the glyph's advance box: bitmap strikes are drawn
	// to cover the em square from the baseline up, roughly.
	adv := face.HorizontalAdvance(g.gid) * scale
	if adv <= 0 {
		adv = float32(b.Dx())
	}
	k := adv / float32(b.Dx())
	w := max(1, int(math.Round(float64(float32(b.Dx())*k))))
	h := max(1, int(math.Round(float64(float32(b.Dy())*k))))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	scaleRGBA(dst, src)
	x := int(math.Round(float64(g.x)))
	y := int(math.Round(float64(g.y))) - h + int(math.Round(float64(float32(h)*0.12)))
	dst.Rect = dst.Rect.Add(image.Pt(x, y))
	return dst
}
