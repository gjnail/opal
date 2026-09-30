// Command mkicon draws Opal Terminal's icon and writes it in the formats
// each platform wants: PNG sizes for Linux, a Windows .ico and a macOS
// .icns. Run it from terminal/: go run ./tools/mkicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

// The fire theme's gradient, the default Opal theme.
var stops = []color.NRGBA{
	{0x6e, 0xf3, 0xd6, 255}, {0x7c, 0xc7, 0xff, 255}, {0xb6, 0x9c, 0xff, 255},
	{0xff, 0x8f, 0xcf, 255}, {0xff, 0xc3, 0x6e, 255},
}

func gradientAt(t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t)) * float64(len(stops)-1)
	i := int(t)
	if i >= len(stops)-1 {
		return stops[len(stops)-1]
	}
	f := t - float64(i)
	a, b := stops[i], stops[i+1]
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
}

// roundedRect adds a rounded rectangle path.
func roundedRect(r *vector.Rasterizer, x0, y0, x1, y1, rad float32) {
	const k = 0.5523 // cubic approximation of a quarter circle
	r.MoveTo(x0+rad, y0)
	r.LineTo(x1-rad, y0)
	r.CubeTo(x1-rad+rad*k, y0, x1, y0+rad-rad*k, x1, y0+rad)
	r.LineTo(x1, y1-rad)
	r.CubeTo(x1, y1-rad+rad*k, x1-rad+rad*k, y1, x1-rad, y1)
	r.LineTo(x0+rad, y1)
	r.CubeTo(x0+rad-rad*k, y1, x0, y1-rad+rad*k, x0, y1-rad)
	r.LineTo(x0, y0+rad)
	r.CubeTo(x0, y0+rad-rad*k, x0+rad-rad*k, y0, x0+rad, y0)
	r.ClosePath()
}

// fill paints coverage with a color function over dst.
func fill(dst *image.NRGBA, r *vector.Rasterizer, col func(x, y int) color.NRGBA) {
	b := dst.Bounds()
	mask := image.NewAlpha(b)
	r.Draw(mask, b, image.Opaque, image.Point{})
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := float64(mask.AlphaAt(x, y).A) / 255
			if a == 0 {
				continue
			}
			c := col(x, y)
			d := dst.NRGBAAt(x, y)
			ca := float64(c.A) / 255 * a
			da := float64(d.A) / 255
			oa := ca + da*(1-ca)
			blend := func(s, t uint8) uint8 {
				if oa == 0 {
					return 0
				}
				return uint8((float64(s)*ca + float64(t)*da*(1-ca)) / oa)
			}
			dst.SetNRGBA(x, y, color.NRGBA{blend(c.R, d.R), blend(c.G, d.G), blend(c.B, d.B), uint8(oa*255 + 0.5)})
		}
	}
}

func draw(size int) *image.NRGBA {
	s := float32(size)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	diag := func(x, y int) float64 { return (float64(x) + float64(y)) / (2 * float64(size)) }

	// Body: a dark rounded square with a thin gradient rim, like the
	// terminal window in the fire theme.
	m := s * 0.06
	r := vector.NewRasterizer(size, size)
	roundedRect(r, m, m, s-m, s-m, s*0.2)
	fill(img, r, func(x, y int) color.NRGBA { return gradientAt(diag(x, y)) })
	rim := s * 0.022
	r = vector.NewRasterizer(size, size)
	roundedRect(r, m+rim, m+rim, s-m-rim, s-m-rim, s*0.2-rim)
	fill(img, r, func(x, y int) color.NRGBA {
		t := float64(y) / float64(size)
		v := uint8(26 - 8*t)
		return color.NRGBA{v, v - 2, v + 6, 255}
	})

	// The prompt: a chevron and a block cursor, in the gradient.
	stroke := s * 0.075
	cx, cy := s*0.32, s*0.5
	w, h := s*0.16, s*0.18
	r = vector.NewRasterizer(size, size)
	r.MoveTo(cx-w/2, cy-h)
	r.LineTo(cx-w/2+stroke*0.9, cy-h)
	r.LineTo(cx+w/2+stroke*0.45, cy)
	r.LineTo(cx-w/2+stroke*0.9, cy+h)
	r.LineTo(cx-w/2, cy+h)
	r.LineTo(cx+w/2-stroke*0.45, cy)
	r.ClosePath()
	fill(img, r, func(x, y int) color.NRGBA { return gradientAt(diag(x, y)*1.6 - 0.2) })
	r = vector.NewRasterizer(size, size)
	roundedRect(r, s*0.5, cy+h-stroke, s*0.72, cy+h, stroke*0.25)
	fill(img, r, func(x, y int) color.NRGBA { return gradientAt(diag(x, y)*1.6 - 0.2) })
	return img
}

// downscale resamples with a box filter.
func downscale(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	f := float64(src.Bounds().Dx()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, n float64
			for sy := int(float64(y) * f); sy < int(float64(y+1)*f); sy++ {
				for sx := int(float64(x) * f); sx < int(float64(x+1)*f); sx++ {
					c := src.NRGBAAt(sx, sy)
					ca := float64(c.A)
					r += float64(c.R) * ca
					g += float64(c.G) * ca
					b += float64(c.B) * ca
					a += ca
					n++
				}
			}
			if a > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / n)})
			}
		}
	}
	return dst
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

// ico packs PNG images into a .ico (Windows Vista and later read PNG
// entries directly).
func ico(sizes []int, pngs [][]byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(pngs))})
	offset := 6 + 16*len(pngs)
	for i, p := range pngs {
		dim := uint8(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		binary.Write(&b, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(p)), uint32(offset)})
		offset += len(p)
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}

// icns packs PNG images into a macOS .icns.
func icns(types []string, pngs [][]byte) []byte {
	var body bytes.Buffer
	for i, p := range pngs {
		body.WriteString(types[i])
		binary.Write(&body, binary.BigEndian, uint32(8+len(p)))
		body.Write(p)
	}
	var b bytes.Buffer
	b.WriteString("icns")
	binary.Write(&b, binary.BigEndian, uint32(8+body.Len()))
	b.Write(body.Bytes())
	return b.Bytes()
}

func main() {
	out := "assets"
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	big := draw(1024)
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	scaled := map[int]*image.NRGBA{1024: big}
	for _, s := range []int{16, 24, 32, 48, 64, 128, 256, 512} {
		scaled[s] = downscale(big, s)
	}
	write("icon-256.png", pngBytes(scaled[256]))
	write("icon-1024.png", pngBytes(big))

	icoSizes := []int{16, 24, 32, 48, 64, 128, 256}
	var icoPNGs [][]byte
	for _, s := range icoSizes {
		icoPNGs = append(icoPNGs, pngBytes(scaled[s]))
	}
	write("icon.ico", ico(icoSizes, icoPNGs))

	// icp4 16, icp5 32, icp6 64, ic07 128, ic08 256, ic09 512, ic10 1024.
	types := []string{"icp4", "icp5", "icp6", "ic07", "ic08", "ic09", "ic10"}
	var icnsPNGs [][]byte
	for _, s := range []int{16, 32, 64, 128, 256, 512, 1024} {
		icnsPNGs = append(icnsPNGs, pngBytes(scaled[s]))
	}
	write("icon.icns", icns(types, icnsPNGs))
	log.Printf("wrote icons to %s", out)
}
