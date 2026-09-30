package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"opal/internal/config"
	"opal/internal/git"
	"opal/internal/platform"
	"opal/internal/tools"
)

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func nowUnix() int64 { return time.Now().Unix() }

func cmdOpen(args []string) int {
	if len(args) == 0 {
		args = []string{"."}
	}
	code := 0
	for _, a := range args {
		if err := tools.Open(a); err != nil {
			errorf("open %s: %v", a, err)
			code = 1
		}
	}
	return code
}

func cmdClip(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal clip copy [text] | paste")
		return 2
	}
	switch args[0] {
	case "copy":
		var text string
		if len(args) > 1 {
			text = strings.Join(args[1:], " ")
		} else {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				errorf("%v", err)
				return 1
			}
			text = string(b)
		}
		if err := tools.Copy(text); err != nil {
			errorf("copy: %v", err)
			return 1
		}
		return 0
	case "paste":
		s, err := tools.Paste()
		if err != nil {
			errorf("paste: %v", err)
			return 1
		}
		fmt.Print(s)
		return 0
	case "backend":
		fmt.Println(tools.ClipBackend())
		return 0
	}
	errorf("usage: opal clip copy [text] | paste")
	return 2
}

func cmdExtract(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal extract <archive>...")
		return 2
	}
	u := newUI()
	code := 0
	for _, a := range args {
		dest, n, err := tools.Extract(a)
		if err != nil {
			errorf("%v", err)
			code = 1
			continue
		}
		noun := "files"
		if n == 1 {
			noun = "file"
		}
		fmt.Printf("%s %s %s %s %s\n", u.ok("✓"), filepath.Base(a), u.muted("→"), u.bold(dest+string(filepath.Separator)), u.muted(fmt.Sprintf("(%d %s)", n, noun)))
	}
	return code
}

func cmdPorts(args []string) int {
	ls, err := tools.Ports()
	if err != nil {
		errorf("ports: %v", err)
		return 1
	}
	filter := 0
	if len(args) > 0 {
		filter, _ = strconv.Atoi(args[0])
	}
	u := newUI()
	fmt.Printf("%s  %s  %s  %s\n", u.muted(pad("PORT", 6)), u.muted(pad("PID", 7)), u.muted(pad("PROCESS", 22)), u.muted("ADDRESS"))
	shown := 0
	for _, l := range ls {
		if filter != 0 && l.Port != filter {
			continue
		}
		pid := "-"
		if l.PID > 0 {
			pid = strconv.Itoa(l.PID)
		}
		proc := l.Process
		if proc == "" {
			proc = "?"
		}
		fmt.Printf("%s  %s  %s  %s\n", u.accent(pad(strconv.Itoa(l.Port), 6)), pad(pid, 7), pad(proc, 22), u.muted(strings.Join(l.Addrs, ", ")))
		shown++
	}
	if shown == 0 && filter != 0 {
		fmt.Println(u.muted(fmt.Sprintf("nothing listening on %d", filter)))
		return 1
	}
	return 0
}

func pad(s string, n int) string {
	if len([]rune(s)) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func cmdPath(args []string) int {
	u := newUI()
	seen := map[string]int{}
	for i, p := range filepath.SplitList(os.Getenv("PATH")) {
		key := filepath.Clean(p)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		note := ""
		if first, dup := seen[key]; dup {
			note = u.warn(fmt.Sprintf("  duplicate of #%d", first))
		} else if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			note = u.bad("  missing")
		}
		if _, dup := seen[key]; !dup {
			seen[key] = i + 1
		}
		fmt.Printf("%s %s%s\n", u.muted(fmt.Sprintf("%3d", i+1)), platform.ShellPath(p, currentShell()), note)
	}
	return 0
}

func cmdPkg(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal pkg <%s> [package...]", strings.Join(tools.Verbs, "|"))
		return 2
	}
	cfg, _ := config.Load()
	dry := false
	if args[0] == "--dry-run" || args[0] == "-n" {
		dry, args = true, args[1:]
		if len(args) == 0 {
			errorf("usage: opal pkg --dry-run <verb> [package...]")
			return 2
		}
	}
	argv, name, err := tools.PkgCommand(cfg.Pkg.Prefer, args[0], args[1:])
	if err != nil {
		errorf("%v", err)
		return 2
	}
	u := newUI()
	fmt.Fprintf(os.Stderr, "%s %s\n", u.muted("→ "+name+":"), strings.Join(argv, " "))
	if dry {
		return 0
	}
	return tools.Exec(argv)
}

func cmdJS(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal js <install|add|remove|run|test|dlx|exec> [args]")
		return 2
	}
	cwd, _ := os.Getwd()
	pm, why := tools.DetectJS(cwd)
	argv, err := tools.JSCommand(pm, args[0], args[1:])
	if err != nil {
		errorf("%v", err)
		return 2
	}
	u := newUI()
	fmt.Fprintf(os.Stderr, "%s %s\n", u.muted("→ "+pm+" ("+why+"):"), strings.Join(argv, " "))
	return tools.Exec(argv)
}

func cmdVenv(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal venv create [dir] | activate-script [--shell s] [dir]")
		return 2
	}
	switch args[0] {
	case "create":
		dir := ".venv"
		if len(args) > 1 {
			dir = args[1]
		}
		py, err := tools.Python()
		if err != nil {
			errorf("%v", err)
			return 1
		}
		argv := append(py, "-m", "venv", platform.FromMSYS(dir))
		fmt.Fprintf(os.Stderr, "%s %s\n", newUI().muted("→"), strings.Join(argv, " "))
		return tools.Exec(argv)
	case "activate-script":
		shell, name := currentShell(), ""
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--shell" && i+1 < len(rest) {
				shell = rest[i+1]
				i++
				continue
			}
			name = rest[i]
		}
		cwd, _ := os.Getwd()
		venv, err := tools.FindVenv(cwd, name)
		if err != nil {
			errorf("%v", err)
			return 1
		}
		script, err := tools.ActivateScript(venv, shell)
		if err != nil {
			errorf("%v", err)
			return 1
		}
		fmt.Println(script)
		return 0
	}
	errorf("usage: opal venv create [dir] | activate-script [--shell s] [dir]")
	return 2
}

func cmdGit(args []string) int {
	switch {
	case len(args) > 0 && args[0] == "main-branch":
		fmt.Println(git.MainBranch())
		return 0
	case len(args) > 1 && args[0] == "refresh":
		// Run detached by the prompt when git status missed its time budget.
		if err := git.Refresh(gitCacheDir(), args[1]); err != nil {
			return 1
		}
		return 0
	}
	errorf("usage: opal git main-branch")
	return 2
}

func gitCacheDir() string { return filepath.Join(platform.DataDir(), "cache") }

// refreshGitInBackground starts `opal git refresh <root>` detached, so the
// prompt returns now and the next one has fresh counts.
func refreshGitInBackground(root string) {
	cmd := exec.Command(binPath(), "git", "refresh", root)
	platform.Detach(cmd) // stdio stays nil: never hold the pipe the shell reads
	if cmd.Start() == nil {
		cmd.Process.Release()
	}
}
