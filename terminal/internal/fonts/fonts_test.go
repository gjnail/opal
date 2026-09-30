package fonts

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"
	"time"
)

func newSet(t *testing.T) *Set {
	t.Helper()
	start := time.Now()
	s, err := New(Config{SizePx: 18})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("font set %q loaded in %v, metrics %+v", s.Family(), time.Since(start), s.Metrics())
	return s
}

func TestMetricsAreSane(t *testing.T) {
	s := newSet(t)
	m := s.Metrics()
	if m.CellW < 5 || m.CellW > 20 || m.CellH < 12 || m.CellH > 40 {
		t.Fatalf("odd cell size %dx%d at 18px", m.CellW, m.CellH)
	}
	if m.Baseline <= 0 || m.Baseline >= m.CellH {
		t.Fatalf("baseline %d outside cell of height %d", m.Baseline, m.CellH)
	}
	if m.Underline <= m.Baseline || m.Underline >= m.CellH {
		t.Fatalf("underline %d should sit between baseline %d and cell bottom %d", m.Underline, m.Baseline, m.CellH)
	}
}

func TestGlyphsRender(t *testing.T) {
	s := newSet(t)
	for _, c := range []struct {
		text  string
		cells int
		color bool
	}{
		{"A", 1, false},
		{"g", 1, false},
		{"中", 2, false},
		{"", 1, false}, // powerline branch, from the bundled Nerd Font
		{"", 1, false}, // Font Awesome diamond
		{"😀", 2, true},
	} {
		g := s.Glyph(c.text, Regular, c.cells)
		if g.Empty() {
			t.Errorf("%q rendered nothing", c.text)
			continue
		}
		if c.color && g.Color == nil {
			t.Errorf("%q should be a color glyph", c.text)
		}
	}
	if s.Glyph(" ", Regular, 1) != nil {
		t.Error("space should be empty")
	}
}

// TestDumpSample writes a PNG for eyeballing when OPAL_DUMP is set.
func TestDumpSample(t *testing.T) {
	out := os.Getenv("OPAL_DUMP")
	if out == "" {
		t.Skip("set OPAL_DUMP=path.png to write a sample")
	}
	s := newSet(t)
	m := s.Metrics()
	lines := []struct {
		text  string
		style Style
	}{
		{"The quick brown fox jumps over 0123456789 {}[]()<>=!", Regular},
		{"Bold: The quick brown fox jumps over the lazy dog", Bold},
		{"Italic: The quick brown fox jumps over the lazy dog", Italic},
		{"CJK: 中文字符 日本語 한국어  Greek: αβγ  Cyrillic: жзи", Regular},
		{"Icons:         ", Regular},
		{"Emoji: 😀 🚀 ❤️ 👍🏽 🇯🇵 👨‍👩‍👧", Regular},
	}
	W, H := 64*m.CellW, len(lines)*m.CellH
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Rect, &image.Uniform{color.RGBA{22, 22, 26, 255}}, image.Point{}, draw.Src)
	for row, l := range lines {
		x := 0
		for _, cl := range clusters(l.text) {
			cells := 1
			if wide(cl) {
				cells = 2
			}
			g := s.Glyph(cl, l.style, cells)
			if !g.Empty() {
				pt := image.Pt(x*m.CellW+g.X, row*m.CellH+g.Y)
				if g.Mask != nil {
					r := g.Mask.Rect.Sub(g.Mask.Rect.Min).Add(pt)
					draw.DrawMask(img, r, &image.Uniform{color.RGBA{230, 230, 235, 255}}, image.Point{}, g.Mask, g.Mask.Rect.Min, draw.Over)
				} else {
					r := g.Color.Rect.Sub(g.Color.Rect.Min).Add(pt)
					draw.Draw(img, r, g.Color, g.Color.Rect.Min, draw.Over)
				}
			}
			x += cells
		}
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
