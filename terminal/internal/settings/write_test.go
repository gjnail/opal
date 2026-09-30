package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# opal config
theme = "fire"   # the prompt theme

[prompt]
transient = true

[terminal]
font_size = 12   # points
font_family = [
  "Cascadia Mono",
]

[terminal.keys]
"ctrl+shift+t" = "new_tab"

[[terminal.profiles]]
name = "x"
command = "y"
`

func TestSetInTable(t *testing.T) {
	out := setInTable(sample, "terminal", "font_size", "14")
	if !strings.Contains(out, "font_size = 14  # points") {
		t.Fatalf("replace keeping comment:\n%s", out)
	}
	out = setInTable(out, "terminal", "font_family", `["JetBrains Mono"]`)
	if !strings.Contains(out, `font_family = ["JetBrains Mono"]`) || strings.Contains(out, `"Cascadia Mono",`) {
		t.Fatalf("multi-line array replace:\n%s", out)
	}
	out = setInTable(out, "terminal", "cursor_style", `"bar"`)
	if !strings.Contains(out, "font_family = [\"JetBrains Mono\"]\ncursor_style = \"bar\"\n\n[terminal.keys]") {
		t.Fatalf("insert at end of table:\n%s", out)
	}
	out = setInTable(out, "terminal.keys", "ctrl+shift+t", `"none"`)
	out = setInTable(out, "terminal.keys", "ctrl+alt+k", `"clear_scrollback"`)
	if !strings.Contains(out, `"ctrl+shift+t" = "none"`) || !strings.Contains(out, `"ctrl+alt+k" = "clear_scrollback"`) {
		t.Fatalf("keys table:\n%s", out)
	}
	out = setInTable(out, "terminal.clipboard", "read", "true")
	if !strings.HasSuffix(strings.TrimSpace(out), "[terminal.clipboard]\nread = true") {
		t.Fatalf("new table appended:\n%s", out)
	}
	// The CLI's parts are untouched.
	if !strings.Contains(out, `theme = "fire"   # the prompt theme`) || !strings.Contains(out, "[[terminal.profiles]]\nname = \"x\"") {
		t.Fatalf("other content changed:\n%s", out)
	}
}

func TestSetAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPAL_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set("terminal", "scrollback", "5000"); err != nil {
		t.Fatal(err)
	}
	if err := Set("terminal.clipboard", "read", "true"); err != nil {
		t.Fatal(err)
	}
	if err := Set("", "background", Quote("light")); err != nil {
		t.Fatal(err)
	}
	c := Load()
	if len(c.Warnings) > 0 {
		t.Fatal(c.Warnings)
	}
	if c.Scrollback != 5000 || !c.Clipboard.Read || c.Background != "light" || c.FontFamily[0] != "Cascadia Mono" {
		t.Fatalf("loaded %+v", c)
	}
	if err := Unset("terminal", "scrollback"); err != nil {
		t.Fatal(err)
	}
	if Load().Scrollback != Defaults().Scrollback {
		t.Fatal("unset should restore the default")
	}
}
