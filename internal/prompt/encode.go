package prompt

import (
	"html"
	"strings"
)

// Encode turns lines of spans into the bytes a shell's prompt needs.
//
//	zsh   wraps escapes in %{ %} and doubles literal %
//	bash  wraps escapes in \001 \002 (readline's raw ignore markers, which
//	      work even though the prompt arrives through ${_opal_ps})
//	fish, pwsh, raw  plain escapes
func Encode(lines [][]Span, shell string, depth int) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		var cur style
		for _, s := range line {
			if s.Seq != "" { // invisible control sequence (OSC 133/7 marks)
				writeSeq(&b, shell, s.Seq)
				continue
			}
			st := style{s.FG.FG(depth), s.BG.BG(depth), s.Bold && depth > 0}
			if st != cur {
				writeSGR(&b, shell, st)
				cur = st
			}
			b.WriteString(escapeText(shell, s.Text))
		}
		if cur != (style{}) {
			writeSeq(&b, shell, "\x1b[0m")
		}
	}
	return b.String()
}

type style struct {
	fg, bg string
	bold   bool
}

func writeSGR(b *strings.Builder, shell string, st style) {
	params := []string{"0"}
	if st.bold {
		params = append(params, "1")
	}
	if st.fg != "" {
		params = append(params, st.fg)
	}
	if st.bg != "" {
		params = append(params, st.bg)
	}
	writeSeq(b, shell, "\x1b["+strings.Join(params, ";")+"m")
}

func writeSeq(b *strings.Builder, shell, seq string) {
	switch shell {
	case "zsh":
		b.WriteString("%{" + seq + "%}")
	case "bash":
		b.WriteString("\x01" + seq + "\x02")
	default:
		b.WriteString(seq)
	}
}

func escapeText(shell, s string) string {
	if shell == "zsh" {
		return strings.ReplaceAll(s, "%", "%%")
	}
	return s
}

// HTML renders spans as HTML (for `opal theme preview --html`).
func HTML(lines [][]Span) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, s := range line {
			var css []string
			if s.FG.Set {
				css = append(css, "color:"+s.FG.Hex())
			}
			if s.BG.Set {
				css = append(css, "background:"+s.BG.Hex())
			}
			if s.Bold {
				css = append(css, "font-weight:700")
			}
			text := html.EscapeString(s.Text)
			if len(css) == 0 {
				b.WriteString(text)
				continue
			}
			b.WriteString(`<span style="` + strings.Join(css, ";") + `">` + text + `</span>`)
		}
	}
	return b.String()
}
