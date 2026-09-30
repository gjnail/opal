package render

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"opal/terminal/internal/fonts"
	"opal/terminal/internal/vt"
)

func demoTerminal(cols, rows int) *vt.Terminal {
	t := vt.New(vt.Options{Cols: cols, Rows: rows, GraphemeClustering: true})
	w := t.WriteString
	w("\x1b[48;2;255;138;101m\x1b[38;2;30;20;30m  opal \x1b[38;2;255;138;101m\x1b[48;2;64;52;72m\x1b[38;2;240;220;200m ~/src/opal \x1b[0m\x1b[38;2;64;52;72m\x1b[0m \x1b[38;2;242;216;167m main\x1b[0m \x1b[32m+2\x1b[0m \x1b[33m!1\x1b[0m\r\n")
	w("\x1b[1;34mdrwxr-xr-x\x1b[0m  \x1b[36mterminal\x1b[0m  \x1b[1;32mopal.exe\x1b[0m  README.md  \x1b[31mcore.dump\x1b[0m\r\n")
	w("┌──────────┬──────────╥──────┐  ╔══════╦══════╗  ╭──────╮\r\n")
	w("│ \x1b[1mname\x1b[0m     │ size     ║ ok   │  ║ dbl  ║ box  ║  │ arcs │\r\n")
	w("├──────────┼──────────╫──────┤  ╠══════╬══════╣  ╰──────╯\r\n")
	w("│ vt       │ ━━━━━━━━ ║ ┅┅┅┅ │  ╚══════╩══════╝  ╱╲╳ ┄┈╌\r\n")
	w("└──────────┴──────────╨──────┘  ▁▂▃▄▅▆▇█ ▏▎▍▌▋▊▉ ░▒▓ ▖▗▘▙▚▛▜▝▞▟\r\n")
	w("\x1b[4msingle\x1b[0m \x1b[4:2mdouble\x1b[0m \x1b[4:3;58;2;255;80;80mcurly\x1b[0m \x1b[4:4mdotted\x1b[0m \x1b[4:5mdashed\x1b[0m \x1b[9mstrike\x1b[0m \x1b[53moverline\x1b[0m \x1b[2mdim\x1b[0m \x1b[3mitalic\x1b[0m \x1b[7minverse\x1b[0m\r\n")
	var sb strings.Builder
	for i := 16; i < 232; i += 3 {
		fmt.Fprintf(&sb, "\x1b[48;5;%dm ", i)
	}
	w(sb.String() + "\x1b[0m\r\n")
	sb.Reset()
	for i := 0; i < 72; i++ {
		fmt.Fprintf(&sb, "\x1b[48;2;%d;%d;%dm ", 255-i*3, i*3, 128+i)
	}
	w(sb.String() + "\x1b[0m\r\n")
	w("emoji 😀 🚀 ❤️ 👍🏽 👨‍👩‍👧  cjk 中文字符 日本語  braille ⠁⠃⠇⡇⣇⣧⣷⣿  sextant 🬀🬁🬂🬃🬋🬎🬹\r\n")
	w("\x1b#6double width\r\n")
	w("\x1b#3double height\r\n\x1b#4double height\r\n")
	w("$ echo \x1b]8;;https://example.com\x1b\\hyperlink\x1b]8;;\x1b\\ selected text here")
	return t
}

func TestRenderDemo(t *testing.T) {
	f, err := fonts.New(fonts.Config{SizePx: 18})
	if err != nil {
		t.Fatal(err)
	}
	term := demoTerminal(78, 16)
	r := New(f, vt.DefaultPalette(), Options{})
	cols, rows := term.Size()
	size := r.Size(cols)
	img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y*rows))
	term.Lock()
	cx, cy := term.CursorPos()
	for y := 0; y < rows; y++ {
		row := &Row{Line: term.ScreenLine(y).Clone(), CursorX: -1}
		if y == cy {
			row.CursorX, row.CursorShape = cx, CursorBlock
			row.SelFrom, row.SelTo = 23, 36
		}
		sub := img.SubImage(image.Rect(0, y*size.Y, size.X, (y+1)*size.Y)).(*image.RGBA)
		r.Draw(row, sub)
	}
	term.Unlock()
	if out := os.Getenv("OPAL_DUMP"); out != "" {
		fh, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(fh, img)
		fh.Close()
	}
}

func TestKeyChangesWithContent(t *testing.T) {
	f, err := fonts.New(fonts.Config{SizePx: 16})
	if err != nil {
		t.Fatal(err)
	}
	r := New(f, vt.DefaultPalette(), Options{})
	term := vt.New(vt.Options{Cols: 10, Rows: 2})
	term.WriteString("abc")
	l := term.ScreenLine(0).Clone()
	k1 := r.Key(&Row{Line: l, CursorX: -1})
	k2 := r.Key(&Row{Line: l, CursorX: 3, CursorShape: CursorBlock})
	term.WriteString("d")
	k3 := r.Key(&Row{Line: term.ScreenLine(0).Clone(), CursorX: -1})
	if k1 == k2 || k1 == k3 {
		t.Fatal("row keys must differ when the cursor or content changes")
	}
	if k1 != r.Key(&Row{Line: l, CursorX: -1}) {
		t.Fatal("row key must be stable")
	}
}

func BenchmarkDrawRow(b *testing.B) {
	f, err := fonts.New(fonts.Config{SizePx: 16})
	if err != nil {
		b.Fatal(err)
	}
	term := vt.New(vt.Options{Cols: 200, Rows: 1})
	term.WriteString("\x1b[32m" + strings.Repeat("the quick brown fox jumps ", 8))
	r := New(f, vt.DefaultPalette(), Options{})
	row := &Row{Line: term.ScreenLine(0).Clone(), CursorX: -1}
	s := r.Size(200)
	dst := image.NewRGBA(image.Rect(0, 0, s.X, s.Y))
	r.Draw(row, dst) // warm the glyph cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Draw(row, dst)
	}
}
