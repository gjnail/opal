// Package fonts finds, loads and rasterizes the terminal's fonts: the
// user's monospace family in four styles, the bundled Nerd Font symbols,
// a color emoji font, and system fallbacks for everything else.
package fonts

import (
	"bytes"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Style selects bold and/or italic.
type Style uint8

const (
	Regular Style = 0
	Bold    Style = 1
	Italic  Style = 2
)

// Config chooses fonts and sizes.
type Config struct {
	// Families are tried in order; platform defaults follow them.
	Families []string
	// SizePx is the em size in device pixels.
	SizePx float32
	// LineHeight scales the cell height (1 = the font's own line height).
	LineHeight float32
	// CellWidth scales the cell width.
	CellWidth float32
	// CacheDir holds the system font index.
	CacheDir string
}

// Metrics describe the cell grid, in device pixels. Y values are measured
// from the top of the cell.
type Metrics struct {
	CellW, CellH   int
	Baseline       int
	Underline      int
	UnderlineThick int
	Strike         int
	StrikeThick    int
	// Ascent and Descent of the primary font, for placing fallback glyphs.
	Ascent, Descent float32
}

// Glyph is a rasterized cell's worth of text. Exactly one of Mask (tinted
// with the text color) or Color (drawn as is) is set, unless the glyph is
// empty.
type Glyph struct {
	Mask  *image.Alpha
	Color *image.RGBA
	// X, Y offset the image from the top-left of the cell.
	X, Y int
}

// Empty reports whether there is nothing to draw.
func (g *Glyph) Empty() bool { return g == nil || (g.Mask == nil && g.Color == nil) }

type faceInfo struct {
	face   *font.Face
	family string
	synth  Style // styles we have to fake
}

// Set is a loaded font configuration. It is safe for concurrent use.
type Set struct {
	mu       sync.Mutex
	cfg      Config
	fm       *fontscan.FontMap
	primary  [4]faceInfo
	nerd     *font.Face
	emoji    *font.Face
	families []string
	m        Metrics
	scale    float32 // px per font unit for the primary face
	glyphs   map[glyphKey]*Glyph
	faces    map[faceKey]*faceInfo
	shaper   shaping.HarfbuzzShaper
}

type glyphKey struct {
	text  string
	style Style
	cells uint8
}

type faceKey struct {
	r     rune
	style Style
	emoji bool
}

type quietLogger struct{}

func (quietLogger) Printf(string, ...interface{}) {}

// DefaultFamilies are the monospace families tried when the user's
// choices aren't installed.
func DefaultFamilies() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"Cascadia Mono", "Cascadia Code", "Consolas", "Lucida Console", "Courier New"}
	case "darwin":
		return []string{"SF Mono", "Menlo", "Monaco", "Courier New"}
	default:
		return []string{"JetBrains Mono", "DejaVu Sans Mono", "Noto Sans Mono", "Liberation Mono", "Ubuntu Mono", "monospace"}
	}
}

func emojiFamilies() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"Segoe UI Emoji"}
	case "darwin":
		return []string{"Apple Color Emoji"}
	default:
		return []string{"Noto Color Emoji", "Twemoji", "JoyPixels", "Emoji One"}
	}
}

var (
	sharedMapOnce sync.Once
	sharedMap     *fontscan.FontMap
	sharedMapErr  error
	sharedMapMu   sync.Mutex
)

// systemFonts returns the process-wide system font index. Scanning the
// system's fonts is slow the first time, so every Set shares one (fontscan
// keeps an on-disk index too).
func systemFonts(cacheDir string) (*fontscan.FontMap, error) {
	sharedMapOnce.Do(func() {
		if cacheDir == "" {
			if d, err := os.UserCacheDir(); err == nil {
				cacheDir = filepath.Join(d, "opal-terminal")
			}
		}
		sharedMap = fontscan.NewFontMap(quietLogger{})
		sharedMapErr = sharedMap.UseSystemFonts(cacheDir)
	})
	return sharedMap, sharedMapErr
}

// New loads fonts for cfg.
func New(cfg Config) (*Set, error) {
	if cfg.SizePx <= 0 {
		cfg.SizePx = 16
	}
	if cfg.LineHeight <= 0 {
		cfg.LineHeight = 1
	}
	if cfg.CellWidth <= 0 {
		cfg.CellWidth = 1
	}
	fm, err := systemFonts(cfg.CacheDir)
	s := &Set{cfg: cfg, fm: fm, glyphs: map[glyphKey]*Glyph{}, faces: map[faceKey]*faceInfo{}}
	s.families = append(append([]string{}, cfg.Families...), DefaultFamilies()...)

	sharedMapMu.Lock()
	defer sharedMapMu.Unlock()
	if err == nil && fm != nil {
		for st := Style(0); st < 4; st++ {
			s.primary[st] = s.findPrimary(st)
		}
		for _, fam := range emojiFamilies() {
			if f := s.exactFace(fam, font.Aspect{}); f != nil {
				s.emoji = f
				break
			}
		}
	}
	if s.primary[Regular].face == nil {
		// No usable system font at all (a bare container): use the
		// bundled symbols font so we at least draw something.
		f, perr := loadEmbedded()
		if perr != nil {
			return nil, perr
		}
		for st := Style(0); st < 4; st++ {
			s.primary[st] = faceInfo{face: f, family: "Symbols Nerd Font Mono", synth: st}
		}
	}
	if nf, err := loadEmbedded(); err == nil {
		s.nerd = nf
	}
	s.computeMetrics()
	return s, nil
}

// exactFace returns the system face of exactly this family, or nil.
func (s *Set) exactFace(family string, aspect font.Aspect) *font.Face {
	if strings.EqualFold(family, "monospace") {
		s.fm.SetQuery(fontscan.Query{Families: []string{fontscan.Monospace}, Aspect: aspect})
		return s.fm.ResolveFace('M')
	}
	s.fm.SetQuery(fontscan.Query{Families: []string{family}, Aspect: aspect})
	f := s.fm.ResolveFace('M')
	if f == nil {
		// Emoji fonts may not contain 'M'.
		f = s.fm.ResolveFace('😀')
	}
	if f == nil {
		return nil
	}
	got, _ := s.fm.FontMetadata(f.Font)
	if normFamily(got) != normFamily(family) {
		return nil
	}
	return f
}

// normFamily compares family names the way fontscan stores them:
// lowercase, without spaces, hyphens or underscores.
func normFamily(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_':
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

func (s *Set) findPrimary(st Style) faceInfo {
	aspect := font.Aspect{Weight: font.WeightNormal, Style: font.StyleNormal}
	if st&Bold != 0 {
		aspect.Weight = font.WeightBold
	}
	if st&Italic != 0 {
		aspect.Style = font.StyleItalic
	}
	for _, fam := range s.families {
		f := s.exactFace(fam, aspect)
		if f == nil {
			continue
		}
		_, got := s.fm.FontMetadata(f.Font)
		var synth Style
		if st&Bold != 0 && got.Weight < font.WeightSemibold {
			synth |= Bold
		}
		if st&Italic != 0 && got.Style != font.StyleItalic {
			synth |= Italic
		}
		return faceInfo{face: f, family: fam, synth: synth}
	}
	return faceInfo{}
}

func (s *Set) computeMetrics() {
	f := s.primary[Regular].face
	upem := float32(f.Upem())
	s.scale = s.cfg.SizePx / upem
	ext, ok := f.FontHExtents()
	if !ok || ext.Ascender <= 0 {
		ext = font.FontExtents{Ascender: upem * 0.8, Descender: -upem * 0.2}
	}
	asc := ext.Ascender * s.scale
	desc := -ext.Descender * s.scale
	gap := max(ext.LineGap, 0) * s.scale
	adv := s.cfg.SizePx * 0.6
	if gid, ok := f.NominalGlyph('M'); ok {
		adv = f.HorizontalAdvance(gid) * s.scale
	}
	m := Metrics{Ascent: asc, Descent: desc}
	m.CellW = max(1, int(math.Round(float64(adv*s.cfg.CellWidth))))
	m.CellH = max(1, int(math.Ceil(float64((asc+desc+gap)*s.cfg.LineHeight))))
	m.Baseline = int(math.Round(float64(asc + (float32(m.CellH)-(asc+desc))/2)))
	m.UnderlineThick = max(1, int(math.Round(float64(f.LineMetric(font.UnderlineThickness)*s.scale))))
	upos := f.LineMetric(font.UnderlinePosition) * s.scale
	if upos == 0 {
		upos = -desc / 2
	}
	m.Underline = min(m.Baseline+max(1, int(math.Round(float64(-upos)))), m.CellH-m.UnderlineThick)
	m.StrikeThick = max(1, int(math.Round(float64(f.LineMetric(font.StrikethroughThickness)*s.scale))))
	spos := f.LineMetric(font.StrikethroughPosition) * s.scale
	if spos <= 0 {
		spos = f.LineMetric(font.XHeight) * s.scale / 2
		if spos <= 0 {
			spos = asc / 3
		}
	}
	m.Strike = m.Baseline - int(math.Round(float64(spos)))
	s.m = m
}

// Metrics returns the cell metrics.
func (s *Set) Metrics() Metrics { return s.m }

// Family names the primary font actually in use.
func (s *Set) Family() string { return s.primary[Regular].family }

// SizePx is the em size.
func (s *Set) SizePx() float32 { return s.cfg.SizePx }

// isPUA reports private-use code points, where Nerd Font icons live.
func isPUA(r rune) bool {
	return (r >= 0xe000 && r <= 0xf8ff) || r >= 0xf0000
}

// wantsEmoji reports whether a cluster should come from the color emoji
// font: emoji-presentation characters, VS16 sequences, ZWJ sequences,
// flags and keycaps.
func wantsEmoji(text string, cells int) bool {
	r, size := utf8.DecodeRuneInString(text)
	rest := text[size:]
	if strings.ContainsRune(rest, 0xfe0f) || strings.ContainsRune(rest, 0x20e3) {
		return true
	}
	if strings.ContainsRune(rest, 0xfe0e) {
		return false // explicit text presentation
	}
	if r >= 0x1f1e6 && r <= 0x1f1ff {
		return true // regional indicators
	}
	return cells == 2 && r >= 0x2300 && !isPUA(r) && isEmojiRange(r)
}

func isEmojiRange(r rune) bool {
	return (r >= 0x1f000 && r <= 0x1faff) || (r >= 0x2600 && r <= 0x27bf) ||
		(r >= 0x2300 && r <= 0x23ff) || (r >= 0x2b00 && r <= 0x2bff) || r == 0x3030 || r == 0x303d
}

// faceFor picks the face that should draw r. Callers hold s.mu.
func (s *Set) faceFor(r rune, st Style, emoji bool) *faceInfo {
	k := faceKey{r, st, emoji}
	if fi, ok := s.faces[k]; ok {
		return fi
	}
	fi := s.resolveFace(r, st, emoji)
	s.faces[k] = fi
	return fi
}

func (s *Set) resolveFace(r rune, st Style, emoji bool) *faceInfo {
	p := &s.primary[st]
	if emoji && s.emoji != nil {
		if _, ok := s.emoji.NominalGlyph(r); ok {
			return &faceInfo{face: s.emoji, family: "emoji"}
		}
	}
	if _, ok := p.face.NominalGlyph(r); ok {
		return p
	}
	if s.nerd != nil && (isPUA(r) || (r >= 0x2500 && r <= 0x2bff)) {
		if _, ok := s.nerd.NominalGlyph(r); ok {
			return &faceInfo{face: s.nerd, family: "Symbols Nerd Font Mono", synth: st}
		}
	}
	if s.emoji != nil && isEmojiRange(r) {
		if _, ok := s.emoji.NominalGlyph(r); ok {
			return &faceInfo{face: s.emoji, family: "emoji"}
		}
	}
	if s.fm != nil {
		sharedMapMu.Lock()
		fams := append(append([]string{}, s.families...), fontscan.Monospace, fontscan.SansSerif)
		s.fm.SetQuery(fontscan.Query{Families: fams})
		s.fm.SetScript(language.LookupScript(r))
		f := s.fm.ResolveFace(r)
		sharedMapMu.Unlock()
		if f != nil {
			if _, ok := f.NominalGlyph(r); ok {
				return &faceInfo{face: f, family: "fallback", synth: st}
			}
		}
	}
	if s.nerd != nil {
		if _, ok := s.nerd.NominalGlyph(r); ok {
			return &faceInfo{face: s.nerd, family: "Symbols Nerd Font Mono", synth: st}
		}
	}
	return p
}

// Glyph rasterizes text (one grapheme cluster) for a cell group `cells`
// wide. The result is cached.
func (s *Set) Glyph(text string, st Style, cells int) *Glyph {
	if text == "" || text == " " {
		return nil
	}
	k := glyphKey{text, st & 3, uint8(cells)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.glyphs[k]; ok {
		return g
	}
	g := s.render(text, st&3, cells)
	if len(s.glyphs) > 20000 {
		clear(s.glyphs)
	}
	s.glyphs[k] = g
	return g
}

type placed struct {
	gid  font.GID
	x, y float32 // pixel offset of the glyph origin from the cluster origin
}

func (s *Set) render(text string, st Style, cells int) *Glyph {
	r, _ := utf8.DecodeRuneInString(text)
	emoji := wantsEmoji(text, cells)
	fi := s.faceFor(r, st, emoji)
	face := fi.face
	scale := s.cfg.SizePx / float32(face.Upem())

	var gs []placed
	var advance float32
	if utf8.RuneCountInString(text) == 1 {
		gid, ok := face.NominalGlyph(r)
		if !ok {
			return nil
		}
		gs = []placed{{gid: gid}}
		advance = face.HorizontalAdvance(gid) * scale
	} else {
		runes := []rune(text)
		out := s.shaper.Shape(shaping.Input{
			Text: runes, RunStart: 0, RunEnd: len(runes),
			Direction: di.DirectionLTR, Face: face,
			Size:   fixed.Int26_6(s.cfg.SizePx * 64),
			Script: language.LookupScript(r), Language: language.DefaultLanguage(),
		})
		var x float32
		for _, g := range out.Glyphs {
			gs = append(gs, placed{
				gid: font.GID(g.GlyphID),
				x:   x + float32(g.XOffset)/64,
				y:   -float32(g.YOffset) / 64,
			})
			x += float32(g.Advance) / 64
		}
		advance = x
	}

	boxW := float32(cells * s.m.CellW)
	boxH := float32(s.m.CellH)
	isPrimary := face == s.primary[st].face

	// Color glyphs (emoji) are scaled to fit the cell group and centered.
	if face.COLR != nil || hasBitmaps(face) {
		if img, ok := s.renderColor(face, gs, scale); ok {
			return fitColor(img, boxW, boxH)
		}
	}

	// Monochrome: primary-font glyphs sit on the grid as designed;
	// fallback glyphs are shrunk if they'd overflow their cells and
	// centered in them.
	gscale := scale
	ox := float32(0)
	if !isPrimary {
		if advance > boxW*1.05 && advance > 0 {
			gscale *= boxW / advance
			advance = boxW
		}
		ox = (boxW - advance) / 2
		if face == s.nerd {
			ox = 0 // the Mono variant is already cell-sized
		}
	}
	var skew, embolden float32
	if fi.synth&Italic != 0 {
		skew = 0.2
	}
	if fi.synth&Bold != 0 {
		embolden = max(1, s.cfg.SizePx/16)
	}
	return rasterGlyphs(face, gs, gscale, ox, float32(s.m.Baseline), skew, embolden)
}

func hasBitmaps(f *font.Face) bool {
	if gid, ok := f.NominalGlyph('😀'); ok {
		_, ok := f.GlyphDataBitmap(gid)
		return ok
	}
	return false
}

// loadEmbedded parses the bundled Symbols Nerd Font Mono.
func loadEmbedded() (*font.Face, error) {
	return font.ParseTTF(bytes.NewReader(nerdFont))
}
