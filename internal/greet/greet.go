// Package greet draws the banner opal shows when a new terminal opens.
package greet

import (
	"strings"

	"opal/internal/ansi"
	"opal/internal/prompt"
	"opal/internal/theme"
)

// KoFi is where people can support opal.
const KoFi = "https://ko-fi.com/gnail"

const indent = "  "

// shadow is "opal" in figlet's ANSI Shadow font: solid blocks that catch the
// theme's gradient, with a box-drawing shadow painted a shade deeper.
var shadow = []string{
	" ██████╗ ██████╗  █████╗ ██╗",
	"██╔═══██╗██╔══██╗██╔══██╗██║",
	"██║   ██║██████╔╝███████║██║",
	"██║   ██║██╔═══╝ ██╔══██║██║",
	"╚██████╔╝██║     ██║  ██║███████╗",
	" ╚═════╝ ╚═╝     ╚═╝  ╚═╝╚══════╝",
}

const shadowRunes = "╗╔╝╚║═"

// plain is the same word in figlet's standard font, for the bare Linux
// console (icons = "ascii") and terminals too narrow for the big one.
var plain = []string{
	"                   _",
	"  ___  _ __   __ _| |",
	" / _ \\| '_ \\ / _` | |",
	"| (_) | |_) | (_| | |",
	" \\___/| .__/ \\__,_|_|",
	"      |_|",
}

// Render lays the banner out for a terminal width cells wide (0 = unknown).
func Render(p *theme.Palette, icons string, width int) [][]prompt.Span {
	if width <= 0 {
		width = 80
	}
	sym := prompt.SymbolSet(icons)
	muted := p.C("muted")
	gem := prompt.Span{Text: sym.Gem, FG: p.Hue, Bold: true}

	lines := [][]prompt.Span{{}}
	tagline := []prompt.Span{gem, {Text: " prompt and shell tools for zsh, bash, fish and PowerShell", FG: muted}}
	switch {
	case icons != "ascii" && fits(width, artWidth(shadow)):
		lines = append(lines, art(p, shadow)...)
	case fits(width, artWidth(plain)):
		lines = append(lines, art(p, plain)...)
	default:
		tagline = append([]prompt.Span{gem, {Text: " "}}, word(p, "opal")...)
	}
	if fits(width, spansWidth(tagline)) {
		lines = append(lines, append([]prompt.Span{{Text: indent}}, tagline...))
	}

	thanks := []prompt.Span{{Text: "Thanks for using opal.", FG: muted}}
	ask := []prompt.Span{{Text: "You can support it at", FG: muted}}
	link := []prompt.Span{{Text: KoFi, FG: p.Hue}}
	pieces := [][]prompt.Span{thanks, join(ask, link)}
	if !fits(width, spansWidth(pieces[1])) {
		pieces = [][]prompt.Span{thanks, ask, link}
	}
	lines = append(lines, pack(pieces, width)...)
	return append(lines, []prompt.Span{})
}

// art colors the rows along the theme's gradient, shifted two columns per
// row so the colors run diagonally.
func art(p *theme.Palette, rows []string) [][]prompt.Span {
	stops := p.Gradient
	if len(stops) == 0 {
		stops = []ansi.Color{p.Hue}
	}
	grad := ansi.Gradient(stops, artWidth(rows)+2*len(rows))
	frame := p.C("frame")
	out := make([][]prompt.Span, len(rows))
	for r, row := range rows {
		line := []prompt.Span{{Text: indent}}
		for c, ch := range []rune(row) {
			s := prompt.Span{Text: string(ch)}
			switch {
			case ch == ' ':
			case strings.ContainsRune(shadowRunes, ch):
				s.FG = ansi.Mix(grad[c+2*r], frame, 0.55)
			default:
				s.FG = grad[c+2*r]
			}
			line = append(line, s)
		}
		out[r] = line
	}
	return out
}

// word paints s across the gradient, for terminals too narrow for any art.
func word(p *theme.Palette, s string) []prompt.Span {
	runes := []rune(s)
	cols := ansi.Gradient(p.Gradient, len(runes))
	out := make([]prompt.Span, len(runes))
	for i, r := range runes {
		out[i] = prompt.Span{Text: string(r), FG: cols[i], Bold: true}
	}
	return out
}

// pack fills lines greedily with pieces, a space between them.
func pack(pieces [][]prompt.Span, width int) [][]prompt.Span {
	var out [][]prompt.Span
	var cur []prompt.Span
	for _, piece := range pieces {
		if cur != nil && fits(width, spansWidth(cur)+1+spansWidth(piece)) {
			cur = join(cur, piece)
			continue
		}
		if cur != nil {
			out = append(out, append([]prompt.Span{{Text: indent}}, cur...))
		}
		cur = piece
	}
	return append(out, append([]prompt.Span{{Text: indent}}, cur...))
}

func join(a, b []prompt.Span) []prompt.Span {
	out := append([]prompt.Span{}, a...)
	out = append(out, prompt.Span{Text: " "})
	return append(out, b...)
}

func fits(width, w int) bool { return len(indent)+w <= width }

func artWidth(rows []string) int {
	w := 0
	for _, r := range rows {
		w = max(w, ansi.Width(r))
	}
	return w
}

func spansWidth(spans []prompt.Span) int {
	w := 0
	for _, s := range spans {
		w += ansi.Width(s.Text)
	}
	return w
}
