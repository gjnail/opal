package tools

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"opal/internal/platform"
)

// Verbs are the package operations every manager gets.
var Verbs = []string{"install", "remove", "search", "update", "upgrade", "list", "info"}

// manager maps verbs to argv templates. "sudo" as the first word is added
// only when needed (not root, and sudo/doas exists).
type manager struct {
	name string
	cmds map[string][]string
}

var managers = []manager{
	{"brew", map[string][]string{
		"install": {"brew", "install"}, "remove": {"brew", "uninstall"}, "search": {"brew", "search"},
		"update": {"brew", "update"}, "upgrade": {"brew", "upgrade"}, "list": {"brew", "list"}, "info": {"brew", "info"}}},
	{"apt", map[string][]string{
		"install": {"sudo", "apt", "install"}, "remove": {"sudo", "apt", "remove"}, "search": {"apt", "search"},
		"update": {"sudo", "apt", "update"}, "upgrade": {"sudo", "apt", "upgrade"}, "list": {"apt", "list", "--installed"}, "info": {"apt", "show"}}},
	{"dnf", map[string][]string{
		"install": {"sudo", "dnf", "install"}, "remove": {"sudo", "dnf", "remove"}, "search": {"dnf", "search"},
		"update": {"sudo", "dnf", "makecache"}, "upgrade": {"sudo", "dnf", "upgrade"}, "list": {"dnf", "list", "--installed"}, "info": {"dnf", "info"}}},
	{"pacman", map[string][]string{
		"install": {"sudo", "pacman", "-S"}, "remove": {"sudo", "pacman", "-Rs"}, "search": {"pacman", "-Ss"},
		"update": {"sudo", "pacman", "-Sy"}, "upgrade": {"sudo", "pacman", "-Syu"}, "list": {"pacman", "-Q"}, "info": {"pacman", "-Si"}}},
	{"zypper", map[string][]string{
		"install": {"sudo", "zypper", "install"}, "remove": {"sudo", "zypper", "remove"}, "search": {"zypper", "search"},
		"update": {"sudo", "zypper", "refresh"}, "upgrade": {"sudo", "zypper", "update"}, "list": {"zypper", "search", "--installed-only"}, "info": {"zypper", "info"}}},
	{"apk", map[string][]string{
		"install": {"sudo", "apk", "add"}, "remove": {"sudo", "apk", "del"}, "search": {"apk", "search"},
		"update": {"sudo", "apk", "update"}, "upgrade": {"sudo", "apk", "upgrade"}, "list": {"apk", "info"}, "info": {"apk", "info", "-a"}}},
	{"xbps-install", map[string][]string{
		"install": {"sudo", "xbps-install", "-S"}, "remove": {"sudo", "xbps-remove", "-R"}, "search": {"xbps-query", "-Rs"},
		"update": {"sudo", "xbps-install", "-S"}, "upgrade": {"sudo", "xbps-install", "-Su"}, "list": {"xbps-query", "-l"}, "info": {"xbps-query", "-R"}}},
	{"winget", map[string][]string{
		"install": {"winget", "install"}, "remove": {"winget", "uninstall"}, "search": {"winget", "search"},
		"update": {"winget", "source", "update"}, "upgrade": {"winget", "upgrade", "--all"}, "list": {"winget", "list"}, "info": {"winget", "show"}}},
	{"scoop", map[string][]string{
		"install": {"scoop", "install"}, "remove": {"scoop", "uninstall"}, "search": {"scoop", "search"},
		"update": {"scoop", "update"}, "upgrade": {"scoop", "update", "*"}, "list": {"scoop", "list"}, "info": {"scoop", "info"}}},
	{"choco", map[string][]string{
		"install": {"choco", "install"}, "remove": {"choco", "uninstall"}, "search": {"choco", "search"},
		"update": {"choco", "outdated"}, "upgrade": {"choco", "upgrade", "all"}, "list": {"choco", "list"}, "info": {"choco", "info"}}},
}

// preference order per OS
func order() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"brew"}
	case "windows":
		return []string{"winget", "scoop", "choco"}
	}
	return []string{"apt", "dnf", "pacman", "zypper", "apk", "xbps-install", "brew"}
}

// PackageManagers returns the managers available here, preferred first.
func PackageManagers(prefer string) []string {
	var out []string
	if prefer != "" && platform.HasCommand(prefer) {
		out = append(out, prefer)
	}
	for _, m := range order() {
		if m != prefer && platform.HasCommand(m) {
			out = append(out, m)
		}
	}
	return out
}

// PkgCommand builds the argv for verb on the preferred manager.
func PkgCommand(prefer, verb string, pkgs []string) ([]string, string, error) {
	avail := PackageManagers(prefer)
	if len(avail) == 0 {
		return nil, "", fmt.Errorf("no supported package manager found")
	}
	name := avail[0]
	var m manager
	for _, x := range managers {
		if x.name == name {
			m = x
		}
	}
	tmpl, ok := m.cmds[verb]
	if !ok {
		return nil, name, fmt.Errorf("unknown verb %q (use: %s)", verb, strings.Join(Verbs, ", "))
	}
	argv := append([]string(nil), tmpl...)
	if argv[0] == "sudo" {
		switch {
		case platform.Detect().Root || platform.Detect().Termux:
			argv = argv[1:]
		case platform.HasCommand("sudo"):
		case platform.HasCommand("doas"):
			argv[0] = "doas"
		default:
			argv = argv[1:]
		}
	}
	return append(argv, pkgs...), name, nil
}

// Exec runs argv with the terminal attached and returns its exit code.
func Exec(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "opal:", err)
		return 127
	}
	return 0
}
