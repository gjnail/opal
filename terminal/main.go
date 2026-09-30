// Opal Terminal: opal's own terminal app. On Windows it comes with Opal
// Bash (see tools/fetchshell).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"opal/terminal/internal/notify"
	"opal/terminal/internal/settings"
	"opal/terminal/internal/ui"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	profile := flag.String("profile", "", "start with this profile (a name from the new-tab list)")
	dir := flag.String("dir", "", "start in this directory")
	showVersion := flag.Bool("version", false, "print the version")
	listProfiles := flag.Bool("list-profiles", false, "list the shells Opal Terminal found")
	openSettings := flag.Bool("settings", false, "open the settings page")
	flag.Parse()

	if *showVersion {
		fmt.Println("opal-terminal", version)
		return
	}
	notify.SetProcessAppID()
	cfg := settings.Load()
	if *listProfiles {
		for _, p := range cfg.ProfilesWithDetected() {
			fmt.Printf("%-24s %s %s\n", p.Name, p.Command, strings.Join(p.Args, " "))
		}
		return
	}
	o := ui.Options{Version: version, Dir: *dir, Settings: *openSettings}
	if *profile != "" {
		var found bool
		for _, p := range cfg.ProfilesWithDetected() {
			if strings.EqualFold(p.Name, *profile) {
				p := p
				o.Profile, found = &p, true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "opal-terminal: no profile named %q (see -list-profiles)\n", *profile)
			os.Exit(2)
		}
	}
	// A command after the flags runs instead of a shell.
	if args := flag.Args(); len(args) > 0 {
		o.Profile = &settings.Profile{Name: args[0], Command: args[0], Args: args[1:]}
	}
	ui.Run(cfg, o)
}
