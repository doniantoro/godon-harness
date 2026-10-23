---
name: godon-test
description: Run a repository's tests through godon and read only the failures. Use instead of `go test` whenever running or re-running tests — the test being worked on, the whole unit suite after a large change, or the integration suite.
---

# Running tests with godon

`godon test` runs the tests, prints a few summary lines, and writes the detail
to files. Never run raw `go test` (and never add `-v`) to see more — read the
files godon names.

## Calling godon

- **Claude Code:** `godon` is on PATH while the plugin is enabled.
- **Codex, or `godon` not found:** run the launcher two directories above this
  file: `<plugin root>/bin/godon` (Windows PowerShell:
  `powershell -File <plugin root>\bin\godon.ps1`).

The first call may take a few seconds while the binary is fetched and cached.

## Commands

```bash
godon test ./path/to/pkg -run TestName        # the test being worked on
godon test ./path/to/pkg                      # every test in that package
godon test ./path/to/pkg -focus 'spec text'   # Ginkgo spec(s); -label 'expr' for labels
godon test ./path/to/pkg -run TestName --with-all   # then every unit test, only if it passes
godon test --all                              # every unit test
godon test --integration [pkgs] [-label expr] # integration-tagged suite
```

## When to run what

- A question, a discussion or a documentation-only change: run nothing.
- While iterating: only the test being worked on.
- A change inside one package's internals: that package.
- A change that touches more than one package, or code other packages import,
  or that is about to be committed: `--with-all` (or `--all`) **once**, when
  the change is finished — not after every edit.
- The integration suite can take many minutes: **start it in the background**
  and carry on until it reports.

The repository's own instructions (CLAUDE.md, AGENTS.md) may tighten these
rules; theirs win.

## Reading the result — stop as soon as you understand the failure

1. **The printed summary**: one line per failure — category, package, test,
   and the path of its extract.
2. **`failures/NN-<category>-<pkg>.txt`**: only the lines that explain that
   failure (assertion diff, race report, panic head, goleak stack, build error).
3. **`full.log`**: only when an extract is not enough. Search it; never read
   it whole.

Categories: `build`, `assertion`, `mock`, `panic`, `timeout`, `race`, `leak`,
`setup`, `unclassified`.

Exit codes: 0 passed, 1 failed, 2 usage, **3 the `-run`/`-focus`/`-label`
filter matched nothing — nothing was verified; fix the filter, never treat it
as a pass**, 4 tool error.
