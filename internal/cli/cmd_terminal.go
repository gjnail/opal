package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"opal/internal/platform"
)

// cmdTerminal starts Opal Terminal, the terminal emulator built from
// terminal/ in this repository. It's a separate program with its own
// dependencies, so this only finds and runs it.
func cmdTerminal(args []string) int {
	exe := findTerminal()
	if exe == "" {
		errorf("Opal Terminal isn't installed. The install scripts add it next to opal: https://github.com/gjnail/opal#install")
		return 1
	}
	cmd := exec.Command(exe, args...)
	if printsAndExits(args) {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			errorf("%v", err)
			return 1
		}
		return 0
	}
	// Open in the directory opal was run from, unless the arguments say
	// otherwise.
	if !hasFlag(args, "dir") {
		if wd, err := os.Getwd(); err == nil {
			cmd.Args = append([]string{exe, "-dir", wd}, args...)
		}
	}
	startDetached(cmd)
	if err := cmd.Start(); err != nil {
		errorf("starting %s: %v", exe, err)
		return 1
	}
	_ = cmd.Process.Release()
	return 0
}

// findTerminal looks where the install scripts put Opal Terminal, then on
// PATH.
func findTerminal() string {
	name := "opal-terminal"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var candidates []string
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}
		dir := filepath.Dir(self)
		// Linux installs both into ~/.local/bin; Windows keeps the terminal
		// and its ConPTY files in their own folder beside opal's bin.
		candidates = append(candidates, filepath.Join(dir, name), filepath.Join(dir, "..", "terminal", name))
	}
	if p, ok := platform.LookPath("opal-terminal"); ok {
		candidates = append(candidates, p)
	}
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			candidates = append(candidates, filepath.Join(d, "opal", "terminal", name))
		}
	case "darwin":
		app := filepath.Join("Opal Terminal.app", "Contents", "MacOS", name)
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, "Applications", app))
		}
		candidates = append(candidates, filepath.Join("/Applications", app))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.Clean(c)
		}
	}
	return ""
}

// printsAndExits reports whether args ask Opal Terminal to print something
// instead of opening a window, so we wait for it and pass its output on.
func printsAndExits(args []string) bool {
	return hasFlag(args, "version") || hasFlag(args, "list-profiles") ||
		hasFlag(args, "h") || hasFlag(args, "help")
}

// hasFlag reports whether args set the Go-style flag name (-name, --name,
// -name=value) before the first non-flag argument, which Opal Terminal runs
// as a command.
func hasFlag(args []string, name string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") {
			return false
		}
		f := strings.TrimLeft(a, "-")
		f, _, hasValue := strings.Cut(f, "=")
		if f == name {
			return true
		}
		// Skip the value of the flags that take one.
		if !hasValue && (f == "dir" || f == "profile") {
			i++
		}
	}
	return false
}
