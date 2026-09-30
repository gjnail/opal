package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	cmdOnce  sync.Once
	cmdIndex map[string]string // command name -> full path (first on PATH wins)
)

// HasCommand reports whether an executable is on PATH. It lists each PATH
// directory once instead of probing name×PATHEXT×dirs, which keeps
// `opal init` fast on Windows where stat calls are expensive.
func HasCommand(name string) bool {
	_, ok := LookPath(name)
	return ok
}

// LookPath is exec.LookPath answered from the same one-pass PATH index:
// on Windows, each exec.LookPath costs a stat per directory per PATHEXT.
func LookPath(name string) (string, bool) {
	cmdOnce.Do(buildIndex)
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	p, ok := cmdIndex[name]
	return p, ok
}

func buildIndex() {
	cmdIndex = map[string]string{}
	add := func(name, path string) {
		if _, taken := cmdIndex[name]; !taken {
			cmdIndex[name] = path
		}
	}
	var exts []string
	if runtime.GOOS == "windows" {
		for _, e := range strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";") {
			if e != "" {
				exts = append(exts, e)
			}
		}
		if len(exts) == 0 {
			exts = []string{".com", ".exe", ".bat", ".cmd"}
		}
	}
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if runtime.GOOS != "windows" {
				add(e.Name(), full)
				continue
			}
			n := strings.ToLower(e.Name())
			for _, ext := range exts {
				if strings.HasSuffix(n, ext) {
					add(n, full)
					add(strings.TrimSuffix(n, ext), full)
					break
				}
			}
		}
	}
}
