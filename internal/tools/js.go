package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectJS finds the JavaScript package manager for the project containing
// dir: lockfile first, then package.json's "packageManager", else npm.
func DetectJS(dir string) (manager, reason string) {
	locks := []struct{ file, pm string }{
		{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"}, {"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"},
	}
	for d := dir; ; {
		for _, l := range locks {
			if _, err := os.Stat(filepath.Join(d, l.file)); err == nil {
				return l.pm, l.file
			}
		}
		if b, err := os.ReadFile(filepath.Join(d, "package.json")); err == nil {
			var pj struct {
				PackageManager string `json:"packageManager"`
			}
			if json.Unmarshal(b, &pj) == nil && pj.PackageManager != "" {
				name, _, _ := strings.Cut(pj.PackageManager, "@")
				return name, "package.json packageManager"
			}
			return "npm", "package.json (no lockfile)"
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "npm", "default"
		}
		d = parent
	}
}

// JSCommand maps a verb onto the detected manager's spelling.
func JSCommand(pm, verb string, args []string) ([]string, error) {
	type row map[string][]string
	table := map[string]row{
		"install": {"npm": {"npm", "install"}, "pnpm": {"pnpm", "install"}, "yarn": {"yarn", "install"}, "bun": {"bun", "install"}},
		"add":     {"npm": {"npm", "install"}, "pnpm": {"pnpm", "add"}, "yarn": {"yarn", "add"}, "bun": {"bun", "add"}},
		"remove":  {"npm": {"npm", "uninstall"}, "pnpm": {"pnpm", "remove"}, "yarn": {"yarn", "remove"}, "bun": {"bun", "remove"}},
		"run":     {"npm": {"npm", "run"}, "pnpm": {"pnpm", "run"}, "yarn": {"yarn", "run"}, "bun": {"bun", "run"}},
		"test":    {"npm": {"npm", "test"}, "pnpm": {"pnpm", "test"}, "yarn": {"yarn", "test"}, "bun": {"bun", "test"}},
		"dlx":     {"npm": {"npx"}, "pnpm": {"pnpm", "dlx"}, "yarn": {"yarn", "dlx"}, "bun": {"bunx"}},
		"exec":    {"npm": {"npm", "exec", "--"}, "pnpm": {"pnpm", "exec"}, "yarn": {"yarn", "exec"}, "bun": {"bun", "x"}},
	}
	r, ok := table[verb]
	if !ok {
		return nil, fmt.Errorf("unknown verb %q (install, add, remove, run, test, dlx, exec)", verb)
	}
	base, ok := r[pm]
	if !ok {
		return nil, fmt.Errorf("unsupported package manager %q", pm)
	}
	return append(append([]string(nil), base...), args...), nil
}
