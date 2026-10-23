---
name: godon-find-code
description: Find and trace Go code — where an identifier is declared, and before reading or changing a function, type or interface, what it uses, who calls it (through interfaces too), which tests reach it and which line ranges to read. Use instead of `grep -rn <name>` and instead of reading whole files to trace a call.
---

# Finding and tracing code with godon

| Need | Command |
|---|---|
| Where is X declared? | `godon symbol X` |
| What do I need to know before reading or changing X? | `godon context X` |

```bash
godon symbol CreateOrder                      # every declaration named CreateOrder
godon symbol OrderStore.CreateOrder --doc     # one owner, plus signature and comment
godon symbol Total internal/billing           # only under a directory
godon symbol Handler --tests                  # include _test.go files and mocks

godon context Service.CreateOrder             # signature, deps, callers, tests, ranges to read
godon context CreateOrder internal/billing    # pick one declaration by directory
godon context OrderStore                      # an interface: its implementations and users
```

Calling `godon`: on Claude Code it is on PATH; otherwise run
`<plugin root>/bin/godon` (Windows PowerShell: `<plugin root>\bin\godon.ps1`),
two directories above this file.

## godon symbol

Each result is `path:line  Owner.Name  kind`. Then read **only the
declaration**: open the file at that line with a small window around it,
not the whole file. `--doc` often makes reading the file unnecessary.

- "showing N of M" means the list was cut: narrow it with a directory rather
  than raising the limit.
- "no declaration named X" is exact and case-sensitive; check the spelling,
  or add `--tests`.

## godon context

Prints the declaration with its line range and signature, then:

- **Implements** / **Implementations** / **Methods** — the interfaces it
  satisfies, or for an interface what satisfies it.
- **Direct dependencies** — repository declarations it uses, with their
  location; other packages on one line.
- **Referenced by** — each non-test caller by its enclosing function.
  `via types.X` means the call goes through interface `types.X`; it is still
  a caller of this method. Generated mocks are never counted.
- **Relevant tests** — where tests use it, named by the Ginkgo `It`/`Describe`
  text or the test function; pass that text to `godon test -focus`.
- **Read** — the line ranges worth opening, most useful first. Read those
  ranges with an offset and limit, not the whole files; open more only if
  they leave a question.

`none` under a heading is an answer, not a failure. Each list stops at 10;
the "full lists" file has the rest. A name with several declarations prints
the candidates and exits 1: repeat with `Type.Name` or a directory. A line
starting `incomplete:` means a package does not build, so its uses may be
missing. The first call after the Go build cache was cleared takes up to
half a minute; later calls take about a second.

Use grep for string literals, configuration and non-Go files — godon reads
Go declarations and their uses only.
