# Releasing opal

Releases are built by [`.github/workflows/release.yml`](../.github/workflows/release.yml).
Nothing is published until you publish the draft release it creates.

## Try the build first

Actions > Release > Run workflow builds all the archives without making a
release. Use it after changing the workflow. The archives are attached to the
run.

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
4. Look over the draft on the Releases page, try the archive for your
   platform, then click **Publish release**.

If something is wrong before publishing, delete the draft and the tag
(`git push origin :vx.y.z`), fix it, and tag again.

## Archive names

The archives are always named `opal_<os>_<arch>.tar.gz` (or `.zip` on
Windows), without the version, because the install scripts and `opal update`
download `releases/latest/download/<name>`. Don't rename them, and keep
`SHA256SUMS.txt`: `opal update` refuses a download that doesn't match it.

## Licenses

Every archive includes `THIRD-PARTY-LICENSES.txt`, the licenses of the Go
modules opal is built with. The workflow collects them from the module cache
and fails if a module has no license file. To see the file locally:

```
go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' . | sort -u
```
