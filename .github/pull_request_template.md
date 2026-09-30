## What this changes

<!-- One or two sentences. Link the issue if there is one. -->

## How I tested it

<!-- Which OS, shells and shell versions you ran it in, and what you checked. -->

## Checklist

- [ ] `go test ./...`, `go vet ./...` and `gofmt -l .` pass
- [ ] Smoke tests pass in the shells I changed (`test/smoke.*`), or CI is green
- [ ] Nothing the shell evaluates can expand user-controlled text (paths, branch names, config values)
- [ ] The PowerShell init stays pure ASCII
- [ ] `CHANGELOG.md` updated for user-visible changes
