package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
)

const contextUsage = `godon context — what to know about a declaration before reading or changing it

  godon context <Name|Type.Name> [path]

  Prints the declaration's signature and range, the interfaces it
  implements, what it uses, who uses it — calls made through an interface
  included — the tests that reach it, and the line ranges worth reading.
  Each list stops at 10; the full lists are written to a file.

  Name        exact, case-sensitive, as for godon symbol
  Type.Name   only Name declared on Type
  path        only under this directory

Exit: 0 found, 1 no declaration or more than one (they are listed), 2 usage,
4 the packages could not be loaded.
`

// contextMax is how many entries each section prints.
const contextMax = 10

func cmdContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var name, path string
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(stdout, contextUsage)
			return exitOK
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "godon context: unknown flag %s\n\n%s", a, contextUsage)
			return exitUsage
		case name == "":
			name = a
		case path == "":
			path = a
		default:
			fmt.Fprint(stderr, contextUsage)
			return exitUsage
		}
	}
	if !identifier.MatchString(name) {
		fmt.Fprintf(stderr, "godon context: %q is not an identifier or Type.Name\n\n%s", name, contextUsage)
		return exitUsage
	}

	w, ok := openWorkspace(cwd(), stderr)
	if !ok {
		return exitUsage
	}
	q := driver.ContextQuery{Name: name}
	if owner, member, found := strings.Cut(name, "."); found {
		q.Owner, q.Name = owner, member
	}
	if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			if rel, err := filepath.Rel(w.root, abs); err == nil {
				path = filepath.ToSlash(rel)
			}
		}
		if _, err := os.Stat(filepath.Join(w.root, filepath.FromSlash(path))); err != nil {
			fmt.Fprintf(stderr, "godon context: path does not exist: %s\n", path)
			return exitUsage
		}
		q.Path = path
	}

	c, err := w.driver.Context(ctx, w.root, q)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}
	if !c.Found {
		if len(c.Candidates) == 0 {
			fmt.Fprintf(stdout, "no declaration named %s (tests and mocks excluded)\n", name)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s: %d declarations — name one with Type.Name or a path:\n", name, len(c.Candidates))
		for _, s := range c.Candidates {
			fmt.Fprintf(stdout, "  %s:%d  %s  %s\n", s.File, s.Line, s.Qualified(), s.Kind)
		}
		return exitFail
	}

	short, full := formatContext(c, contextMax)
	fmt.Fprint(stdout, short)
	if short != full {
		env, err := w.env("context")
		if err == nil {
			p := filepath.Join(env.RunDir, "context.txt")
			if os.WriteFile(p, []byte(full), 0o644) == nil {
				fmt.Fprintf(stdout, "\nfull lists: %s\n", p)
			}
		}
	}
	return exitOK
}

// formatContext renders c with each list cut at max, and in full.
func formatContext(c driver.SymbolContext, limit int) (short, full string) {
	return renderContext(c, limit), renderContext(c, -1)
}

func renderContext(c driver.SymbolContext, limit int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SYMBOL: %s  %s  %s\n", c.Symbol.Qualified(), c.Symbol.Kind, locString(c.Loc))
	if c.Signature != "" {
		fmt.Fprintf(&b, "  %s\n", c.Signature)
	}

	section := func(title string, refs []driver.Ref, always bool, extra ...string) {
		if len(refs) == 0 && len(extra) == 0 && !always {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		if len(refs) == 0 && len(extra) == 0 {
			b.WriteString("  none\n")
			return
		}
		shown := refs
		if limit >= 0 && len(refs) > limit {
			shown = refs[:limit]
		}
		width := 0
		for _, r := range shown {
			width = max(width, len(r.Name))
		}
		for _, r := range shown {
			line := fmt.Sprintf("  %-*s  %s", width, r.Name, locString(r.Loc))
			if r.Via != "" {
				line += "  via " + r.Via
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
		if len(shown) < len(refs) {
			fmt.Fprintf(&b, "  + %d more\n", len(refs)-len(shown))
		}
		for _, e := range extra {
			b.WriteString("  " + e + "\n")
		}
	}

	section("Implements", c.Implements, false)
	section("Implementations", c.Implementations, false)
	section("Methods", c.Methods, false)
	var external []string
	if len(c.External) > 0 {
		external = append(external, "+ external: "+strings.Join(c.External, ", "))
	}
	section("Direct dependencies", c.Deps, true, external...)
	section("Referenced by", c.Callers, true)
	b.WriteString("\nRelevant tests:\n")
	if len(c.Tests) == 0 {
		b.WriteString("  none\n")
	}
	for i, t := range c.Tests {
		if limit >= 0 && i == limit {
			fmt.Fprintf(&b, "  + %d more\n", len(c.Tests)-limit)
			break
		}
		fmt.Fprintf(&b, "  %s  %s\n", locString(t.Loc), t.Note)
	}

	if len(c.Reads) > 0 {
		b.WriteString("\nRead:\n")
		files := map[string]bool{}
		lines := 0
		for _, l := range c.Reads {
			fmt.Fprintf(&b, "  %s\n", locString(l))
			files[l.File] = true
			lines += l.EndLine - l.Line + 1
		}
		fmt.Fprintf(&b, "  (%d files, %d lines)\n", len(files), lines)
	}
	for _, w := range c.Warnings {
		b.WriteString("\n" + w)
	}
	if len(c.Warnings) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}

func locString(l driver.Loc) string {
	if l.File == "" {
		return ""
	}
	if l.EndLine > l.Line {
		return fmt.Sprintf("%s:%d-%d", l.File, l.Line, l.EndLine)
	}
	return fmt.Sprintf("%s:%d", l.File, l.Line)
}
