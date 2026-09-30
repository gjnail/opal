# Changelog

Notable changes to opal. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

### Added

- Opal Terminal, opal's own terminal app
  ([terminal/](terminal/README.md)): tabs and split panes, inline images,
  quick select, a command palette over the shared history, desktop
  notifications, session restore and a settings page. Release archives for
  Windows, macOS and Linux on x86_64 and ARM64. So far it has only been used
  on Windows.
- Opal Bash: on Windows, Opal Terminal comes with its own bash. It's
  MSYS2's bash with coreutils, grep, sed, gawk, findutils, diffutils, less,
  tar, gzip and which. It starts with its own startup file and history
  instead of `~/.bashrc`, so it's separate from your other shells, and new
  tabs open it unless `shell` names another. Each release carries the source
  of its packages.
- The install scripts install Opal Terminal too: with a Start menu shortcut
  on Windows, in `~/Applications` on macOS, and with a menu entry on Linux
  desktops. `OPAL_NO_TERMINAL=1` leaves it out.
- `opal terminal` opens Opal Terminal in the current folder.

## [0.1.0] - 2026-09-29

First public version.

### Shells and systems

- zsh, bash (3.2 through 5.x), fish, Windows PowerShell 5.1 and PowerShell 7,
  on Windows (including Git Bash), macOS and Linux (including WSL).
- `opal setup` adds a marked block to each installed shell's startup file,
  finds PowerShell profiles that OneDrive has moved, and `opal setup --remove`
  takes it out again.

### Prompt

- Three themes: fire (two lines, gradient path), black (colored blocks) and
  boulder (one line). The prompt symbol's color depends on the shell.
- Git branch, ahead/behind, staged, modified, untracked, conflicts, stash, and
  rebase/merge state. `git status` gets a time budget (200 ms by default);
  when it's slower, the prompt shows the last known counts and refreshes them
  in the background.
- Command duration, exit status with signal names, background jobs, Python
  virtualenv, and user@host over SSH, in containers or as root/administrator.
- Transient prompt in zsh, fish and PowerShell: finished prompts are redrawn
  as just the prompt symbol.
- OSC 133 command marks and OSC 7 working-directory reports (and OSC 9;9 for
  Windows Terminal).

### Shell features

- Grey as-you-type suggestions: opal's own engine in zsh, ble.sh in bash when
  it's installed, PSReadLine 2.1+ in PowerShell, fish's own. Tab or Right
  arrow accepts one.
- Tab completion for git (branches, remotes, changed files, stash entries)
  and for the git aliases in PowerShell, and for opal's own commands in every
  shell. Completion scripts that installed tools can generate (docker,
  kubectl, gh, helm, uv, deno, pnpm, just, winget) are cached and loaded on
  first use.
- One command history shared by every shell, with a full-screen fuzzy search
  on Ctrl+R that can narrow to the current directory or shell. Setup imports
  the existing PowerShell, bash, zsh and fish histories.

### Plugins

- Plugins are TOML files whose aliases and functions opal translates into
  each shell's syntax, with per-shell and per-OS variants.
- Built in: core, git, jump, python, node, docker, kubectl and pkg.
- `opal plugin install <user/repo>` installs plugins from git;
  `opal plugin update` pulls them.

### Tools

- `opal open`, `opal clip`, `opal extract`, `opal ports`, `opal path`,
  `opal pkg` (brew, apt, dnf, pacman, zypper, apk, winget, scoop, choco) and
  `opal js` (npm, pnpm, yarn, bun), with the same behavior on every OS.
- `opal doctor` shows what opal detected and which features are active.
- `opal update` rebuilds from a source checkout or downloads the latest
  release after checking it against `SHA256SUMS.txt`.

[Unreleased]: https://github.com/gjnail/opal/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gjnail/opal/releases/tag/v0.1.0
