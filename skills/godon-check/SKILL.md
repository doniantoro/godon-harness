---
name: godon-check
description: Run static checks — gofmt, go vet, mock freshness and golangci-lint — with one line per check and the detail in files. Use instead of `make vet`, `make lint`, `go vet` or regenerating mocks to see a diff, when a change is finished.
---

# Static checks with godon

```bash
godon check            # gofmt, go vet, mocks, golangci-lint (when installed)
godon check --no-lint  # skip the slowest check
```

Calling `godon`: on Claude Code it is on PATH; otherwise run
`<plugin root>/bin/godon` (Windows PowerShell: `<plugin root>\bin\godon.ps1`),
two directories above this file.

Output is one line per check — `OK`, `FAILED`, `STALE` or `SKIPPED` — and a
check that did not pass names its detail file. Open only those files.

- Nothing in the working tree is modified. `mocks STALE` means a
  `//go:generate mockgen` output no longer matches its interface: regenerate
  with `go generate` on the named directive.
- `gofmt FAILED` lists files; the format hook formats files as they are
  edited, so this usually means files nobody has touched yet.
- Run it once when a change is finished, alongside `godon test --with-all`,
  not after every edit.

## Hooks that run on their own

While the plugin is enabled, two hooks need no call:

- **After every edit** the edited file is formatted (goimports when
  installed, otherwise gofmt). Do not format by hand, and re-read a file
  before editing it again if you wrote it earlier in the turn. A file that no
  longer parses is reported back immediately.
- **Before the turn ends**, if Go files changed since the last passing gate,
  `go vet` runs on the changed packages. A finding blocks finishing until it
  is fixed.
