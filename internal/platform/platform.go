// Package platform detects the OS, terminal and environment opal is running
// in, so everything else can adapt instead of assuming.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Info describes the machine and session.
type Info struct {
	OS        string // "macos", "linux", "windows" (or GOOS for anything else)
	Arch      string
	WSL       bool
	WSLDistro string
	Termux    bool
	Container string // "docker", "podman", ... or ""
	SSH       bool
	MSYS      string // MSYSTEM when under Git Bash / MSYS2, e.g. "MINGW64"
	Terminal  string
	Home      string
	User      string
	Host      string
	Root      bool // uid 0, or an elevated Windows session
}

var (
	infoOnce sync.Once
	info     Info
)

// Detect returns cached information about the current environment.
func Detect() Info {
	infoOnce.Do(func() { info = detect() })
	return info
}

func detect() Info {
	in := Info{OS: OSName(runtime.GOOS), Arch: runtime.GOARCH}
	in.Home = Home()
	in.User = firstEnv("USER", "USERNAME", "LOGNAME")
	in.Host, _ = os.Hostname()
	if i := strings.IndexByte(in.Host, '.'); i > 0 {
		in.Host = in.Host[:i]
	}
	if runtime.GOOS == "linux" {
		if d := os.Getenv("WSL_DISTRO_NAME"); d != "" {
			in.WSL, in.WSLDistro = true, d
		} else if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil &&
			strings.Contains(strings.ToLower(string(b)), "microsoft") {
			in.WSL = true
		}
		in.Termux = os.Getenv("TERMUX_VERSION") != "" ||
			strings.HasPrefix(os.Getenv("PREFIX"), "/data/data/com.termux")
		switch {
		case exists("/.dockerenv"):
			in.Container = "docker"
		case exists("/run/.containerenv"):
			in.Container = "podman"
		case os.Getenv("container") != "":
			in.Container = os.Getenv("container")
		}
	}
	if runtime.GOOS == "windows" {
		in.MSYS = os.Getenv("MSYSTEM")
		in.Root = os.Getenv("OPAL_ADMIN") == "1"
	} else {
		in.Root = os.Geteuid() == 0
	}
	in.SSH = os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != ""
	in.Terminal = terminalName()
	return in
}

// OSName maps GOOS to the names used in configs.
func OSName(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

func terminalName() string {
	if os.Getenv("WT_SESSION") != "" {
		return "Windows Terminal"
	}
	switch tp := os.Getenv("TERM_PROGRAM"); tp {
	case "":
	case "Apple_Terminal":
		return "Terminal.app"
	case "iTerm.app":
		return "iTerm2"
	case "OpalTerminal":
		return "Opal Terminal"
	case "vscode":
		return "VS Code"
	default:
		return tp
	}
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "":
		return "kitty"
	case os.Getenv("ALACRITTY_WINDOW_ID") != "" || os.Getenv("ALACRITTY_SOCKET") != "":
		return "Alacritty"
	case os.Getenv("KONSOLE_VERSION") != "":
		return "Konsole"
	case os.Getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm":
		return "JetBrains"
	case os.Getenv("VTE_VERSION") != "":
		return "VTE terminal"
	case os.Getenv("ConEmuPID") != "":
		return "ConEmu"
	case runtime.GOOS == "windows":
		return "Console Host"
	}
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return "unknown"
}

// TerminalID names the terminal (window, tab or pane) this process runs in,
// so a shell started inside another shell can tell it isn't a new terminal.
func TerminalID() string {
	if id := ttyID(); id != "" {
		return id
	}
	for _, k := range []string{"WT_SESSION", "TERM_SESSION_ID", "TMUX_PANE", "KITTY_WINDOW_ID", "WEZTERM_PANE", "WINDOWID"} {
		if v := os.Getenv(k); v != "" {
			return k + ":" + v
		}
	}
	return "1" // unknown: the first shell counts as new, the ones nested in it don't
}

// ColorDepth returns 0 (none), 16, 256 or 24 (truecolor). pref is the
// config value: "auto", "none", "16", "256" or "truecolor".
func ColorDepth(pref string) int {
	switch pref {
	case "none":
		return 0
	case "16":
		return 16
	case "256":
		return 256
	case "truecolor", "24bit":
		return 24
	}
	if os.Getenv("NO_COLOR") != "" {
		return 0
	}
	term := os.Getenv("TERM")
	if term == "dumb" {
		return 0
	}
	if ct := strings.ToLower(os.Getenv("COLORTERM")); ct == "truecolor" || ct == "24bit" {
		return 24
	}
	if os.Getenv("WT_SESSION") != "" {
		return 24
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "Hyper", "mintty", "ghostty", "Tabby", "rio", "WarpTerminal", "OpalTerminal":
		return 24
	case "Apple_Terminal":
		return 256
	}
	for _, k := range []string{"kitty", "alacritty", "foot", "ghostty", "wezterm", "direct"} {
		if strings.Contains(term, k) {
			return 24
		}
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("KONSOLE_VERSION") != "" {
		return 24
	}
	if v, err := strconv.Atoi(os.Getenv("VTE_VERSION")); err == nil && v >= 3600 {
		return 24
	}
	if runtime.GOOS == "windows" {
		return 24 // conhost has spoken truecolor since Windows 10 1703
	}
	if strings.Contains(term, "256") {
		return 256
	}
	return 16
}

// IconSet resolves the "icons" config value. Terminals that bundle Nerd Font
// symbols get them automatically; everyone else gets plain Unicode.
func IconSet(pref string) string {
	switch pref {
	case "nerd", "unicode", "ascii":
		return pref
	}
	if os.Getenv("OPAL_NERD_FONT") == "1" {
		return "nerd"
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "WezTerm", "ghostty", "OpalTerminal":
		return "nerd"
	}
	if os.Getenv("TERM") == "linux" { // the bare Linux VT has almost no glyphs
		return "ascii"
	}
	return "unicode"
}

// ConfigDir is ~/.config/opal on every OS (overridable), so dotfile repos
// work the same everywhere.
func ConfigDir() string {
	if d := os.Getenv("OPAL_CONFIG_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opal")
	}
	return filepath.Join(Home(), ".config", "opal")
}

// Home is the user's home directory. It never returns "" (which would turn
// every path relative to wherever opal happens to run): with no home at all
// (e.g. under `env -i`), it falls back to a temp directory.
func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return FromMSYS(h)
	}
	if d, p := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); d != "" && p != "" {
		return d + p
	}
	return filepath.Join(os.TempDir(), "opal-home")
}

// DataDir holds state such as the jump database.
func DataDir() string {
	if d := os.Getenv("OPAL_DATA_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			return filepath.Join(l, "opal")
		}
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "opal")
	}
	return filepath.Join(Home(), ".local", "share", "opal")
}

// ShellPath converts a native path into the form a shell expects. On Windows,
// Git Bash / MSYS2 want /c/Users/... while PowerShell wants C:\Users\...
func ShellPath(p, shell string) string {
	if runtime.GOOS != "windows" || shell == "pwsh" || shell == "" {
		return p
	}
	if os.Getenv("MSYSTEM") == "" {
		return filepath.ToSlash(p)
	}
	return ToMSYS(p)
}

// ToMSYS turns C:\x\y into /c/x/y.
func ToMSYS(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + filepath.ToSlash(p[2:])
	}
	return filepath.ToSlash(p)
}

// FromMSYS turns /c/x/y into C:\x\y (other paths pass through).
func FromMSYS(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if len(p) >= 2 && p[0] == '/' && isLetter(p[1]) && (len(p) == 2 || p[2] == '/') {
		rest := p[2:]
		if rest == "" {
			rest = "/"
		}
		return strings.ToUpper(p[1:2]) + ":" + filepath.FromSlash(rest)
	}
	return p
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
