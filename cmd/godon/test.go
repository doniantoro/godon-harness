package main

import (
	"context"
	"fmt"
	"io"

	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/report"
)

const testUsage = `godon test — run tests and print a short summary

  godon test <packages...> [-run REGEX] [-focus REGEX] [-label EXPR] [--with-all]
      the tests being worked on; --with-all then runs every unit test, only if they pass
  godon test --all
      every unit test
  godon test --integration [packages...] [-run REGEX] [-focus REGEX] [-label EXPR]
      the integration-tagged suite (can take long — start it in the background)

  -run REGEX    test-function filter (go test -run)
  -focus REGEX  Ginkgo spec filter (-ginkgo.focus); only Ginkgo packages run
  -label EXPR   Ginkgo label filter (-ginkgo.label-filter); only Ginkgo packages run
  --race        force the race detector on   --no-race  force it off

Each stage writes summary.txt, failures/NN-<category>-<pkg>.txt, full.log and
events.json. Read the printed summary first, then only the failure files
that matter, and full.log (with a search, not whole) only if those are not
enough.

Exit: 0 passed, 1 failed, 2 usage, 3 a filter matched no test, 4 tool error.
`

type testArgs struct {
	pkgs                 []string
	run, focus, label    string
	withAll, all, integr bool
	race                 *bool
}

func parseTestArgs(args []string) (testArgs, error) {
	var t testArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "-run", "--run":
			t.run, err = value()
		case "-focus", "--focus":
			t.focus, err = value()
		case "-label", "--label":
			t.label, err = value()
		case "--with-all":
			t.withAll = true
		case "--all":
			t.all = true
		case "--integration":
			t.integr = true
		case "--race":
			on := true
			t.race = &on
		case "--no-race":
			off := false
			t.race = &off
		default:
			if len(a) > 0 && a[0] == '-' {
				return t, fmt.Errorf("unknown flag %s", a)
			}
			t.pkgs = append(t.pkgs, a)
		}
		if err != nil {
			return t, err
		}
	}

	switch {
	case t.integr && (t.all || t.withAll):
		return t, fmt.Errorf("--integration cannot be combined with --all or --with-all")
	case t.all && (t.withAll || len(t.pkgs) > 0 || t.run != "" || t.focus != "" || t.label != ""):
		return t, fmt.Errorf("--all takes no packages or filters")
	case !t.all && !t.integr && len(t.pkgs) == 0:
		return t, fmt.Errorf("name the package(s) under change, or use --all")
	}
	return t, nil
}

func cmdTest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, testUsage)
		return exitOK
	}
	t, err := parseTestArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "godon test: %v\n\n%s", err, testUsage)
		return exitUsage
	}

	w, ok := openWorkspace(cwd(), stderr)
	if !ok {
		return exitUsage
	}
	env, err := w.env("run")
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}

	race := w.config.Test.Race
	if t.race != nil {
		race = *t.race
	}
	narrowed := t.run != "" || t.focus != "" || t.label != ""

	var stages []driver.TestSpec
	switch {
	case t.integr:
		pkgs := t.pkgs
		if len(pkgs) == 0 {
			pkgs = w.config.Integration.Packages
		}
		stages = append(stages, driver.TestSpec{
			Name: "integration", Packages: pkgs, Run: t.run, Focus: t.focus, Label: t.label,
			Tags: w.config.Integration.Tags, Timeout: w.config.Integration.Timeout,
			NoCache: true, Narrowed: narrowed, Race: t.race != nil && *t.race,
		})
	case t.all:
		stages = append(stages, driver.TestSpec{Name: "all", Packages: []string{"./..."}, Race: race})
	default:
		stages = append(stages, driver.TestSpec{
			Name: "focused", Packages: t.pkgs, Run: t.run, Focus: t.focus, Label: t.label,
			Race: race, Narrowed: narrowed,
		})
		if t.withAll {
			stages = append(stages, driver.TestSpec{Name: "all", Packages: []string{"./..."}, Race: race})
		}
	}

	code := exitOK
	for i, spec := range stages {
		st, err := w.driver.RunTests(ctx, env, spec)
		if err != nil {
			fmt.Fprintln(stderr, "godon:", err)
			return exitToolError
		}
		summary, err := st.Write()
		if err != nil {
			fmt.Fprintln(stderr, "godon:", err)
			return exitToolError
		}
		fmt.Fprint(stdout, summary)

		switch st.Status {
		case report.Failed:
			code = exitFail
		case report.NoMatches:
			code = exitNoTests
		}
		if code != exitOK {
			if i < len(stages)-1 {
				fmt.Fprintln(stdout, "stopped before the full suite.")
			}
			break
		}
	}
	fmt.Fprintln(stdout, "run dir:", env.RunDir)
	return code
}
