package greet

import (
	"strings"
	"testing"

	"opal/internal/ansi"
	"opal/internal/prompt"
	"opal/internal/theme"
)

func render(t *testing.T, icons string, width int) []string {
	t.Helper()
	th, err := theme.Load("fire")
	if err != nil {
		t.Fatal(err)
	}
	out := prompt.Encode(Render(th.Resolve("dark", "bash"), icons, width), "", 0)
	return strings.Split(out, "\n")
}

func TestBannerFitsAndThanks(t *testing.T) {
	for _, icons := range []string{"unicode", "nerd", "ascii"} {
		for _, width := range []int{36, 44, 50, 60, 79, 80, 200} {
			lines := render(t, icons, width)
			for _, l := range lines {
				if w := ansi.Width(l); w > width {
					t.Errorf("%s @%d: line is %d wide: %q", icons, width, w, l)
				}
			}
			all := strings.Join(lines, " ")
			if !strings.Contains(all, "Thanks for using opal.") || !strings.Contains(all, KoFi) {
				t.Errorf("%s @%d: missing thanks or Ko-fi link:\n%s", icons, width, strings.Join(lines, "\n"))
			}
		}
	}
}

func TestBannerArtAdapts(t *testing.T) {
	has := func(lines []string, s string) bool { return strings.Contains(strings.Join(lines, "\n"), s) }
	if l := render(t, "unicode", 80); !has(l, "███╗") {
		t.Errorf("wide unicode terminal should get the block art:\n%s", strings.Join(l, "\n"))
	}
	if l := render(t, "unicode", 30); has(l, "█") || !has(l, `\__,_|_|`) {
		t.Errorf("narrow terminal should get the figlet art:\n%s", strings.Join(l, "\n"))
	}
	l := render(t, "ascii", 80)
	for _, line := range l {
		for _, r := range line {
			if r > 0x7e {
				t.Fatalf("ascii banner has %q: %q", r, line)
			}
		}
	}
	if l := render(t, "unicode", 0); !has(l, "███╗") {
		t.Errorf("unknown width should assume 80 columns")
	}
}
