---
name: godon-diff
description: Review what changed — the working tree, the staged index, a commit or a branch — with one line per file and only the patches worth reading. Use instead of `git diff` or `git show` whenever looking at changes, your own or a branch's.
---

# Reading changes with godon

```bash
godon diff                     # working tree + staged vs HEAD, plus untracked files
godon diff --staged            # only what is staged
godon diff main...HEAD         # what this branch changed; HEAD~1..HEAD for the last commit
godon diff [rev] <path>...     # only these files or directories
godon diff [rev] -- <path>     # a path that no longer exists
```

Calling `godon`: on Claude Code it is on PATH; otherwise run
`<plugin root>/bin/godon` (Windows PowerShell: `<plugin root>\bin\godon.ps1`),
two directories above this file.

The first lines are a summary — status (`M A D R ?`), lines added and
removed, path, and a note for `untracked`, `binary`, `generated`, `lock` or
`vendor` files.

- **Short change**: the patches follow the summary. Generated, lock and
  vendored files are listed but their patch is left out.
- **Large change**: only the summary prints. Pick the files that matter from
  it and read each with `godon diff [rev] -- <path>`, or open
  `<patches>/<path>.patch`. Do not read every file; do not fall back to
  `git diff`.
- A file named on the command line is always printed, generated or not.
- Exit `3` means there is nothing to show for that scope — not an error.

`git status`, `git log` and `git blame` are still the tools for what they do;
godon replaces only the patch output.
