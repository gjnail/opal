package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPickMostSpecificWins(t *testing.T) {
	var cfg struct {
		Aliases map[string]Variants `toml:"aliases"`
	}
	src := `
[aliases]
plain = "git status"
ls    = { macos = "ls -G", linux = "ls --color=auto", "bash.windows" = "ls --color=auto", pwsh = "" }
ll    = { unix = "ls -lh", pwsh = "Get-ChildItem", default = "dir" }
sh    = { sh = "posix", fish = "fishy" }
`
	if _, err := toml.Decode(src, &cfg); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		tgt   Target
		want  string
		found bool
	}{
		{"plain", Target{Shell: "fish", OS: "linux"}, "git status", true},
		{"ls", Target{Shell: "zsh", OS: "macos"}, "ls -G", true},
		{"ls", Target{Shell: "bash", OS: "linux", WSL: true}, "ls --color=auto", true},
		{"ls", Target{Shell: "bash", OS: "windows"}, "ls --color=auto", true},
		{"ls", Target{Shell: "pwsh", OS: "linux"}, "", false},  // explicit opt-out
		{"ls", Target{Shell: "zsh", OS: "freebsd"}, "", false}, // nothing applies
		{"ll", Target{Shell: "pwsh", OS: "windows"}, "Get-ChildItem", true},
		{"ll", Target{Shell: "zsh", OS: "linux"}, "ls -lh", true},
		{"ll", Target{Shell: "bash", OS: "windows"}, "dir", true},
		{"sh", Target{Shell: "zsh", OS: "linux"}, "posix", true},
		{"sh", Target{Shell: "fish", OS: "linux"}, "fishy", true},
		{"sh", Target{Shell: "pwsh", OS: "linux"}, "", false},
	}
	for _, c := range cases {
		got, ok := cfg.Aliases[c.name].Pick(c.tgt)
		if got != c.want || ok != c.found {
			t.Errorf("%s for %+v = (%q, %v), want (%q, %v)", c.name, c.tgt, got, ok, c.want, c.found)
		}
	}
}

func TestKeysOrder(t *testing.T) {
	got := strings.Join(Target{Shell: "bash", OS: "linux", WSL: true}.Keys(), " ")
	want := "bash.wsl bash.linux bash sh.wsl sh.linux sh wsl linux unix default"
	if got != want {
		t.Errorf("keys = %q\nwant  %q", got, want)
	}
}

func TestVariantsMeta(t *testing.T) {
	var cfg struct {
		F map[string]Variants `toml:"functions"`
	}
	src := `
[functions.ji]
desc = "pick"
requires = ["fzf"]
unless = "zoxide"
sh = 'echo hi'
`
	if _, err := toml.Decode(src, &cfg); err != nil {
		t.Fatal(err)
	}
	v := cfg.F["ji"]
	if v.Desc != "pick" || len(v.Requires) != 1 || v.Requires[0] != "fzf" || len(v.Unless) != 1 || v.Values["sh"] != "echo hi" {
		t.Errorf("unexpected variants: %+v", v)
	}
}

func TestBadVariantType(t *testing.T) {
	var cfg struct {
		A map[string]Variants `toml:"aliases"`
	}
	if _, err := toml.Decode("[aliases]\nx = 3\n", &cfg); err == nil {
		t.Error("expected an error for a numeric alias")
	}
}
