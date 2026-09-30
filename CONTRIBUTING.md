# Contributing

Bug reports, fixes, plugins, themes and testing on shells and systems we
haven't covered are all welcome.

## Rules for code that runs in the shell

opal's output is evaluated by your shell at startup and after every command,
so these apply to every change to the init scripts, the prompt and the
plugins:

1. Nothing that ends up in a prompt or an init script may be expanded by the
   shell a second time. The rendered prompt goes into a variable (`${_opal_ps}`
   in bash and zsh) for this reason, and values go through the quoting helpers
   in `internal/shells/quote.go`. A directory or branch named `$(rm -rf ~)`
   must show up as text.
2. The PowerShell init script must be pure ASCII. Windows PowerShell decodes
   native output with the console code page, so `psQuote` spells anything else
   as `[char]` codes. `go test ./internal/shells` checks this.
3. Keep startup and the prompt fast. `opal init` should stay well under 50 ms
   and `opal prompt` outside a git repository under 20 ms. Don't add a process
   spawn to the prompt path, and on Windows avoid `exec.LookPath` in init
   (`platform.LookPath` answers from one pass over PATH).
4. Respect what's already there. Don't replace PowerShell's built-in aliases
   unless the user opts in, don't override bindings other plugins set, and
   leave zsh-autosuggestions and other popular plugins in charge when they're
   loaded.
5. No network access, except `opal update`, `opal plugin install` and
   `update`, and the PSReadLine upgrade that `opal setup` asks about.
6. Never write to a user's startup files outside the marked
   `# >>> opal >>>` block, and keep a `.opal-backup` of anything setup changes.

## Building and testing

You need Go 1.22 or newer (<https://go.dev/dl/>).

```
go build -o opal .
go test ./...
go vet ./...
gofmt -l .
```

`OPAL_CONFIG_DIR` and `OPAL_DATA_DIR` point opal at another config folder and
another data folder (jump database, shared history, caches), so experiments
and tests don't touch your own. `OPAL_THEME`, `OPAL_ICONS` and `OPAL_COLOR`
override the config for one session.

### Real shells

The unit tests check the generated scripts as text. The smoke tests in `test/`
load opal into a real shell and poke at it. Build opal, put it on `PATH`, then
run the ones for the shells you have:

```
bash --norc -i test/smoke.bash
zsh -f -i test/smoke.zsh
fish --no-config -i test/smoke.fish
pwsh -NoProfile -File test/smoke.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File test/smoke.ps1
```

They use temporary data and history folders, so they don't touch yours. CI
runs them in bash 5, zsh, fish and PowerShell 7 on Linux, in bash 3.2, zsh
and PowerShell 7 on macOS, and in Windows PowerShell 5.1, PowerShell 7 and
Git Bash on Windows.

A lot of behavior depends on the shell version (bash 3.2 has no `PS0`,
PSReadLine 2.0 has no predictions), so say which versions you tested in your
pull request.

### Other platforms

Some files are behind build tags (`clip_windows.go`, `term_unix.go` and so
on). Check the targets you can't run:

```
GOOS=linux go vet ./...
GOOS=darwin go vet ./...
GOOS=windows go vet ./...
```

### Website and README images

The site in `site/` is plain HTML and CSS. The theme previews in it are
generated, not screenshots:

```
opal theme preview fire black boulder --svg > site/images/themes.svg
opal theme preview fire --html
```

The second command prints a `<pre class="term">` block to paste into a page.
Both use the default settings, so the output doesn't depend on your config.

## Source layout

```
main.go                 entry point
internal/cli/           commands: setup, init, prompt, doctor, theme, plugin, history, update...
internal/shells/        generates the init script for zsh, bash, fish and PowerShell
internal/prompt/        segments, layout, per-shell escaping, OSC 133/7 marks
internal/theme/         theme loading and color resolution
internal/plugin/        plugin loading and resolving aliases/functions per shell and OS
internal/config/        config.toml and the per-shell/per-OS variant keys
internal/complete/      tab completion for git and opal (used by all four shells)
internal/history/       the shared history file, importers and search ranking
internal/picker/        the full-screen Ctrl+R picker
internal/git/           repository status for the prompt, with a time budget and cache
internal/jump/          the frecency database behind `j`
internal/tools/         open, clipboard, extract, ports, pkg, js, venv
internal/platform/      OS, terminal and color detection, PATH lookups, paths
internal/greet/         the banner shown when a terminal opens
internal/ansi/          colors, gradients and character widths
internal/assets/        embedded plugins, themes, shell snippets and the default config
install/                install scripts for Windows and for macOS/Linux
test/                   smoke tests for each shell
site/                   the website (GitHub Pages)
terminal/               Opal Terminal, a separate module (work in progress, Go 1.26)
```

Releases are built by GitHub Actions; [docs/releasing.md](docs/releasing.md)
has the steps.

## Pull requests

- Keep each pull request to one fix or feature.
- Add tests for new logic. For anything that generates shell code, a test that
  checks the output, and ideally a line in the smoke tests.
- Follow the style of the surrounding code. Comments explain why, in plain
  words.
- Run `go test ./...`, `go vet ./...` and `gofmt -l .` before pushing.
- Add user-visible changes to `CHANGELOG.md` under "Unreleased".
- For a new plugin, say which shells and systems you tried it on.

## Reporting bugs

Include your OS, shell and version, terminal, and the output of `opal doctor`.
For prompt or startup problems, the relevant part of `opal init <shell>` helps.
Check that anything you paste doesn't show paths, hostnames or commands you'd
rather keep private.

Report anything that could make a shell run unintended commands privately, as
described in [SECURITY.md](SECURITY.md).

## License

Contributions are licensed under the [MIT License](LICENSE), like the rest of
the project.
