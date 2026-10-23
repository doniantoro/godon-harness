# godon-harness

A harness for AI coding agents — **Claude Code** and **Codex** — that keeps raw
tool output out of the agent's context. Instead of `go test -v` scrolling
thousands of lines into the conversation, the agent runs `godon`, reads a
few summary lines, and opens only the file that explains the failure.

Go is supported today. The design is per-language drivers, so other languages
can be added without changing the commands, hooks or skills.

| | What the agent gets |
|---|---|
| `godon test` | One line per failure, classified (`build`, `assertion`, `mock`, `panic`, `timeout`, `race`, `leak`, `setup`), and one small file per failure. A filter that matched nothing is reported as such, never as a pass |
| `godon symbol` | Where an identifier is declared — functions, methods, types, interface methods, struct fields, consts, vars — one line each, with its owner |
| `godon check` | gofmt, go vet, mock freshness and golangci-lint, one line each; nothing in the working tree is modified |
| Hooks | Every edited file is formatted; before a turn ends, `go vet` runs on the packages that changed |
| `godon-subagents` skill | A gate before delegating: subagents only for large research that splits into four or more independent, read-only parts; at most two at a time; never for edits or small tasks |

All detail goes to `<OS temp dir>/godon/<repository>/`, never into the working
tree.

## What it saves

Measured on a real Go service of 57 packages and ~750 tests. Tokens are
estimated as characters ÷ 4 of what the agent reads.

| Case | Raw command | godon |
|---|---:|---:|
| One test function passes (`go test -v -run`) | 53 | 48 |
| One package passes (`go test -v`) | 2,880 | 48 |
| Whole suite passes (`go test ./...`) | 2,794 | 47 |
| Whole suite passes (`go test -v ./...`) | 33,743 | 47 |
| Two Ginkgo specs fail (`-ginkgo.v`) | 799 | 83, or 425 with one failure file read |
| Two small `t.Errorf` failures | 89 | 125 |
| Declaration lookup, rare name (`grep -rn`) | 1,214 | 75 |
| Declaration lookup, field name used in 13 types | 4,767 | 274 |
| Declaration lookup, `Error` | 60,742 | 246 |
| Static checks, clean tree (`gofmt -l` + `go vet`) | 22 | 64 |

Where the raw output is already tiny — one passing test, a two-line
assertion, a clean `go vet` — godon's summary line and file paths cost a few
dozen tokens more. Where it is not, godon is 20–700× smaller, and the raw
column is often worse than shown: an agent that cannot find the cause in a
truncated log re-runs the tests to see more. The clean-tree row also hides a
difference in coverage: `gofmt -l` and `go vet` do not notice a stale
generated mock, which `godon check` does.

The fixed cost per session is the skill listing, about 260 tokens, plus the
pointer lines in `CLAUDE.md`/`AGENTS.md`. A skill's body (350–860 tokens)
loads only in a session that uses it.

Every tool result also stays in the context for the rest of the session and
is sent again with each later turn, so a log read once early in a task is paid
for many times over. That repetition is where most of the saving comes from.

## Requirements

- The Go toolchain and git, as for any Go repository.
- **Windows:** Claude Code runs the hooks through Git Bash (Git for Windows).
  Codex on Windows uses `bin/godon.ps1` through PowerShell.
- Network access to GitHub the first time, to fetch the `godon` binary for
  your OS and CPU (see [Offline machines](#offline-machines)).

## Install

### Claude Code

```text
/plugin marketplace add doniantoro/godon-harness
/plugin install godon-harness@godon-harness
```

For a whole team, run these once in the repository and commit
`.claude/settings.json`:

```bash
claude plugin marketplace add doniantoro/godon-harness --scope project
claude plugin install godon-harness@godon-harness --scope project
```

Each teammate trusts the folder and installs once. A plugin cannot grant
permissions, so add this to the same `.claude/settings.json` to let the agent
run godon without a prompt:

```json
{ "permissions": { "allow": ["Bash(godon:*)"] } }
```

### Codex

```bash
codex plugin marketplace add doniantoro/godon-harness
codex plugin add godon-harness@godon-harness
```

Codex does not run a plugin's hooks until you review and trust them; do that
once after installing. The skills and the `godon` command work without hooks.

### Tell the agent to use it

Add one line to the repository's `CLAUDE.md` (Claude Code) or `AGENTS.md`
(Codex), so the skills are used rather than merely available:

```markdown
Run tests, static checks and declaration lookups through the godon skills
(godon-test, godon-check, godon-find-code), not raw `go test`, `go vet` or `grep`.
Before starting any subagent, follow the godon-subagents skill.
```

## Commands

```bash
godon test ./pkg -run TestName            # the test being worked on
godon test ./pkg -focus 'spec' | -label 'expr'   # Ginkgo specs; only Ginkgo packages run
godon test ./pkg -run TestName --with-all # then every unit test, only if it passes
godon test --all                          # every unit test
godon test --integration [pkgs]           # -tags=integration, 30m timeout, no cache

godon symbol Name | Type.Name [path] [max] [--doc] [--tests]
godon check [--no-lint]
godon version
```

`godon <command> -h` prints the details and exit codes. On Claude Code
`godon` is on PATH while the plugin is enabled; elsewhere call
`<plugin root>/bin/godon` (or `bin\godon.ps1` in PowerShell).

Defaults today: the race detector is on except on Windows (it needs cgo and a
C compiler; `--race`/`--no-race` override it), and the integration suite is
every package under `-tags=integration`. Per-repository configuration is the
next planned step.

## How the binary arrives

`bin/godon` (sh) and `bin/godon.ps1` (PowerShell) are launchers with no logic
of their own. On first use they download the release named in `VERSION` for
the current OS and CPU, verify it against the release's `checksums.txt`
(SHA-256), cache it, and run it. Updating the plugin to a new `VERSION`
fetches the new binary on next use.

| Variable | Effect |
|---|---|
| `GODON_BIN` | Run this binary instead of downloading |
| `GODON_CACHE` | Cache directory (default `~/.cache/godon`, or `%LOCALAPPDATA%\godon` in PowerShell) |
| `GODON_RELEASE_URL` | Download base URL, for a mirror |
| `GODON_OUT` | Root of the output directories (default: the OS temp dir) |

### Offline machines

Install the binary yourself and point the launcher at it:

```bash
go install github.com/doniantoro/godon-harness/cmd/godon@v$(cat VERSION)
export GODON_BIN="$(go env GOPATH)/bin/godon"
```

If a download fails inside a hook, the hook prints why and lets the agent
carry on rather than blocking its turn.

## Repository layout

```text
.claude-plugin/        Claude Code manifest and marketplace
plugin.json            Codex (portable) manifest
.agents/plugins/       Codex marketplace
bin/                   launchers
skills/                godon-test, godon-find-code, godon-check, godon-subagents (shared by both agents)
hooks/                 claude.json, codex.json
cmd/godon/             the CLI
internal/driver/       the language seam; internal/driver/golang is the Go driver
internal/report/       summaries and failure files, language-neutral
```

Adding a language means adding a `driver.Driver` under `internal/driver/` and
listing it in `cmd/godon/main.go`.

## Releasing

1. Tag any commit `v<X.Y.Z>` and push the tag. The release workflow runs
   the tests and publishes the binaries for that tag with GoReleaser.
2. To make the launchers use it, run `scripts/bump.sh -b X.Y.Z`, which sets
   `VERSION` and the `version` in both `.claude-plugin/plugin.json` and
   `plugin.json` (they must match; CI checks). Merge to `main`.

A change to skills or hooks alone needs no new binary: bump the plugin
version with `scripts/bump.sh X.Y.Z` and leave `VERSION` as it is.
