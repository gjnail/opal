# Security

opal runs inside every shell you start: its init script is evaluated by your
shell, and its prompt runs after every command. It also downloads plugins and
updates when you ask it to, and keeps a history of the commands you run. Bugs
in those areas can run code you didn't intend or expose private data, so
they're treated as security issues.

## What to report privately

- Anything that makes a shell execute text it shouldn't, for example a
  directory name, git branch name or config value that gets run as a command
  by the prompt or the init script.
- Problems with `opal update`: installing a binary that doesn't match the
  release's `SHA256SUMS.txt`, or replacing the binary in an unsafe way.
- Problems with `opal plugin install`: installing somewhere other than
  `~/.config/opal/plugins`, or running anything during the install.
- Anything that writes the shared history (`history.tsv` in opal's data
  folder) somewhere other people can read it, or records commands that start
  with a space.
- Anything that sends data off the machine. opal only uses the network for
  `opal update`, `opal plugin install` / `update`, and on Windows the optional
  PSReadLine upgrade during setup.

Crashes, wrong prompts and completion bugs can go in public issues.

## How to report

Use GitHub's private vulnerability reporting: open the **Security** tab of the
repository and choose **Report a vulnerability**. Include your OS, shell and
version, the opal version, and steps to reproduce. Please don't open a public
issue until a fix is released.

Expect a reply within a week. Fixes ship in the next release, and the release
notes credit the reporter unless they'd rather not be named.

## Supported versions

Only the latest release receives fixes.
