package render

import (
	"image"
	"math"

	"opal/terminal/internal/vt"
)

// fillRect sets rect (clipped to dst) to col at alpha a, replacing what
// was there.
func fillRect(dst *image.RGBA, rect image.Rectangle, col vt.RGB, a uint8) {
	rect = rect.Add(dst.Rect.Min).Intersect(dst.Rect)
	if rect.Empty() {
		return
	}
	k := uint32(a)
	px := [4]uint8{uint8(uint32(col.R) * k / 255), uint8(uint32(col.G) * k / 255), uint8(uint32(col.B) * k / 255), a}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		i := dst.PixOffset(rect.Min.X, y)
		row := dst.Pix[i : i+4*rect.Dx()]
		for j := 0; j < len(row); j += 4 {
			row[j], row[j+1], row[j+2], row[j+3] = px[0], px[1], px[2], px[3]
		}
	}
}

// strokeRect outlines rect with a line of width t.
func strokeRect(dst *image.RGBA, rect image.Rectangle, t int, col vt.RGB) {
	fillRect(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+t), col, 255)
	fillRect(dst, image.Rect(rect.Min.X, rect.Max.Y-t, rect.Max.X, rect.Max.Y), col, 255)
	fillRect(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+t, rect.Max.Y), col, 255)
	fillRect(dst, image.Rect(rect.Max.X-t, rect.Min.Y, rect.Max.X, rect.Max.Y), col, 255)
}

// blendPixel composites col at coverage a over the pixel at (x, y), where
// x and y are relative to dst's origin.
func blendPixel(dst *image.RGBA, x, y int, col vt.RGB, a uint8) {
	p := image.Pt(x, y).Add(dst.Rect.Min)
	if !p.In(dst.Rect) || a == 0 {
		return
	}
	i := dst.PixOffset(p.X, p.Y)
	blendAt(dst.Pix[i:i+4], col, uint32(a))
}

func blendAt(px []uint8, col vt.RGB, a uint32) {
	inv := 255 - a
	px[0] = uint8((uint32(col.R)*a + uint32(px[0])*inv + 127) / 255)
	px[1] = uint8((uint32(col.G)*a + uint32(px[1])*inv + 127) / 255)
	px[2] = uint8((uint32(col.B)*a + uint32(px[2])*inv + 127) / 255)
	px[3] = uint8((255*a + uint32(px[3])*inv + 127) / 255)
}

// blendMask tints a coverage mask with col and composites it at pt
// (relative to dst's origin), clipped to dst.
func blendMask(dst *image.RGBA, mask *image.Alpha, pt image.Point, col vt.RGB) {
	r := mask.Rect.Sub(mask.Rect.Min).Add(pt).Add(dst.Rect.Min)
	clip := r.Intersect(dst.Rect)
	if clip.Empty() {
		return
	}
	off := mask.Rect.Min.Sub(r.Min)
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		mi := mask.PixOffset(clip.Min.X+off.X, y+off.Y)
		di := dst.PixOffset(clip.Min.X, y)
		for x := clip.Min.X; x < clip.Max.X; x++ {
			if a := mask.Pix[mi]; a != 0 {
				blendAt(dst.Pix[di:di+4], col, uint32(a))
			}
			mi++
			di += 4
		}
	}
}

// blendColor composites a premultiplied RGBA image at pt.
func blendColor(dst *image.RGBA, src *image.RGBA, pt image.Point) {
	r := src.Rect.Sub(src.Rect.Min).Add(pt).Add(dst.Rect.Min)
	clip := r.Intersect(dst.Rect)
	if clip.Empty() {
		return
	}
	off := src.Rect.Min.Sub(r.Min)
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		si := src.PixOffset(clip.Min.X+off.X, y+off.Y)
		di := dst.PixOffset(clip.Min.X, y)
		for x := clip.Min.X; x < clip.Max.X; x++ {
			sa := uint32(src.Pix[si+3])
			if sa != 0 {
				inv := 255 - sa
				for k := 0; k < 4; k++ {
					dst.Pix[di+k] = uint8(uint32(src.Pix[si+k]) + (uint32(dst.Pix[di+k])*inv+127)/255)
				}
			}
			si += 4
			di += 4
		}
	}
}

func sin2pi(x float64) float64 { return math.Sin(2 * math.Pi * x) }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
