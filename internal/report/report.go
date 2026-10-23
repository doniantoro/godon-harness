// Package report writes what an agent reads: a short summary per stage and
// one file per failure holding only the lines that explain it. It knows
// nothing about any language; drivers produce Failures, this package files
// them.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Status of a stage.
const (
	Passed    = "PASSED"
	Failed    = "FAILED"
	NoMatches = "NO TESTS MATCHED"
)

// Failure is one classified failure.
type Failure struct {
	Category string   // build, assertion, mock, panic, timeout, race, leak, setup, unclassified
	Package  string   // display name, already shortened by the driver
	Test     string   // empty for a package-level failure
	Lines    []string // the extract
	Total    int      // lines available before extraction
}

// Stage is the result of one test stage.
type Stage struct {
	Name     string
	Status   string
	Packages int
	Tests    int
	Failures []Failure
	Dir      string // where summary.txt, full.log and failures/ live
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Write files the failures and summary.txt, and returns the summary text.
func (s *Stage) Write() (string, error) {
	var sb strings.Builder
	switch s.Status {
	case Failed:
		fmt.Fprintf(&sb, "STAGE %s: FAILED — %d failure(s)\n", s.Name, len(s.Failures))
	case NoMatches:
		fmt.Fprintf(&sb, "STAGE %s: NO TESTS MATCHED — the -run/-focus/-label pattern selected nothing, so nothing was verified\n", s.Name)
	default:
		fmt.Fprintf(&sb, "STAGE %s: PASSED — %d package(s), %d test(s)\n", s.Name, s.Packages, s.Tests)
	}

	if len(s.Failures) > 0 {
		failDir := filepath.Join(s.Dir, "failures")
		if err := os.MkdirAll(failDir, 0o755); err != nil {
			return "", err
		}
		for i, f := range s.Failures {
			name := fmt.Sprintf("%02d-%s-%s.txt", i+1, f.Category, unsafeChars.ReplaceAllString(f.Package, "_"))
			path := filepath.Join(failDir, name)
			if err := writeFailure(path, f); err != nil {
				return "", err
			}
			test := f.Test
			if test == "" {
				test = "(package)"
			}
			fmt.Fprintf(&sb, "- [%s] %s %s → %s\n", f.Category, f.Package, test, path)
		}
	}
	fmt.Fprintf(&sb, "full log: %s\n", filepath.Join(s.Dir, "full.log"))

	summary := sb.String()
	return summary, os.WriteFile(filepath.Join(s.Dir, "summary.txt"), []byte(summary), 0o644)
}

func writeFailure(path string, f Failure) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "category: %s\npackage:  %s\n", f.Category, f.Package)
	if f.Test != "" {
		fmt.Fprintf(&sb, "test:     %s\n", f.Test)
	}
	fmt.Fprintf(&sb, "--- extract: %d of %d line(s); the rest is in full.log ---\n", len(f.Lines), f.Total)
	for _, l := range f.Lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// Check is the result of one static check.
type Check struct {
	Name   string
	Status string // OK, FAILED, STALE, SKIPPED
	Detail string
}

// Failing reports whether the check should fail the run.
func (c Check) Failing() bool { return c.Status == "FAILED" || c.Status == "STALE" }

// Line formats a check as one summary line.
func (c Check) Line() string {
	return strings.TrimRight(fmt.Sprintf("%-14s %-8s %s", c.Name, c.Status, c.Detail), " ")
}
