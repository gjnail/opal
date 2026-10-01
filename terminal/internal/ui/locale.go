package ui

import "strings"

// hasLocale reports whether env already chooses a character set: LC_ALL,
// LC_CTYPE or LANG, in the order the C library reads them.
func hasLocale(env []string) bool {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if v != "" && (k == "LC_ALL" || k == "LC_CTYPE" || k == "LANG") {
			return true
		}
	}
	return false
}

// utf8Locale turns a macOS locale identifier ("en_US", "zh-Hans_CN",
// "en_GB@rg=uszzzz") into the UTF-8 locale to put in LANG. installed
// reports whether the system has a locale; one it doesn't have (en_DE, say)
// becomes en_US.UTF-8, so the shell still gets UTF-8.
func utf8Locale(id string, installed func(string) bool) string {
	id, _, _ = strings.Cut(id, "@")
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' })
	for _, p := range parts {
		if strings.IndexFunc(p, func(r rune) bool { return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') }) >= 0 {
			parts = nil
			break
		}
	}
	if len(parts) >= 2 {
		// The parts between are a script (Hans, Latn), which POSIX locale
		// names leave out.
		region := parts[len(parts)-1]
		if len(region) == 2 {
			if l := strings.ToLower(parts[0]) + "_" + strings.ToUpper(region) + ".UTF-8"; installed(l) {
				return l
			}
		}
	}
	if installed("en_US.UTF-8") {
		return "en_US.UTF-8"
	}
	return ""
}
