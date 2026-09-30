// Package tools holds the everyday helpers that behave identically on every
// OS: open, clipboard, extract, ports, path, pkg, js and venv.
package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"opal/internal/platform"
)

var urlLike = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:(//|[^\\/])`)

// Open opens a file, directory or URL with the desktop's default handler.
func Open(target string) error {
	isURL := urlLike.MatchString(target) && !(runtime.GOOS == "windows" && filepath.VolumeName(target) != "")
	if !isURL {
		abs, err := filepath.Abs(platform.FromMSYS(target))
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		target = abs
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Run()
	case "windows":
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			return exec.Command("explorer.exe", target).Start()
		}
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target).Start()
	}
	in := platform.Detect()
	if in.WSL {
		if platform.HasCommand("wslview") {
			return exec.Command("wslview", target).Run()
		}
		arg := target
		if !isURL {
			if out, err := exec.Command("wslpath", "-w", target).Output(); err == nil {
				arg = strings.TrimSpace(string(out))
			}
		}
		// explorer.exe returns 1 even on success.
		_ = exec.Command("explorer.exe", arg).Run()
		return nil
	}
	if in.Termux && platform.HasCommand("termux-open") {
		return exec.Command("termux-open", target).Run()
	}
	for _, c := range [][]string{{"xdg-open"}, {"gio", "open"}, {"gnome-open"}, {"kde-open"}} {
		if platform.HasCommand(c[0]) {
			cmd := exec.Command(c[0], append(c[1:], target)...)
			return cmd.Start()
		}
	}
	return errors.New("no opener found (install xdg-utils)")
}
