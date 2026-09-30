# Releasing opal

Releases are built by [`.github/workflows/release.yml`](../.github/workflows/release.yml),
and Opal Terminal's archives by
[`.github/workflows/terminal-release.yml`](../.github/workflows/terminal-release.yml),
which adds them to the same draft. Nothing is published until you publish the
draft release.

## Try the build first

Actions > Release > Run workflow builds all the archives without making a
release. Use it after changing the workflow. The archives are attached to the
run. Actions > Terminal release > Run workflow does the same for Opal
Terminal.

## Make a release

1. In `CHANGELOG.md`, move everything under `## [Unreleased]` into a new
   `## [x.y.z] - YYYY-MM-DD` section, and update the comparison links at the
   bottom. That section becomes the release notes.
2. Commit, then tag and push:

   ```
   git tag vx.y.z
   git push origin main vx.y.z
   ```

3. The workflow checks that the changelog has a section for the version, runs
   the tests, builds for Windows, macOS and Linux (x86_64 and ARM64), smoke
   tests the Linux build, and opens a draft release with the archives,
   `SHA256SUMS.txt` and a build provenance attestation for each file. The
   version comes from the tag; there is no version number in the source.
   The Terminal release workflow builds Opal Terminal on Windows, macOS and
   Linux runners at the same time, waits for that draft, uploads its archives
   and `opal-terminal_SHA256SUMS.txt` (with attestations), and adds an Opal
   Terminal section to the notes. It also uploads
   `opal-terminal_shell-sources.tar`, the MSYS2 source packages of Opal Bash,
   which the Windows archives include.
4. Wait for both workflows. Look over the draft on the Releases page, try the
   archives for your platform, then click **Publish release**.

If something is wrong before publishing, delete the draft and the tag
(`git push origin :vx.y.z`), fix it, and tag again.

## Archive names

The archives are always named `opal_<os>_<arch>.tar.gz` (or `.zip` on
Windows), without the version, because the install scripts and `opal update`
download `releases/latest/download/<name>`. Don't rename them, and keep
`SHA256SUMS.txt`: `opal update` refuses a download that doesn't match it.
Opal Terminal's follow the same rule: `opal-terminal_<os>_<arch>.tar.gz` (or
`.zip`), checked against `opal-terminal_SHA256SUMS.txt`.

## Updating Opal Bash

Opal Bash's MSYS2 packages are pinned in
`terminal/packaging/shell/packages.lock`, so releases don't pick up new
versions by themselves. To update them, run
`go run ./tools/fetchshell -update` in `terminal/`, then
`go run ./tools/fetchshell` and try the result in Opal Terminal, and commit
the lock file. If a package's license changes, add the new license text to
`terminal/packaging/shell/licenses`.

## Licenses

Every archive includes `THIRD-PARTY-LICENSES.txt`, the licenses of the Go
modules opal is built with. The workflow collects them from the module cache
and fails if a module has no license file. Opal Terminal's archives get
theirs from `terminal/scripts/package.sh`, which adds the bundled font's
license, and the Windows ones also carry ConPTY's. Opal Bash, in the Windows
archives, is GPL software from MSYS2: its `shell/PACKAGES.txt` lists each
package's license and source, `shell/LICENSES` has the license texts, and the
release's `opal-terminal_shell-sources.tar` is the source. To see the file
locally:

```
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' . | sort -u
```
