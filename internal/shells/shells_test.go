package shells

import (
	"strings"
	"testing"

	"opal/internal/config"
	"opal/internal/platform"
	"opal/internal/theme"
)

func TestPsQuoteIsASCII(t *testing.T) {
	cases := map[string]string{
		"plain":      "'plain'",
		"it's":       "'it''s'",
		"C:\\Users":  "'C:\\Users'",
		"opal ◆":     "(''+'opal '+[string][char]0x25C6)",
		"🦪":          "(''+[char]::ConvertFromUtf32(0x1F9AA))",
		"José's dir": "(''+'Jos'+[string][char]0xE9+'''s dir')",
	}
	for in, want := range cases {
		got := psQuote(in)
		if got != want {
			t.Errorf("psQuote(%q) = %s, want %s", in, got, want)
		}
		if !isASCII(got) {
			t.Errorf("psQuote(%q) is not ASCII: %s", in, got)
		}
	}
}

func TestShAndFishQuote(t *testing.T) {
	if got := shQuote("it's $HOME"); got != `'it'\''s $HOME'` {
		t.Errorf("shQuote = %s", got)
	}
	if got := fishQuote(`a\b'c`); got != `'a\\b\'c'` {
		t.Errorf("fishQuote = %s", got)
	}
	if got := ansiCQuote("\x1b[0m·'\\"); got != `$'\e[0m·\'\\'` {
		t.Errorf("ansiCQuote = %s", got)
	}
}

func testOptions(t *testing.T, cfg *config.Config) Options {
	t.Helper()
	th, err := theme.Load("fire")
	if err != nil {
		t.Fatal(err)
	}
	has := func(c string) bool { return c == "git" || c == "pbcopy" }
	return Options{Cfg: cfg, Theme: th, Bin: "/usr/local/bin/opal", Info: platform.Info{OS: "linux"}, Has: has, TermID: "tty:34816"}
}

func TestGreetingOncePerTerminal(t *testing.T) {
	cfg := config.Defaults()
	cfg.File = "x"
	for _, sh := range Supported {
		opt := testOptions(t, cfg)
		out, _ := Generate(sh, opt)
		if !strings.Contains(out, "greet --hook") || !strings.Contains(out, "OPAL_GREETED") || !strings.Contains(out, "tty:34816") {
			t.Errorf("%s: a new terminal should be greeted", sh)
		}
		opt.Greeted = opt.TermID
		if out, _ := Generate(sh, opt); strings.Contains(out, "greet --hook") {
			t.Errorf("%s: a shell nested in a greeted terminal should stay quiet", sh)
		}
	}
	cfg.Greeting = false
	if out, _ := Generate("bash", testOptions(t, cfg)); strings.Contains(out, "greet --hook") {
		t.Error("greeting = false should turn the banner off")
	}
}

func TestGenerateEveryShell(t *testing.T) {
	cfg := config.Defaults()
	cfg.File = "/home/u/.config/opal/config.toml"
	for _, sh := range Supported {
		out, err := Generate(sh, testOptions(t, cfg))
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(out, "OPAL_SHELL") {
			t.Errorf("%s: missing OPAL_SHELL", sh)
		}
		if sh == "pwsh" && !isASCII(out) {
			for i, r := range out {
				if r > 0x7e {
					t.Fatalf("pwsh init must be ASCII; found %q at %d: ...%s...", r, i, out[max(0, i-40):min(len(out), i+10)])
				}
			}
		}
	}
}

func TestGenerateTranslatesPerShell(t *testing.T) {
	cfg := config.Defaults()
	cfg.File = "x"
	opt := testOptions(t, cfg)

	zsh, _ := Generate("zsh", opt)
	for _, want := range []string{
		"alias -- gst='git status'",
		"alias -- ls='ls --color=auto'", // linux GNU ls
		"function mkcd {",
		"PROMPT='${_opal_ps}'",
	} {
		if !strings.Contains(zsh, want) {
			t.Errorf("zsh output missing %q", want)
		}
	}
	if strings.Contains(zsh, "alias -- pbcopy=") {
		t.Error("pbcopy shim must be skipped when a real pbcopy exists")
	}
	if strings.Contains(zsh, "alias -- dps=") {
		t.Error("docker plugin must be inactive without docker")
	}

	fish, _ := Generate("fish", opt)
	if !strings.Contains(fish, "abbr -a -g -- gst 'git status'") {
		t.Error("fish should use abbreviations by default")
	}
	if strings.Contains(fish, "abbr -a -g -- ll ") {
		t.Error("fish ships its own ll; opal must not override it")
	}

	cfg.Shell.Fish.Abbreviations = false
	fish, _ = Generate("fish", testOptions(t, cfg))
	if !strings.Contains(fish, "function gst --wraps 'git status'") {
		t.Error("fish should fall back to wrapper functions")
	}

	ps, _ := Generate("pwsh", opt)
	for _, want := range []string{
		"function global:gst {",
		"$input | git status @args",
		"function global:mkcd {",
		"$global:OpalSkipped += $__opal_n", // built-ins kept by default
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("pwsh output missing %q", want)
		}
	}
}

func TestUserConfigOverridesPlugins(t *testing.T) {
	cfg := config.Defaults()
	cfg.File = "x"
	cfg.Aliases = map[string]config.Variants{"gst": {Values: map[string]string{"default": "git status -sb"}}}
	out, _ := Generate("bash", testOptions(t, cfg))
	if !strings.Contains(out, "alias -- gst='git status -sb'") || strings.Contains(out, "alias -- gst='git status'\n") {
		t.Error("config alias should replace the plugin's")
	}
}

func TestWrapBase64OnlyWhenNeeded(t *testing.T) {
	if got := Wrap("pwsh", "Write-Host hi"); got != "Write-Host hi" {
		t.Error("ASCII scripts should pass through")
	}
	if got := Wrap("pwsh", "Write-Host 'é'"); !strings.Contains(got, "FromBase64String") || !isASCII(got) {
		t.Error("non-ASCII scripts should be base64-wrapped")
	}
	if got := Wrap("zsh", "echo é"); got != "echo é" {
		t.Error("only PowerShell needs wrapping")
	}
}

func TestCommands(t *testing.T) {
	cfg := config.Defaults()
	cfg.File = "x"
	cfg.Aliases = map[string]config.Variants{"dep": {Values: map[string]string{"default": "make deploy"}}}
	has := func(c string) bool { return c == "git" }
	names := func(shell string) map[string]bool {
		m := map[string]bool{}
		for _, d := range Commands(shell, cfg, platform.Info{OS: "linux"}, has) {
			m[d.Name] = true
		}
		return m
	}
	zsh := names("zsh")
	for _, n := range []string{"gst", "gp", "gcm", "mkcd", "dep", "-"} {
		if !zsh[n] {
			t.Errorf("zsh should list %s", n)
		}
	}
	if zsh["dps"] {
		t.Error("docker isn't installed, so its aliases shouldn't be listed")
	}
	// PowerShell's own gp and gcm win over opal's, so they aren't opal's
	// to list; "-" isn't a name PowerShell can define.
	pwsh := names("powershell")
	for _, n := range []string{"gp", "gcm", "-"} {
		if pwsh[n] {
			t.Errorf("pwsh shouldn't list %s", n)
		}
	}
	if !pwsh["gst"] || !pwsh["dep"] {
		t.Error("pwsh should list gst and dep")
	}
	cfg.Shell.Pwsh.ClobberBuiltinAliases = true
	if !names("pwsh")["gp"] {
		t.Error("with clobber_builtin_aliases, gp is opal's")
	}
	if Commands("cmd", cfg, platform.Info{OS: "windows"}, has) != nil {
		t.Error("cmd isn't a shell opal sets up")
	}
}
