package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"opal/internal/config"
	"opal/internal/platform"
	"opal/internal/plugin"
)

func pluginDir() string { return filepath.Join(platform.ConfigDir(), "plugins") }

var shortRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// repoURL expands "user/repo" to a GitHub URL; full URLs and paths pass through.
func repoURL(s string) string {
	if shortRepo.MatchString(s) && !strings.HasPrefix(s, ".") {
		return "https://github.com/" + s + ".git"
	}
	return s
}

// pluginName derives a plugin's directory name from its repo:
// someone/opal-plugin-terraform.git -> terraform.
func pluginName(src string) string {
	n := filepath.Base(strings.TrimSuffix(strings.TrimRight(src, "/\\"), ".git"))
	for _, p := range []string{"opal-plugin-", "opal-", "plugin-"} {
		n = strings.TrimPrefix(n, p)
	}
	return n
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// pluginInstall clones a plugin repo into ~/.config/opal/plugins/<name> and
// enables it. A repo holds plugin.toml (plus optional init.<shell> files).
func pluginInstall(cfg *config.Config, src string) int {
	u := newUI()
	if !platform.HasCommand("git") {
		errorf("installing plugins needs git")
		return 1
	}
	name := pluginName(src)
	dest := filepath.Join(pluginDir(), name)
	if _, err := os.Stat(dest); err == nil {
		errorf("%s is already installed at %s (opal plugin update %s)", name, dest, name)
		return 1
	}
	if err := os.MkdirAll(pluginDir(), 0o755); err != nil {
		errorf("%v", err)
		return 1
	}
	url := repoURL(src)
	fmt.Printf("  %s cloning %s\n", u.muted("→"), url)
	if err := runGit("", "clone", "--depth", "1", "--quiet", url, dest); err != nil {
		errorf("clone failed: %v", err)
		return 1
	}
	if _, err := os.Stat(filepath.Join(dest, "plugin.toml")); err != nil {
		os.RemoveAll(dest)
		errorf("%s has no plugin.toml at its root, so it isn't an opal plugin", url)
		return 1
	}
	all, _ := plugin.Available()
	p, ok := all[name]
	if !ok {
		// The TOML may declare its own name; find what we just installed.
		for n, q := range all {
			if strings.HasPrefix(q.Source, dest) {
				name, p, ok = n, q, true
			}
		}
	}
	if !ok {
		errorf("installed, but its plugin.toml didn't load (see: opal plugin list)")
		return 1
	}
	if !cfg.HasPlugin(name) {
		if err := config.SetKey("plugins", config.QuoteList(append(append([]string(nil), cfg.Plugins...), name))); err != nil {
			errorf("%v", err)
			return 1
		}
	}
	fmt.Printf("  %s %s %s %s\n", u.ok("✓"), u.bold(name), u.muted(p.Description), u.muted("(run `reload` to use it)"))
	return 0
}

// pluginUpdate pulls every git-installed plugin (or just the named ones).
func pluginUpdate(names []string) int {
	u := newUI()
	ents, err := os.ReadDir(pluginDir())
	if err != nil {
		fmt.Println("  no installed plugins")
		return 0
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	code := 0
	for _, e := range ents {
		dir := filepath.Join(pluginDir(), e.Name())
		if !e.IsDir() || (len(want) > 0 && !want[e.Name()]) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue // hand-made plugin, nothing to pull
		}
		if err := runGit(dir, "pull", "--ff-only", "--quiet"); err != nil {
			fmt.Printf("  %s %s %v\n", u.bad("✗"), e.Name(), err)
			code = 1
			continue
		}
		fmt.Printf("  %s %s\n", u.ok("✓"), e.Name())
	}
	return code
}

// pluginRemove deletes an installed plugin and disables it.
func pluginRemove(cfg *config.Config, name string) int {
	u := newUI()
	dir := filepath.Join(pluginDir(), name)
	file := filepath.Join(pluginDir(), name+".toml")
	removed := false
	for _, p := range []string{dir, file} {
		if _, err := os.Stat(p); err == nil {
			if err := os.RemoveAll(p); err != nil {
				errorf("%v", err)
				return 1
			}
			removed = true
		}
	}
	if cfg.HasPlugin(name) {
		if err := config.SetKey("plugins", config.QuoteList(removeStr(append([]string(nil), cfg.Plugins...), name))); err != nil {
			errorf("%v", err)
			return 1
		}
	}
	if !removed {
		fmt.Printf("  %s %s wasn't installed (built-in plugins can only be disabled)\n", u.muted("·"), name)
		return 0
	}
	fmt.Printf("  %s removed %s\n", u.ok("✓"), name)
	return 0
}
