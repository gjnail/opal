// Command opal is a prompt, plugin system and set of shell tools for zsh,
// bash, fish and PowerShell.
package main

import (
	"os"

	"opal/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
