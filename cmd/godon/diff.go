package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/doniantoro/godon-harness/internal/diff"
	"github.com/doniantoro/godon-harness/internal/output"
	"github.com/doniantoro/godon-harness/internal/repo"
)

const diffUsage = `godon diff — what changed, with only the patches worth reading

  godon diff                      working tree and index against HEAD, plus untracked files
  godon diff --staged             only what is staged
  godon diff <rev>                against a revision or range: main, main...HEAD, HEAD~3
  godon diff [rev] <path>...      only these files or directories
  godon diff [rev] -- <path>...   the same, for a path that no longer exists

  One line per file: status, lines added and removed, path. Patches follow
  when they are short (150 lines, or 400 when paths are named); otherwise
  only the summary prints and each file's patch is written to
  <patches>/<path>.patch. Generated files, lock files and vendored code are
  never printed unless named on the command line.

Exit: 0 changes found, 3 no changes, 2 usage, 4 git failed (bad revision).
`

// Inline limits, in patch lines.
const (
	inlineLines      = 150
	inlineNamedLines = 400
)

func cmdDiff(_ context.Context, args []string, stdout, stderr io.Writer) int {
	var opts diff.Options
	var paths []string
flags:
	for i, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(stdout, diffUsage)
			return exitOK
		case a == "--staged" || a == "--cached":
			opts.Staged = true
		case a == "--":
			paths = append(paths, args[i+1:]...)
			break flags
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "godon diff: unknown flag %s\n\n%s", a, diffUsage)
			return exitUsage
		default:
			// An argument on disk is a path; anything else is a revision,
			// which git rejects if it is not one.
			if _, err := os.Stat(a); err == nil || opts.Rev != "" {
				paths = append(paths, a)
			} else {
				opts.Rev = a
			}
		}
	}

	root, err := repo.Root(cwd())
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitUsage
	}
	for _, p := range paths {
		opts.Paths = append(opts.Paths, relToRoot(root, p))
	}

	files, err := diff.Collect(root, opts)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}
	scope := describe(opts)
	if len(files) == 0 {
		fmt.Fprintf(stdout, "no changes (%s)\n", scope)
		return exitNoChanges
	}

	named := map[string]bool{}
	for _, p := range opts.Paths {
		named[p] = true
	}
	shown := func(f diff.File) bool {
		return named[f.Path] || named[f.OldPath] || (f.Kind == diff.Source && !f.Binary)
	}
	limit := inlineLines
	if len(opts.Paths) > 0 {
		limit = inlineNamedLines
	}
	var added, deleted, inline int
	for _, f := range files {
		added += f.Added
		deleted += f.Deleted
		if shown(f) {
			inline += f.Lines()
		}
	}
	fits := inline <= limit

	dir, err := writePatches(repo.Name(root), files)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}

	fmt.Fprintf(stdout, "%s: %d files, +%d -%d\n", scope, len(files), added, deleted)
	hidden := false
	for _, f := range files {
		name := f.Path
		if f.OldPath != "" {
			name = f.OldPath + " -> " + f.Path
		}
		var notes []string
		if f.Status == "?" {
			notes = append(notes, "untracked")
		}
		if f.Binary {
			notes = append(notes, "binary")
		}
		if f.Kind != diff.Source {
			notes = append(notes, f.Kind.String())
		}
		if fits && !shown(f) && !f.Binary {
			notes = append(notes, "patch not shown")
			hidden = true
		}
		line := fmt.Sprintf("%s %5s %5s  %s", f.Status, fmt.Sprintf("+%d", f.Added), fmt.Sprintf("-%d", f.Deleted), name)
		if len(notes) > 0 {
			line += "  " + strings.Join(notes, ", ")
		}
		fmt.Fprintln(stdout, line)
	}

	if !fits {
		fmt.Fprintf(stdout, "patches are %d lines, not printed\n", inline)
		fmt.Fprintf(stdout, "patches: %s\n", dir)
		fmt.Fprintln(stdout, "read one with: godon diff -- <path>   (or open <patches>/<path>.patch)")
		return exitOK
	}
	if hidden {
		fmt.Fprintf(stdout, "full patch: %s\n", filepath.Join(dir, "all.patch"))
	}
	for _, f := range files {
		if shown(f) {
			fmt.Fprint(stdout, "\n"+f.Patch)
		}
	}
	return exitOK
}

func describe(o diff.Options) string {
	switch {
	case o.Staged && o.Rev != "":
		return "staged vs " + o.Rev
	case o.Staged:
		return "staged vs HEAD"
	case strings.Contains(o.Rev, ".."):
		return o.Rev
	case o.Rev != "":
		return "working tree vs " + o.Rev
	default:
		return "working tree vs HEAD"
	}
}

// relToRoot turns a path given relative to the current directory into one
// relative to the repository root, which is where git runs.
func relToRoot(root, p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// writePatches writes all.patch and one <path>.patch per file into a new run
// directory.
func writePatches(repoName string, files []diff.File) (string, error) {
	r, err := output.NewRun(repoName, "diff")
	if err != nil {
		return "", err
	}
	var all strings.Builder
	for _, f := range files {
		all.WriteString(f.Patch)
		p := filepath.Join(r.Dir, filepath.FromSlash(f.Path)+".patch")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(f.Patch), 0o644); err != nil {
			return "", err
		}
	}
	return r.Dir, os.WriteFile(filepath.Join(r.Dir, "all.patch"), []byte(all.String()), 0o644)
}
