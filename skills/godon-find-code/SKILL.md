---
name: godon-find-code
description: Find where a Go identifier is declared — function, method, type, interface method, struct field, const or var — with one line per declaration. Use instead of `grep -rn <name>`, which returns every mention instead of the declaration.
---

# Finding declarations with godon

```bash
godon symbol CreateOrder                      # every declaration named CreateOrder
godon symbol OrderStore.CreateOrder --doc     # one owner, plus signature and comment
godon symbol Total internal/billing           # only under a directory
godon symbol Total 50                         # print up to 50 (default 20)
godon symbol Handler --tests                  # include _test.go files and mocks
```

Calling `godon`: on Claude Code it is on PATH; otherwise run
`<plugin root>/bin/godon` (Windows PowerShell: `<plugin root>\bin\godon.ps1`),
two directories above this file.

Each result is `path:line  Owner.Name  kind`. Then read **only the
declaration**: open the file at that line with a small window around it,
not the whole file. `--doc` often makes reading the file unnecessary.

- "showing N of M" means the list was cut: narrow it with a directory rather
  than raising the limit.
- "no declaration named X" is exact and case-sensitive; check the spelling,
  or add `--tests`.
- Use grep for call sites, string literals and non-Go files — godon finds
  declarations only.
