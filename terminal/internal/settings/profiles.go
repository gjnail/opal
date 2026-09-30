package settings

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Profiles returns the configured profiles followed by the shells found on
// this machine, without duplicates.
func (c *Config) ProfilesWithDetected() []Profile {
	out := append([]Profile(nil), c.Profiles...)
	seen := map[string]bool{}
	for _, p := range out {
		seen[strings.ToLower(p.Name)] = true
	}
	for _, p := range DetectProfiles() {
		if !seen[strings.ToLower(p.Name)] {
			out = append(out, p)
			seen[strings.ToLower(p.Name)] = true
		}
	}
	return out
}

// DefaultProfile picks what a new tab runs: the configured shell (a
// profile name or a command line), else the platform's usual shell.
func (c *Config) DefaultProfile() Profile {
	all := c.ProfilesWithDetected()
	if c.Shell != "" {
		for _, p := range all {
			if strings.EqualFold(p.Name, c.Shell) {
				return p
			}
		}
		fields := splitCommand(c.Shell)
		if len(fields) > 0 {
			return Profile{Name: filepath.Base(fields[0]), Command: fields[0], Args: fields[1:]}
		}
	}
	if len(all) > 0 {
		return all[0]
	}
	if runtime.GOOS == "windows" {
		return Profile{Name: "Command Prompt", Command: "cmd.exe"}
	}
	return Profile{Name: "sh", Command: "/bin/sh"}
}

// DetectProfiles finds installed shells, most preferred first.
func DetectProfiles() []Profile {
	if runtime.GOOS == "windows" {
		return windowsProfiles()
	}
	return unixProfiles()
}

// OpalBashName names the bash that comes with Opal Terminal on Windows.
const OpalBashName = "Opal Bash"

// bundledBash is the bash in the shell folder next to opal-terminal.exe
// (see tools/fetchshell). It starts with Opal's own startup file instead of
// ~/.bash_profile and ~/.bashrc, so it stays separate from Git Bash and any
// other bash on the machine.
func bundledBash(exeDir string) (Profile, bool) {
	bash := filepath.Join(exeDir, "shell", "usr", "bin", "bash.exe")
	if !fileExists(bash) {
		return Profile{}, false
	}
	return Profile{Name: OpalBashName, Command: bash, Args: []string{"--noprofile", "--rcfile", "/etc/opal/bashrc", "-i"}}, true
}

func windowsProfiles() []Profile {
	var out []Profile
	// First, so new tabs open it unless the config names another shell.
	if exe, err := os.Executable(); err == nil {
		if p, ok := bundledBash(filepath.Dir(exe)); ok {
			out = append(out, p)
		}
	}
	add := func(name, path string, args ...string) {
		if path == "" {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		out = append(out, Profile{Name: name, Command: path, Args: args})
	}
	pwsh, _ := exec.LookPath("pwsh.exe")
	if pwsh == "" {
		pwsh = filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe")
	}
	add("PowerShell", pwsh, "-NoLogo")
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	add("Windows PowerShell", filepath.Join(sys, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo")
	cmd := os.Getenv("ComSpec")
	if cmd == "" {
		cmd = filepath.Join(sys, "System32", "cmd.exe")
	}
	add("Command Prompt", cmd)
	for _, dir := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, "Git", "bin", "bash.exe"); fileExists(p) {
			add("Git Bash", p, "--login", "-i")
			break
		}
	}
	out = append(out, wslProfiles(sys)...)
	return out
}

// wslProfiles lists WSL distributions. wsl.exe prints UTF-16.
func wslProfiles(sys string) []Profile {
	wsl := filepath.Join(sys, "System32", "wsl.exe")
	if !fileExists(wsl) {
		return nil
	}
	b, err := exec.Command(wsl, "--list", "--quiet").Output()
	if err != nil || len(b) < 2 {
		return nil
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	var out []Profile
	for _, line := range strings.Split(string(utf16.Decode(u)), "\n") {
		name := strings.TrimSpace(strings.Trim(line, "\ufeff\x00\r"))
		if name == "" || strings.HasPrefix(name, "docker-desktop") {
			continue
		}
		out = append(out, Profile{Name: name + " (WSL)", Command: wsl, Args: []string{"-d", name, "--cd", "~"}})
	}
	return out
}

func unixProfiles() []Profile {
	var out []Profile
	seen := map[string]bool{}
	add := func(path string, login bool) {
		if path == "" || seen[path] || !fileExists(path) {
			return
		}
		seen[path] = true
		out = append(out, Profile{Name: filepath.Base(path), Command: path, Login: login})
	}
	// macOS terminals start login shells; Linux ones usually don't.
	login := runtime.GOOS == "darwin"
	add(os.Getenv("SHELL"), login)
	if f, err := os.Open("/etc/shells"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			switch filepath.Base(line) {
			case "zsh", "bash", "fish", "nu", "pwsh", "elvish", "xonsh":
				add(line, login)
			}
		}
	}
	if len(out) == 0 {
		add("/bin/sh", false)
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// splitCommand splits a command line on spaces, honoring double quotes.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
