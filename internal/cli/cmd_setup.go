package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"opal/internal/config"
	"opal/internal/history"
	"opal/internal/platform"
)

const (
	markStart = "# >>> opal >>>"
	markEnd   = "# <<< opal <<<"
)

// rcTarget is one shell startup file opal hooks into.
type rcTarget struct {
	shell string // display name
	file  string
	body  string // lines between the markers
	whole bool   // opal owns the entire file (fish conf.d)
	note  string
}

func cmdSetup(args []string) int {
	dry, remove := false, false
	only := map[string]bool{}
	for _, a := range args {
		switch a {
		case "--dry-run", "-n":
			dry = true
		case "--remove", "--uninstall":
			remove = true
		default:
			only[a] = true
		}
	}
	u := newUI()
	fmt.Printf("\n  %s %s\n\n", u.gem(), u.gradient("opal setup"))

	if !remove {
		if dry {
			fmt.Printf("  %s config %s\n", u.muted("would ensure"), config.Path())
		} else if p, created, err := config.WriteDefault(); err != nil {
			errorf("writing config: %v", err)
			return 1
		} else if created {
			fmt.Printf("  %s wrote %s\n", u.ok("✓"), p)
		} else {
			fmt.Printf("  %s config %s\n", u.muted("·"), p)
		}
	}

	targets := rcTargets()
	if len(targets) == 0 {
		fmt.Println("  no supported shells found (zsh, bash, fish, pwsh)")
		return 1
	}
	code := 0
	for _, t := range targets {
		if len(only) > 0 && !only[t.shell] && !only[strings.Fields(t.shell)[0]] {
			continue
		}
		verb, err := applyTarget(t, remove, dry)
		switch {
		case err != nil:
			fmt.Printf("  %s %-18s %s  %s\n", u.bad("✗"), t.shell, t.file, u.bad(err.Error()))
			code = 1
		default:
			mark := u.ok("✓")
			if verb == "unchanged" || verb == "absent" {
				mark = u.muted("·")
			}
			fmt.Printf("  %s %-18s %s  %s\n", mark, t.shell, t.file, u.muted(verb))
		}
		if t.note != "" && !remove {
			fmt.Printf("    %s\n", u.warn(t.note))
		}
	}
	if !remove && !dry {
		setupHistory(u)
		offerPSReadLine(u)
		fmt.Printf("\n  Open a new terminal (or run %s) to see it.\n", u.accent("exec $SHELL"))
		fmt.Printf("  %s Tab/→ accept suggestions · Ctrl+R searches every shell's history · opal doctor\n\n", u.muted("try:"))
	}
	return code
}

// setupHistory seeds the shared history from the shells' own history files
// the first time, so Ctrl+R is useful from day one.
func setupHistory(u *ui) {
	cfg, _ := config.Load()
	if !cfg.History.Shared || history.Exists() {
		return
	}
	srcs := history.Sources()
	if len(srcs) == 0 {
		return
	}
	n, err := history.Import(srcs)
	if err != nil {
		fmt.Printf("  %s importing history: %v\n", u.bad("✗"), err)
		return
	}
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.Shell
	}
	fmt.Printf("  %s imported %d commands from %s into the shared history\n", u.ok("✓"), n, strings.Join(names, ", "))
}

// offerPSReadLine: Windows PowerShell ships PSReadLine 2.0, which can't show
// as-you-type suggestions. Offer the upgrade (it downloads from the
// PowerShell Gallery, so ask first; only when someone is there to answer).
func offerPSReadLine(u *ui) {
	if runtime.GOOS != "windows" || !platform.HasCommand("powershell") {
		return
	}
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-Module PSReadLine -ListAvailable | Sort-Object Version -Descending | Select-Object -First 1).Version.ToString()").Output()
	if err != nil {
		return
	}
	v := strings.TrimSpace(string(out))
	if v == "" || !(strings.HasPrefix(v, "1.") || strings.HasPrefix(v, "2.0.")) {
		return
	}
	fmt.Printf("\n  %s Windows PowerShell has PSReadLine %s, which can't show grey suggestions as you type.\n", u.warn("!"), v)
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		fmt.Printf("    %s Install-Module PSReadLine -Scope CurrentUser -Force -SkipPublisherCheck\n", u.muted("fix:"))
		return
	}
	fmt.Printf("    Install the current version from the PowerShell Gallery (for your user only)? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if !strings.HasPrefix(strings.ToLower(answer), "y") {
		fmt.Printf("    %s Install-Module PSReadLine -Scope CurrentUser -Force -SkipPublisherCheck\n", u.muted("later:"))
		return
	}
	script := "[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12; " +
		"if (-not (Get-PackageProvider -ListAvailable -Name NuGet -ErrorAction SilentlyContinue)) { Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Scope CurrentUser -Force | Out-Null }; " +
		"Install-Module PSReadLine -Scope CurrentUser -Repository PSGallery -Force -SkipPublisherCheck -AllowClobber"
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("    %s %v\n", u.bad("✗"), err)
		return
	}
	fmt.Printf("    %s PSReadLine updated; suggestions appear in new PowerShell windows\n", u.ok("✓"))
}

func applyTarget(t rcTarget, remove, dry bool) (string, error) {
	old, err := os.ReadFile(t.file)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	src := string(old)
	var next string
	if t.whole {
		if remove {
			if len(old) == 0 {
				return "absent", nil
			}
			if !dry {
				return "removed", os.Remove(t.file)
			}
			return "would remove", nil
		}
		next = t.body
	} else {
		next = stripBlock(src)
		if !remove {
			nl := "\n"
			if strings.Contains(src, "\r\n") {
				nl = "\r\n"
			}
			block := markStart + nl + strings.ReplaceAll(strings.TrimRight(t.body, "\n"), "\n", nl) + nl + markEnd + nl
			if next != "" && !strings.HasSuffix(next, "\n") {
				next += nl
			}
			if next != "" {
				next += nl
			}
			next += block
		}
	}
	if next == src {
		if remove {
			return "absent", nil
		}
		return "unchanged", nil
	}
	verb := "added"
	switch {
	case remove:
		verb = "removed"
	case strings.Contains(src, markStart) || (t.whole && len(old) > 0):
		verb = "updated"
	}
	if dry {
		return "would be " + verb, nil
	}
	if err := os.MkdirAll(filepath.Dir(t.file), 0o755); err != nil {
		return "", err
	}
	if len(old) > 0 {
		_ = os.WriteFile(t.file+".opal-backup", old, 0o644)
	}
	// Windows PowerShell 5.1 reads BOM-less scripts in the legacy code page.
	bom := string(rune(0xFEFF))
	if strings.HasSuffix(strings.ToLower(t.file), ".ps1") && !strings.HasPrefix(next, bom) &&
		strings.IndexFunc(next, func(r rune) bool { return r > 0x7e }) >= 0 {
		next = bom + next
	}
	return verb, os.WriteFile(t.file, []byte(next), 0o644)
}

// stripBlock removes a previous opal block (and the blank line before it).
func stripBlock(src string) string {
	return stripMarked(src, markStart, markEnd)
}

func stripMarked(src, start, stop string) string {
	i := strings.Index(src, start)
	if i < 0 {
		return src
	}
	j := strings.Index(src[i:], stop)
	if j < 0 {
		return src
	}
	end := i + j + len(stop)
	for end < len(src) && (src[end] == '\r' || src[end] == '\n') {
		end++
		if src[end-1] == '\n' {
			break
		}
	}
	before := strings.TrimRight(src[:i], "\r\n")
	after := src[end:]
	if before == "" {
		return after
	}
	if after == "" {
		return before + "\n"
	}
	return before + "\n\n" + after
}

// onPath reports whether `opal` on PATH is this binary.
func onPath() bool {
	found, err := exec.LookPath("opal")
	if err != nil {
		return false
	}
	a, errA := os.Stat(found)
	b, errB := os.Stat(binPath())
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// Startup lines call `opal` by name, and fall back to this binary's absolute
// path. The fallback matters right after install: editors and terminals that
// were already open keep their old PATH, and their new tabs inherit it.
func rcTargets() []rcTarget {
	home := platform.Home()
	var ts []rcTarget
	note := ""
	if !onPath() {
		note = "opal isn't on PATH yet, so startup falls back to " + binPath()
	}
	posix := func(shell string) string {
		abs := shQ(platform.ShellPath(binPath(), shell))
		return fmt.Sprintf(`if command -v opal >/dev/null 2>&1; then eval "$(opal init %[1]s)"
elif [ -x %[2]s ]; then eval "$(%[2]s init %[1]s)"; fi`, shell, abs)
	}

	if platform.HasCommand("zsh") {
		zdot := os.Getenv("ZDOTDIR")
		if zdot == "" {
			zdot = home
		}
		ts = append(ts, rcTarget{shell: "zsh", file: filepath.Join(zdot, ".zshrc"), body: posix("zsh"), note: note})
	}

	if bash := bashAvailable(); bash != "" {
		ts = append(ts, rcTarget{shell: bash, file: filepath.Join(home, ".bashrc"), body: posix("bash"), note: note})
		// macOS Terminal and Git Bash start login shells, which read
		// .bash_profile and never .bashrc. Bridge them.
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			if profileNeedsBridge(home) {
				ts = append(ts, rcTarget{
					shell: "bash (login)",
					file:  filepath.Join(home, ".bash_profile"),
					body:  `[ -f ~/.bashrc ] && . ~/.bashrc`,
				})
			}
		}
	}

	if platform.HasCommand("fish") {
		abs := fishQ(platform.ShellPath(binPath(), "fish"))
		body := fmt.Sprintf(`# Added by opal setup. Remove with: opal setup --remove
if status is-interactive
    if type -q opal
        opal init fish | source
    else if test -x %[1]s
        command %[1]s init fish | source
    end
end
`, abs)
		ts = append(ts, rcTarget{shell: "fish", file: filepath.Join(fishConfigDir(home), "conf.d", "opal.fish"), whole: true, body: body, note: note})
	}

	for _, ps := range []string{"pwsh", "powershell"} {
		if ps == "powershell" && runtime.GOOS != "windows" {
			continue
		}
		if !platform.HasCommand(ps) {
			continue
		}
		profile := psProfile(ps)
		if profile == "" {
			continue
		}
		name := "pwsh"
		if ps == "powershell" {
			name = "powershell 5.1"
		}
		abs := "'" + strings.ReplaceAll(binPath(), "'", "''") + "'"
		// The known path first: Test-Path is instant, Get-Command scans all of PATH.
		body := fmt.Sprintf(`$__opal = %[1]s
if (-not (Test-Path -LiteralPath $__opal)) { $__opal = Get-Command opal -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source }
if ($__opal) { Invoke-Expression ((& $__opal init pwsh) -join "`+"`"+`n") }
Remove-Variable __opal -ErrorAction SilentlyContinue`, abs)
		t := rcTarget{shell: name, file: profile, body: body, note: note}
		if ps == "powershell" {
			if pol := executionPolicy(); pol == "Restricted" || pol == "AllSigned" || pol == "Undefined" {
				t.note = "execution policy is " + pol + ", so profiles won't run. Fix it yourself with: Set-ExecutionPolicy -Scope CurrentUser RemoteSigned"
			}
		}
		ts = append(ts, t)
	}
	return ts
}

// bashAvailable returns a label for the usable bash, or "". On Windows,
// bash.exe in System32 is the WSL launcher, not a shell for this machine:
// only Git Bash / MSYS2 count.
func bashAvailable() string {
	if runtime.GOOS != "windows" {
		if platform.HasCommand("bash") {
			return "bash"
		}
		return ""
	}
	if os.Getenv("MSYSTEM") != "" {
		return "bash (Git Bash)"
	}
	if g, err := exec.LookPath("git"); err == nil {
		root := filepath.Dir(filepath.Dir(g)) // ...\Git\cmd\git.exe -> ...\Git
		if _, err := os.Stat(filepath.Join(root, "bin", "bash.exe")); err == nil {
			return "bash (Git Bash)"
		}
	}
	return ""
}

func profileNeedsBridge(home string) bool {
	b, err := os.ReadFile(filepath.Join(home, ".bash_profile"))
	if err != nil {
		// No .bash_profile: bash falls back to .profile, which may already bridge.
		p, err := os.ReadFile(filepath.Join(home, ".profile"))
		return err != nil || !strings.Contains(string(p), ".bashrc")
	}
	return !strings.Contains(string(b), ".bashrc")
}

func fishConfigDir(home string) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "fish")
	}
	return filepath.Join(home, ".config", "fish")
}

// psProfile asks PowerShell where its profile lives: it may be redirected
// into OneDrive, and 5.1 and 7 use different folders.
func psProfile(exe string) string {
	out, err := exec.Command(exe, "-NoProfile", "-NonInteractive", "-Command", "$PROFILE.CurrentUserCurrentHost").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func executionPolicy() string {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-ExecutionPolicy").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shQ(s string) string   { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func fishQ(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }
