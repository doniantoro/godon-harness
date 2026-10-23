package golang

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/doniantoro/godon-harness/internal/report"
)

// Extract limits. A failure file is meant to be read whole; these keep it
// small enough that reading it is always cheaper than re-running the test.
const (
	maxPlainLines  = 60
	maxBlockLines  = 40
	maxBlocks      = 5
	maxPanicLines  = 30
	maxLeakLines   = 40
	maxStderrLines = 40
)

type event struct {
	Action      string
	Package     string
	Test        string
	Output      string
	ImportPath  string
	FailedBuild string
}

type testResult struct {
	status string
	out    []string
}

type pkgResult struct {
	name        string
	status      string
	failedBuild string
	out         []string // output not attributed to a test: TestMain, goleak, build
	tests       map[string]*testResult
}

// parsed is a whole go test -json stream.
type parsed struct {
	pkgs     map[string]*pkgResult
	order    []string
	fullLog  []string
	testsRun int
}

var (
	ansi       = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	ginkgoZero = regexp.MustCompile(`Ran 0 of \d+ Specs?`)
)

func parseEvents(r io.Reader) (*parsed, error) {
	p := &parsed{pkgs: map[string]*pkgResult{}}
	builds := map[string][]string{}

	get := func(name string) *pkgResult {
		pr, ok := p.pkgs[name]
		if !ok {
			pr = &pkgResult{name: name, tests: map[string]*testResult{}}
			p.pkgs[name] = pr
			p.order = append(p.order, name)
		}
		return pr
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			// A non-JSON line on stdout is still output worth keeping.
			p.fullLog = append(p.fullLog, sc.Text())
			continue
		}

		switch ev.Action {
		case "build-output":
			line := clean(ev.Output)
			builds[ev.ImportPath] = append(builds[ev.ImportPath], line)
			p.fullLog = append(p.fullLog, line)
			continue
		case "build-fail":
			continue
		}
		if ev.Package == "" {
			continue
		}

		pr := get(ev.Package)
		if ev.Test == "" {
			switch ev.Action {
			case "output":
				line := clean(ev.Output)
				pr.out = append(pr.out, line)
				p.fullLog = append(p.fullLog, line)
			case "pass", "fail", "skip":
				pr.status = ev.Action
				if ev.FailedBuild != "" {
					pr.failedBuild = ev.FailedBuild
					pr.out = slices.Concat(builds[ev.FailedBuild], pr.out)
				}
			}
			continue
		}

		t, ok := pr.tests[ev.Test]
		if !ok {
			t = &testResult{}
			pr.tests[ev.Test] = t
		}
		switch ev.Action {
		case "run":
			p.testsRun++
		case "output":
			line := clean(ev.Output)
			t.out = append(t.out, line)
			p.fullLog = append(p.fullLog, line)
		case "pass", "fail", "skip":
			t.status = ev.Action
		}
	}
	return p, sc.Err()
}

// stage turns a parsed stream into a report.Stage. goExit is go test's exit
// code; narrowed means a filter was given, so zero tests is not a pass.
func (p *parsed) stage(name, module string, goExit int, stderr []string, narrowed bool) report.Stage {
	st := report.Stage{Name: name, Tests: p.testsRun}

	for _, pkg := range p.order {
		st.Failures = append(st.Failures, packageFailures(p.pkgs[pkg], module)...)
	}
	// go test failed but nothing in the stream says why: a bad pattern, a
	// module error, a crash before the first event. Never report that as green.
	if goExit != 0 && len(st.Failures) == 0 {
		lines := nonEmpty(slices.Concat(tail(p.fullLog, maxStderrLines), stderr))
		st.Failures = append(st.Failures, report.Failure{
			Category: "unclassified",
			Package:  "(go test)",
			Lines:    tail(lines, maxStderrLines),
			Total:    len(lines),
		})
	}

	for _, pr := range p.pkgs {
		if pr.status == "pass" && len(pr.tests) > 0 {
			st.Packages++
		}
	}

	switch {
	case len(st.Failures) > 0:
		st.Status = report.Failed
	case narrowed && (p.testsRun == 0 || ginkgoRanZero(p.fullLog)):
		st.Status = report.NoMatches
	default:
		st.Status = report.Passed
	}
	return st
}

// packageFailures reports leaf tests only: a failed subtest fails its parent,
// and reporting both would show the same failure twice.
func packageFailures(p *pkgResult, module string) []report.Failure {
	if p.status != "fail" {
		return nil
	}
	pkg := shortPkg(p.name, module)
	if p.failedBuild != "" {
		lines := nonEmpty(p.out)
		return []report.Failure{{Category: "build", Package: pkg, Lines: head(lines, maxPlainLines), Total: len(lines)}}
	}

	names := make([]string, 0, len(p.tests))
	for name, t := range p.tests {
		if t.status == "fail" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var out []report.Failure
	for _, name := range names {
		if hasFailedChild(p, name) {
			continue
		}
		out = append(out, classify(pkg, name, p.tests[name].out))
	}

	// Output outside any test: TestMain's goleak check, a panic in init, a
	// timeout. Report it when no test explains the failure, or when it has a
	// recognisable cause of its own.
	if pf := classify(pkg, "", p.out); len(out) == 0 || pf.Category != "unclassified" {
		out = append(out, pf)
	}
	return out
}

func hasFailedChild(p *pkgResult, name string) bool {
	prefix := name + "/"
	for other, t := range p.tests {
		if t.status == "fail" && strings.HasPrefix(other, prefix) {
			return true
		}
	}
	return false
}

func classify(pkg, test string, raw []string) report.Failure {
	lines := nonEmpty(dropRunnerNoise(raw))
	f := report.Failure{Package: pkg, Test: test, Total: len(lines)}

	switch {
	case containsAny(lines, "WARNING: DATA RACE"):
		f.Category = "race"
		f.Lines = blocks(lines, func(l string) bool { return strings.Contains(l, "WARNING: DATA RACE") },
			func(l string) bool { return strings.HasPrefix(l, "==================") })
	case containsAny(lines, "panic: test timed out"):
		f.Category = "timeout"
		f.Lines = from(lines, "panic: test timed out", maxPanicLines)
	case containsAny(lines, "found unexpected goroutines"):
		f.Category = "leak"
		f.Lines = from(lines, "found unexpected goroutines", maxLeakLines)
	case containsAny(lines, "[BeforeSuite] [FAILED]", "[SynchronizedBeforeSuite] [FAILED]", "[BeforeSuite] [PANICKED]"):
		f.Category = "setup"
		f.Lines = ginkgoExtract(lines)
	case containsAny(lines, "[PANICKED]", "panic: "):
		f.Category = "panic"
		if containsAny(lines, "[PANICKED]") {
			f.Lines = ginkgoExtract(lines)
		} else {
			f.Lines = from(lines, "panic: ", maxPanicLines)
		}
	case containsAny(lines, "missing call(s)", "Unexpected call to", "there are no expected calls"):
		f.Category = "mock"
		f.Lines = plainOrGinkgo(lines)
	case containsAny(lines, "[FAILED]", "--- FAIL", "Error Trace:"):
		f.Category = "assertion"
		f.Lines = plainOrGinkgo(lines)
	default:
		f.Category = "unclassified"
		f.Lines = tail(lines, maxPlainLines)
	}
	return f
}

func plainOrGinkgo(lines []string) []string {
	if containsAny(lines, "• [FAILED]", "• [PANICKED]") {
		return ginkgoExtract(lines)
	}
	return head(lines, maxPlainLines)
}

// ginkgoExtract keeps each failed spec's report — from its "• [FAILED]"
// header to the next separator — and Ginkgo's closing failure summary.
func ginkgoExtract(lines []string) []string {
	isStart := func(l string) bool {
		t := strings.TrimSpace(l)
		return strings.HasPrefix(t, "• [FAILED]") || strings.HasPrefix(t, "• [PANICKED]") ||
			strings.Contains(t, "Suite] [FAILED]") || strings.Contains(t, "Suite] [PANICKED]")
	}
	isEnd := func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "------------------------------") }

	out := blocks(lines, isStart, isEnd)
	if s := from(lines, "Summarizing ", 20); len(s) > 0 {
		out = append(out, "", "…")
		out = append(out, s...)
	}
	if len(out) == 0 {
		return head(lines, maxPlainLines)
	}
	return out
}

func blocks(lines []string, isStart, isEnd func(string) bool) []string {
	var out []string
	n := 0
	for i := 0; i < len(lines) && n < maxBlocks; i++ {
		if !isStart(lines[i]) {
			continue
		}
		if n > 0 {
			out = append(out, "", "…")
		}
		n++
		j := i
		for ; j < len(lines) && j-i < maxBlockLines; j++ {
			out = append(out, lines[j])
			if j > i && isEnd(lines[j]) {
				break
			}
		}
		i = j
	}
	return out
}

func from(lines []string, marker string, limit int) []string {
	for i, l := range lines {
		if strings.Contains(l, marker) {
			return head(lines[i:], limit)
		}
	}
	return nil
}

func dropRunnerNoise(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "=== PAUSE") ||
			strings.HasPrefix(t, "=== CONT") || strings.HasPrefix(t, "=== NAME") ||
			strings.HasPrefix(t, "--- PASS") || strings.HasPrefix(t, "--- SKIP") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func ginkgoRanZero(log []string) bool {
	ranSome := false
	for _, l := range log {
		if strings.Contains(l, "Ran ") && strings.Contains(l, " Spec") && !ginkgoZero.MatchString(l) {
			ranSome = true
		}
	}
	return !ranSome && containsAny(log, "Ran 0 of ")
}

func shortPkg(pkg, module string) string {
	if module == "" {
		return pkg
	}
	if s, ok := strings.CutPrefix(pkg, module+"/"); ok {
		return s
	}
	return pkg
}

func clean(s string) string {
	return strings.TrimRight(ansi.ReplaceAllString(strings.ReplaceAll(s, "\r\n", "\n"), ""), "\n")
}

func containsAny(lines []string, needles ...string) bool {
	for _, l := range lines {
		for _, n := range needles {
			if strings.Contains(l, n) {
				return true
			}
		}
	}
	return false
}

func nonEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func head(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[:n:n]
}

func tail(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
