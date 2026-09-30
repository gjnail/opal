package fonts

import (
	_ "embed"
	"image"
	"image/draw"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

//go:embed embedded/SymbolsNerdFontMono-Regular.ttf
var nerdFont []byte

// affine maps font units to pixels: x' = a*x + c*y + e, y' = b*x + d*y + f.
type affine struct{ a, b, c, d, e, f float32 }

func (m affine) apply(x, y float32) (float32, float32) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// mul returns m∘n (apply n first).
func (m affine) mul(n affine) affine {
	return affine{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m affine) invert() (affine, bool) {
	det := m.a*m.d - m.b*m.c
	if det == 0 {
		return affine{}, false
	}
	id := 1 / det
	return affine{
		a: m.d * id, b: -m.b * id, c: -m.c * id, d: m.a * id,
		e: (m.c*m.f - m.d*m.e) * id,
		f: (m.b*m.e - m.a*m.f) * id,
	}, true
}

// outlineBounds returns the pixel bounds of segments under m.
func outlineBounds(segs []opentype.Segment, m affine) (minX, minY, maxX, maxY float32, ok bool) {
	minX, minY = float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY = float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, s := range segs {
		n := 1
		switch s.Op {
		case opentype.SegmentOpQuadTo:
			n = 2
		case opentype.SegmentOpCubeTo:
			n = 3
		}
		for i := 0; i < n; i++ {
			x, y := m.apply(s.Args[i].X, s.Args[i].Y)
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	return minX, minY, maxX, maxY, minX <= maxX
}

// drawOutline adds segments, transformed by m, to a rasterizer whose
// origin is at pixel (ox, oy).
func drawOutline(r *vector.Rasterizer, segs []opentype.Segment, m affine, ox, oy float32) {
	open := false
	for _, s := range segs {
		p := func(i int) (float32, float32) {
			x, y := m.apply(s.Args[i].X, s.Args[i].Y)
			return x - ox, y - oy
		}
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				r.ClosePath()
			}
			r.MoveTo(p(0))
			open = true
		case opentype.SegmentOpLineTo:
			r.LineTo(p(0))
		case opentype.SegmentOpQuadTo:
			x1, y1 := p(0)
			x2, y2 := p(1)
			r.QuadTo(x1, y1, x2, y2)
		case opentype.SegmentOpCubeTo:
			x1, y1 := p(0)
			x2, y2 := p(1)
			x3, y3 := p(2)
			r.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	if open {
		r.ClosePath()
	}
}

// rasterGlyphs renders monochrome outlines into one coverage mask.
// Glyph origins sit at (ox + g.x, baseline + g.y) within the cell.
func rasterGlyphs(face *font.Face, gs []placed, scale, ox, baseline, skew, embolden float32) *Glyph {
	type item struct {
		segs []opentype.Segment
		m    affine
	}
	var items []item
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, g := range gs {
		out, ok := face.GlyphDataOutline(g.gid)
		if !ok || len(out.Segments) == 0 {
			continue
		}
		// Font units are y-up; pixels are y-down. Skew fakes italics.
		m := affine{a: scale, b: 0, c: skew * scale, d: -scale, e: ox + g.x, f: baseline + g.y}
		x0, y0, x1, y1, ok := outlineBounds(out.Segments, m)
		if !ok {
			continue
		}
		minX, minY = min(minX, x0), min(minY, y0)
		maxX, maxY = max(maxX, x1+embolden), max(maxY, y1)
		items = append(items, item{out.Segments, m})
	}
	if len(items) == 0 {
		return nil
	}
	x0, y0 := int(math.Floor(float64(minX))), int(math.Floor(float64(minY)))
	x1, y1 := int(math.Ceil(float64(maxX))), int(math.Ceil(float64(maxY)))
	w, h := max(x1-x0, 1), max(y1-y0, 1)
	r := vector.NewRasterizer(w, h)
	for _, it := range items {
		drawOutline(r, it.segs, it.m, float32(x0), float32(y0))
		if embolden > 0 {
			// Faux bold: the union of the glyph and a shifted copy.
			m := it.m
			m.e += embolden
			drawOutline(r, it.segs, m, float32(x0), float32(y0))
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return &Glyph{Mask: mask, X: x0, Y: y0}
}

// renderColor draws color glyphs (COLR layers or bitmap strikes) at their
// natural size. The returned image's origin is the cluster origin on the
// baseline; its bounds may be negative.
func (s *Set) renderColor(face *font.Face, gs []placed, scale float32) (*image.RGBA, bool) {
	var layers []*image.RGBA
	for _, g := range gs {
		if face.COLR != nil {
			if paint, ok := face.COLR.Search(tablesGID(g.gid)); ok {
				m := affine{a: scale, d: -scale, e: g.x, f: g.y}
				if img := renderCOLR(face, paint, m); img != nil {
					layers = append(layers, img)
					continue
				}
			}
		}
		face.SetPpem(uint16(s.cfg.SizePx), uint16(s.cfg.SizePx))
		if bm, ok := face.GlyphDataBitmap(g.gid); ok {
			if img := decodeBitmap(bm, face, g, scale); img != nil {
				layers = append(layers, img)
				continue
			}
		}
		// A glyph with no color data inside a color font: draw its outline
		// in a neutral color so it isn't lost.
		if out, ok := face.GlyphDataOutline(g.gid); ok && len(out.Segments) > 0 {
			m := affine{a: scale, d: -scale, e: g.x, f: g.y}
			if img := fillOutline(out.Segments, m, [4]uint8{0xd0, 0xd0, 0xd0, 0xff}); img != nil {
				layers = append(layers, img)
			}
		}
	}
	if len(layers) == 0 {
		return nil, false
	}
	b := layers[0].Rect
	for _, l := range layers[1:] {
		b = b.Union(l.Rect)
	}
	out := image.NewRGBA(b)
	for _, l := range layers {
		draw.Draw(out, l.Rect, l, l.Rect.Min, draw.Over)
	}
	return out, true
}

// fitColor scales a color glyph to fit the cell box and centers it.
func fitColor(img *image.RGBA, boxW, boxH float32) *Glyph {
	b := img.Rect
	w, h := float32(b.Dx()), float32(b.Dy())
	if w <= 0 || h <= 0 {
		return nil
	}
	pad := float32(1)
	sc := min((boxW-pad)/w, (boxH-pad)/h, 1)
	tw, th := max(1, int(math.Round(float64(w*sc)))), max(1, int(math.Round(float64(h*sc))))
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if tw == b.Dx() && th == b.Dy() {
		draw.Draw(dst, dst.Rect, img, b.Min, draw.Src)
	} else {
		scaleRGBA(dst, img)
	}
	return &Glyph{
		Color: dst,
		X:     int(math.Round(float64((boxW - float32(tw)) / 2))),
		Y:     int(math.Round(float64((boxH - float32(th)) / 2))),
	}
}

// scaleRGBA resamples src into dst with a box filter (good for the
// downscaling emoji need).
func scaleRGBA(dst, src *image.RGBA) {
	sb := src.Rect
	sw, sh := sb.Dx(), sb.Dy()
	dw, dh := dst.Rect.Dx(), dst.Rect.Dy()
	fx, fy := float64(sw)/float64(dw), float64(sh)/float64(dh)
	for y := 0; y < dh; y++ {
		sy0, sy1 := float64(y)*fy, float64(y+1)*fy
		for x := 0; x < dw; x++ {
			sx0, sx1 := float64(x)*fx, float64(x+1)*fx
			var r, g, b, a, wsum float64
			for yy := int(sy0); yy < int(math.Ceil(sy1)) && yy < sh; yy++ {
				wy := math.Min(sy1, float64(yy+1)) - math.Max(sy0, float64(yy))
				for xx := int(sx0); xx < int(math.Ceil(sx1)) && xx < sw; xx++ {
					wx := math.Min(sx1, float64(xx+1)) - math.Max(sx0, float64(xx))
					wgt := wx * wy
					i := src.PixOffset(sb.Min.X+xx, sb.Min.Y+yy)
					r += float64(src.Pix[i]) * wgt
					g += float64(src.Pix[i+1]) * wgt
					b += float64(src.Pix[i+2]) * wgt
					a += float64(src.Pix[i+3]) * wgt
					wsum += wgt
				}
			}
			if wsum > 0 {
				i := dst.PixOffset(x, y)
				dst.Pix[i] = uint8(r/wsum + 0.5)
				dst.Pix[i+1] = uint8(g/wsum + 0.5)
				dst.Pix[i+2] = uint8(b/wsum + 0.5)
				dst.Pix[i+3] = uint8(a/wsum + 0.5)
			}
		}
	}
}
