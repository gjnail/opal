package shells

import (
	"fmt"
	"regexp"
	"strings"
)

// shQuote single-quotes for bash/zsh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ansiCQuote renders $'...' for bash/zsh so escape bytes survive.
func ansiCQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case r == 0x1b:
			b.WriteString(`\e`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\%03o`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("'")
	return b.String()
}

// fishQuote single-quotes for fish (only \\ and \' are special inside).
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// psQuote returns a PowerShell expression for s that is pure ASCII.
// PowerShell decodes native output with the console code page (often 437),
// so non-ASCII characters are spelled as [char] codes instead.
func psQuote(s string) string {
	ascii := true
	for _, r := range s {
		if r > 0x7e {
			ascii = false
			break
		}
	}
	if ascii {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	var parts []string
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			parts = append(parts, "'"+strings.ReplaceAll(run.String(), "'", "''")+"'")
			run.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r > 0xffff:
			flush()
			parts = append(parts, fmt.Sprintf("[char]::ConvertFromUtf32(0x%X)", r))
		case r > 0x7e || r < 0x20:
			flush()
			parts = append(parts, fmt.Sprintf("[string][char]0x%X", r))
		default:
			run.WriteRune(r)
		}
	}
	flush()
	return "(''+" + strings.Join(parts, "+") + ")"
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 0x7e {
			return false
		}
	}
	return true
}

var (
	shName   = regexp.MustCompile(`^[A-Za-z0-9_.:+@%-]+$`)
	fishName = regexp.MustCompile(`^[A-Za-z0-9_.:+@%][A-Za-z0-9_.:+@%-]*$`)
	psName   = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)
	envName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// firstWord returns the command word of an alias body.
func firstWord(body string) string {
	f := strings.Fields(body)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
