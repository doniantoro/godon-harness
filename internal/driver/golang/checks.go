package golang

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/report"
)

var (
	vetIssue  = regexp.MustCompile(`(?m)^[^#\s].*\.go:\d+:\d+: `)
	lintIssue = regexp.MustCompile(`(?m)^\S+\.go:\d+:\d+: .* \(([a-z0-9-]+)\)$`)
)

// Checks implements driver.Driver. Nothing in the working tree is modified:
// formatting is compared in memory and mocks are regenerated into the run
// directory.
func (d Driver) Checks(ctx context.Context, env driver.Env, opts driver.CheckOptions) ([]report.Check, error) {
	checks := []report.Check{
		checkFormat(env),
		checkVet(ctx, env),
		checkMocks(ctx, env),
	}
	if opts.Lint {
		checks = append(checks, checkLint(ctx, env))
	} else {
		checks = append(checks, report.Check{Name: "lint", Status: "SKIPPED", Detail: "--no-lint"})
	}
	return checks, nil
}

func detail(env driver.Env, name string, body []byte) string {
	path := filepath.Join(env.RunDir, name+".txt")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "(could not write detail: " + err.Error() + ")"
	}
	return path
}

// goFiles lists the repository's Go files, slash-separated relative paths.
func goFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func checkFormat(env driver.Env) report.Check {
	files, err := goFiles(env.Root)
	if err != nil {
		return report.Check{Name: "gofmt", Status: "FAILED", Detail: err.Error()}
	}
	var bad []string
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(env.Root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		formatted, err := format.Source(src)
		switch {
		case err != nil:
			bad = append(bad, f+"  (does not parse)")
		case !bytes.Equal(formatted, src):
			bad = append(bad, f)
		}
	}
	if len(bad) == 0 {
		return report.Check{Name: "gofmt", Status: "OK"}
	}
	path := detail(env, "gofmt", []byte(strings.Join(bad, "\n")+"\n"))
	return report.Check{Name: "gofmt", Status: "FAILED", Detail: fmt.Sprintf("%d file(s) → %s", len(bad), path)}
}

func checkVet(ctx context.Context, env driver.Env) report.Check {
	out, code, err := run(ctx, env.Root, "go", "vet", "./...")
	if err != nil {
		return report.Check{Name: "go vet", Status: "FAILED", Detail: err.Error()}
	}
	if code == 0 {
		return report.Check{Name: "go vet", Status: "OK"}
	}
	path := detail(env, "vet", out)
	return report.Check{Name: "go vet", Status: "FAILED",
		Detail: fmt.Sprintf("%d issue(s) → %s", len(vetIssue.FindAll(out, -1)), path)}
}

func checkLint(ctx context.Context, env driver.Env) report.Check {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		return report.Check{Name: "lint", Status: "SKIPPED", Detail: "golangci-lint not installed"}
	}
	out, code, err := run(ctx, env.Root, bin, "run", "./...")
	if err != nil {
		return report.Check{Name: "lint", Status: "FAILED", Detail: err.Error()}
	}
	if code == 0 {
		return report.Check{Name: "lint", Status: "OK"}
	}
	counts := map[string]int{}
	matches := lintIssue.FindAllSubmatch(out, -1)
	for _, m := range matches {
		counts[string(m[1])]++
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return counts[names[i]] > counts[names[j]] })
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s %d", n, counts[n]))
	}
	byLinter := ""
	if len(parts) > 0 {
		byLinter = " (" + strings.Join(parts, ", ") + ")"
	}
	path := detail(env, "lint", out)
	return report.Check{Name: "lint", Status: "FAILED", Detail: fmt.Sprintf("%d issue(s)%s → %s", len(matches), byLinter, path)}
}

// --- mock freshness ---------------------------------------------------------

type directive struct {
	file string // slash-separated, relative to root
	line int
	args []string
}

// checkMocks re-runs each //go:generate mockgen directive with its
// destination redirected into the run directory, and compares the result
// with the committed file. The mockgen version is the one go.mod pins.
func checkMocks(ctx context.Context, env driver.Env) report.Check {
	directives, err := mockDirectives(env.Root)
	if err != nil {
		return report.Check{Name: "mocks", Status: "FAILED", Detail: err.Error()}
	}
	if len(directives) == 0 {
		return report.Check{Name: "mocks", Status: "SKIPPED", Detail: "no //go:generate mockgen directives"}
	}

	var pkg string
	switch {
	case requires(env.Root, "go.uber.org/mock"):
		pkg = "go.uber.org/mock/mockgen"
	case requires(env.Root, "github.com/golang/mock"):
		pkg = "github.com/golang/mock/mockgen"
	default:
		return report.Check{Name: "mocks", Status: "SKIPPED", Detail: "go.mod requires no mockgen module"}
	}
	mockgen := filepath.Join(env.CacheDir, exe("mockgen"))
	// mockgen is a tool, not a vendored library: build it from the module
	// cache. -mod=readonly guarantees go.mod and go.sum are left untouched.
	if out, code, err := run(ctx, env.Root, "go", "build", "-mod=readonly", "-o", mockgen, pkg); err != nil || code != 0 {
		path := detail(env, "mocks", out)
		return report.Check{Name: "mocks", Status: "FAILED", Detail: "could not build mockgen → " + path}
	}

	module := modulePath(env.Root)
	var problems []string
	for i, d := range directives {
		if p := freshness(ctx, env, mockgen, module, d, i); p != "" {
			problems = append(problems, p)
		}
	}
	if len(problems) == 0 {
		return report.Check{Name: "mocks", Status: "OK", Detail: fmt.Sprintf("%d generated file(s) current", len(directives))}
	}
	path := detail(env, "mocks", []byte(strings.Join(problems, "\n")+"\n"))
	return report.Check{Name: "mocks", Status: "STALE", Detail: fmt.Sprintf("%d of %d → %s", len(problems), len(directives), path)}
}

// freshness returns "" when the directive's output matches the committed file.
func freshness(ctx context.Context, env driver.Env, mockgen, module string, d directive, n int) string {
	where := fmt.Sprintf("%s:%d", d.file, d.line)
	dest, args := "", make([]string, 0, len(d.args)+1)
	hasSelf := false
	tmp := filepath.Join(env.RunDir, fmt.Sprintf("mock.%d.go", n))
	for _, a := range d.args {
		if v, ok := strings.CutPrefix(a, "-destination="); ok {
			dest = v
			a = "-destination=" + tmp
		}
		if strings.HasPrefix(a, "-self_package") {
			hasSelf = true
		}
		args = append(args, a)
	}
	if dest == "" {
		return "" // writes to stdout; nothing committed to compare against
	}
	dir := filepath.Join(env.Root, filepath.FromSlash(slashDir(d.file)))
	destPath := filepath.Join(dir, filepath.FromSlash(dest))
	self := ""
	if !hasSelf && module != "" {
		if rel, err := filepath.Rel(env.Root, filepath.Dir(destPath)); err == nil {
			// mockgen infers the mock's own import path from where it writes;
			// a destination outside the module would change it, so pass it.
			// It goes first: flag parsing stops at package mode's positional
			// arguments (". SomeInterface").
			self = "-self_package=" + module + "/" + filepath.ToSlash(rel)
			args = append([]string{self}, args...)
		}
	}

	out, code, err := run(ctx, dir, mockgen, args...)
	if err != nil || code != 0 {
		return fmt.Sprintf("mockgen failed: %s\n%s", where, firstLines(string(out), 3))
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		return "mockgen wrote nothing: " + where
	}
	_ = os.Remove(tmp) // best effort: the run directory is pruned anyway
	// The generated header records the command line: restore the directive's
	// own destination and drop the flag added above.
	got = bytes.ReplaceAll(got, []byte("-destination="+tmp), []byte("-destination="+dest))
	if self != "" {
		got = bytes.ReplaceAll(got, []byte(self+" "), nil)
		got = bytes.ReplaceAll(got, []byte(" "+self), nil)
	}
	want, err := os.ReadFile(destPath)
	if err != nil || !bytes.Equal(got, want) {
		rel, _ := filepath.Rel(env.Root, destPath)
		return fmt.Sprintf("stale: %s (from %s) — regenerate with go generate", filepath.ToSlash(rel), where)
	}
	return ""
}

func slashDir(slashPath string) string {
	if i := strings.LastIndex(slashPath, "/"); i >= 0 {
		return slashPath[:i]
	}
	return "."
}

func mockDirectives(root string) ([]directive, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	var out []directive
	for _, f := range files {
		fh, err := os.Open(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			rest, ok := strings.CutPrefix(line, "//go:generate ")
			if !ok {
				continue
			}
			args := splitArgs(rest)
			// mockgen ..., or go run <module>/mockgen[@version] ...
			switch {
			case len(args) > 0 && args[0] == "mockgen":
				args = args[1:]
			case len(args) > 2 && args[0] == "go" && args[1] == "run" && strings.Contains(args[2], "/mockgen"):
				args = args[3:]
			default:
				continue
			}
			out = append(out, directive{file: f, line: n, args: args})
		}
		fh.Close()
	}
	return out, nil
}

// splitArgs splits a go:generate line the way the go tool does: on spaces,
// with double-quoted words kept together.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, started = !inQuote, true
		case (r == ' ' || r == '\t') && !inQuote:
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}
