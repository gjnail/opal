package ui

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	localeOnce sync.Once
	localeName string
)

// defaultLocale is the LANG to give shells when we have none ourselves.
// macOS starts apps opened from Finder, the Dock or `open` without one, and
// a shell in the C locale shows non-ASCII file names as question marks and
// can't edit them on the command line. Terminal.app and iTerm2 fill it in
// from the system's language and region the same way.
func defaultLocale() string {
	localeOnce.Do(func() {
		out, _ := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLocale").Output()
		localeName = utf8Locale(strings.TrimSpace(string(out)), localeInstalled)
	})
	return localeName
}

func localeInstalled(name string) bool {
	st, err := os.Stat("/usr/share/locale/" + name + "/LC_CTYPE")
	return err == nil && !st.IsDir()
}
