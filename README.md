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
| `godon context` | Before reading or changing a declaration: its signature and range, interfaces it implements, what it uses, who calls it — through consumer-declared interfaces too — the tests that reach it, and the line ranges worth reading |
| `godon diff` | What changed — working tree, staged, a commit or a branch — one line per file; patches only when short, and never for generated, lock or vendored files unless asked |
| `godon check` | gofmt, go vet, mock freshness and golangci-lint, one line each; nothing in the working tree is modified |
| Hooks | Every edited file is formatted; before a turn ends, `go vet` runs on the packages that changed |
| `godon-subagents` skill | A gate before delegating: subagents only for large research that splits into four or more independent, read-only parts; at most two at a time; never for edits or small tasks |

All detail goes to `<OS temp dir>/godon/<repository>/`, never into the working
tree.

## What a harness is

An AI coding agent is a model in a loop: it reads, decides, calls a tool,
reads the result, and decides again. The model is only one part of that
loop. Everything around it — the tools it can call, what those tools print,
the instructions it starts with, the checks that run without being asked —
is the **harness**. A capable model in a poor harness behaves poorly: it
guesses where a function is, reads files it did not need, and declares work
finished that does not compile.

A harness has four parts, and godon supplies each one:

| Part | What it does | In godon |
|---|---|---|
| **Tools** | Commands the agent calls, shaped for a reader whose attention is measured in tokens | `godon test`, `symbol`, `context`, `diff`, `check` |
| **Instructions** | When to use which tool, loaded only when relevant | the skills: `godon-test`, `godon-find-code`, `godon-check`, `godon-diff`, `godon-subagents` |
| **Hooks** | Checks the agent does not have to remember, run by the host at fixed moments | format every edited file; `go vet` the changed packages before a turn ends |
| **Memory outside the context** | Detail kept on disk until it is needed | per-failure files, per-file patches and full lists under the temp directory |

## How the agent and the harness work together

The two divide the work by what each is good at. The agent is good at
judgement: what to change, which failure matters, whether a test describes
the right behaviour. It is poor at reading ten thousand lines without losing
the one that mattered, and at remembering to run a formatter. The harness
does the mechanical half, so the agent spends its context on the judgement.

```text
 agent                                 harness (godon)
 ─────                                 ───────────────
 "where is RecordStatement used?" ──▶  godon context: type-check the module,
                                       follow calls through interfaces
                                  ◀──  12 lines: callers, tests, ranges to read
 reads only those ranges
 edits the file              ──▶  hook: format it; report a parse error now
 "does it work?"             ──▶  godon test ./pkg -run …
                                  ◀──  "1 failed: assertion", one file path
 opens that one file, fixes it
 ends its turn               ──▶  hook: go vet the changed packages
                                  ◀──  blocks the turn if vet fails
```

Three rules keep the division working:

1. **Summary first, detail on request.** Every command prints a few lines and
   names a file for the rest. The agent decides what to open; nothing it did
   not ask for enters its context.
2. **Silence is never success.** A test filter that matched nothing exits 3,
   not 0; `none` under a heading is stated, not omitted; a package that does
   not build is reported as `incomplete`. An agent cannot see what is
   missing, so the harness says it.
3. **The host enforces, the agent decides.** Formatting and the end-of-turn
   gate run as hooks, outside the agent's choices. Everything that needs
   judgement — what to fix, whether to delegate — stays with the agent, and
   the skills only tell it which tool fits.

## Why godon

An agent working without a harness uses what any shell offers, and pays for
it in three ways:

- **Tokens.** `go test -v ./...` on a mid-sized service is ~34,000 tokens to
  say "everything passed"; `grep -rn Error` is ~60,000 tokens to find a
  handful of declarations. Every tool result stays in the conversation and is
  sent again with each later turn, so a log read once early is paid for many
  times. godon returns the same answers in tens to hundreds of tokens — see
  [What it saves](#what-it-saves).
- **Accuracy.** Text search does not understand code. It cannot tell a
  declaration from a mention, misses a caller that goes through a
  consumer-declared interface, and reports a mistyped `-run` filter as a
  pass. godon parses and type-checks instead of matching text, and reports
  the empty cases explicitly.
- **Consistency.** Without hooks, formatting and vetting depend on the agent
  remembering. With them, every edit is formatted and no turn ends on code
  that `go vet` rejects — in Claude Code and Codex alike, on Linux, macOS
  and Windows.

godon is also small to adopt: one plugin, no configuration, none of its own
files in the repository (the hooks only reformat files the agent edits), and
a prebuilt binary fetched on first use. Raw
commands remain available; godon is the default path, not a cage.

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
| Trace a method called only through an interface, 4 files | 11,774 | 2,153 |
| Trace a money-path method, callers in two modules, 22 specs, 6 files | 17,002 | 5,834 |
| Trace a helper with 6 callers and 4 tests, 7 files | 17,379 | 797 |
| Trace a repository method with one caller, 1 file | 1,703 | 182 |
| One-file commit, 17 lines (`git diff`) | 428 | 449 |
| 9-file commit, ~100 lines (`git diff`) | 3,217 | 231 summary; 1,398 with two files read; 3,449 with all |
| 30-file commit, ~1,500 lines (`git diff`) | 35,497 | 560 summary; 3,595 with two files read; 36,058 with all |
| 88-file commit, ~7,400 lines (`git diff`) | 102,589 | 1,369 summary; 1,564 with two files read; 95,117 with all |

A trace is finding a declaration, its callers, its tests and the code it
uses. The raw way is grep, then reading each file whole; godon's is
`godon context`, then reading only the line ranges it lists. Most of the
saving is the files not read whole. `godon context` also finds callers that
reach a method through an interface, which a search for the method's name
finds only by luck of naming. It type-checks the module: about a second on
this service, up to half a minute right after the Go build cache was
cleared. (`godon symbol` then grep then whole files costs nearly the raw
figure: it replaces only the declaration search.)

`godon diff` does not make a patch smaller. It saves by printing a summary
first, so the agent reads only the files it needs; reading every file costs
about as much as `git diff`, a little less only where generated, lock or
vendored files are left out. Use it to choose what to read, and expect no
saving when a review genuinely needs the whole change.

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
Run tests, static checks, declaration lookups, code tracing and diffs through the godon skills
(godon-test, godon-check, godon-find-code, godon-diff), not raw `go test`,
`go vet`, `grep` or `git diff`.
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
godon context Name | Type.Name [path]     # deps, callers via interfaces, tests, ranges to read
godon diff [--staged] [rev] [paths...]   # main...HEAD, HEAD~1..HEAD; -- <path> for one file
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
skills/                godon-test, godon-find-code, godon-check, godon-diff, godon-subagents (shared by both agents)
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
