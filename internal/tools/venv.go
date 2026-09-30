package tools

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"opal/internal/platform"
)

// FindVenv searches dir and its parents for a virtualenv (a directory with
// pyvenv.cfg). If name is given, only that directory name is considered.
func FindVenv(dir, name string) (string, error) {
	if name != "" {
		p := platform.FromMSYS(name)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, err := os.Stat(filepath.Join(p, "pyvenv.cfg")); err == nil {
			return p, nil
		}
	}
	names := []string{".venv", "venv", "env", ".env"}
	for d := dir; ; {
		for _, n := range names {
			p := filepath.Join(d, n)
			if _, err := os.Stat(filepath.Join(p, "pyvenv.cfg")); err == nil {
				return p, nil
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", errors.New("no virtualenv found here or above (try: mkvenv)")
		}
		d = parent
	}
}

// ActivateScript returns the activation script this shell needs: bin/ vs
// Scripts/, and activate / activate.fish / Activate.ps1.
func ActivateScript(venv, shell string) (string, error) {
	bin := "bin"
	if runtime.GOOS == "windows" {
		bin = "Scripts"
	}
	file := map[string]string{"fish": "activate.fish", "pwsh": "Activate.ps1"}[shell]
	if file == "" {
		file = "activate"
	}
	p := filepath.Join(venv, bin, file)
	if _, err := os.Stat(p); err != nil {
		return "", errors.New(p + " not found (this venv has no " + shell + " activation script)")
	}
	return platform.ShellPath(p, shell), nil
}

// Python returns argv for this OS's Python 3.
func Python() ([]string, error) {
	switch {
	case runtime.GOOS != "windows" && platform.HasCommand("python3"):
		return []string{"python3"}, nil
	case runtime.GOOS == "windows" && platform.HasCommand("py"):
		return []string{"py", "-3"}, nil
	case platform.HasCommand("python"):
		return []string{"python"}, nil
	case platform.HasCommand("python3"):
		return []string{"python3"}, nil
	}
	return nil, errors.New("python 3 not found")
}
