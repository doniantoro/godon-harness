package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
)

const symbolUsage = `godon symbol — where an identifier is declared

  godon symbol <Name|Type.Name> [path] [max] [--doc] [--tests]

  Name        exact, case-sensitive: functions, methods, types, interface
              methods, struct fields, consts and vars
  Type.Name   only Name declared on Type
  path        only under this directory
  max         lines to print, default 20; the output says when more exist
  --doc       also print the signature and comment of the first 3
  --tests     include test files and generated mocks

Exit: 0 found, 1 nothing declared under that name, 2 usage.
`

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

func cmdSymbol(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var (
		name, path      string
		max             = 20
		withDoc, withTs bool
		positional      int
	)
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, symbolUsage)
			return exitOK
		case "--doc":
			withDoc = true
		case "--tests":
			withTs = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(stderr, "godon symbol: unknown flag %s\n\n%s", a, symbolUsage)
				return exitUsage
			}
			positional++
			// A number is the limit wherever it appears, unless a directory
			// has that name.
			if n, err := strconv.Atoi(a); err == nil && positional > 1 && n > 0 {
				if _, statErr := os.Stat(a); statErr != nil {
					max = n
					continue
				}
			}
			switch {
			case name == "":
				name = a
			case path == "":
				path = a
			default:
				fmt.Fprint(stderr, symbolUsage)
				return exitUsage
			}
		}
	}
	if !identifier.MatchString(name) {
		fmt.Fprintf(stderr, "godon symbol: %q is not an identifier or Type.Name\n\n%s", name, symbolUsage)
		return exitUsage
	}

	w, ok := openWorkspace(cwd(), stderr)
	if !ok {
		return exitUsage
	}
	q := driver.SymbolQuery{Name: name, IncludeTests: withTs}
	if owner, member, found := strings.Cut(name, "."); found {
		q.Owner, q.Name = owner, member
	}
	if path != "" {
		abs, err := filepath.Abs(path)
		if err == nil {
			if rel, err := filepath.Rel(w.root, abs); err == nil {
				path = filepath.ToSlash(rel)
			}
		}
		if _, err := os.Stat(filepath.Join(w.root, filepath.FromSlash(path))); err != nil {
			fmt.Fprintf(stderr, "godon symbol: path does not exist: %s\n", path)
			return exitUsage
		}
		q.Path = path
	}

	found, err := w.driver.FindSymbols(w.root, q)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}
	if len(found) == 0 {
		scope := "the repository"
		if q.Path != "" {
			scope = q.Path
		}
		hint := ""
		if !withTs {
			hint = " (tests and mocks excluded; add --tests to include them)"
		}
		fmt.Fprintf(stdout, "no declaration named %s in %s%s\n", name, scope, hint)
		return exitFail
	}

	shown := min(len(found), max)
	if shown < len(found) {
		fmt.Fprintf(stdout, "%s: showing %d of %d declarations — narrow with a path or raise max\n", name, shown, len(found))
	} else {
		fmt.Fprintf(stdout, "%s: %d declaration(s)\n", name, len(found))
	}
	for _, s := range found[:shown] {
		fmt.Fprintf(stdout, "%s:%d  %s  %s\n", s.File, s.Line, s.Qualified(), s.Kind)
	}

	if withDoc {
		for _, s := range found[:min(shown, 3)] {
			doc, err := w.driver.SymbolDoc(ctx, w.root, s)
			if err != nil {
				doc = "(no doc: " + err.Error() + ")"
			}
			fmt.Fprintf(stdout, "\n--- %s (%s:%d)\n%s\n", s.Qualified(), s.File, s.Line, doc)
		}
	}
	return exitOK
}
