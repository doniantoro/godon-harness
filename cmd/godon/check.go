package main

import (
	"context"
	"fmt"
	"io"

	"github.com/doniantoro/godon-harness/internal/driver"
)

const checkUsage = `godon check — static checks, one line each

  godon check [--no-lint]

  Go: gofmt, go vet, mock freshness (//go:generate mockgen), golangci-lint
  when installed. Nothing in the working tree is modified. A check that does
  not pass names a detail file; open only those.

Exit: 0 every check passed or was skipped, 1 at least one failed, 2 usage.
`

func cmdCheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts := driver.CheckOptions{Lint: true}
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, checkUsage)
			return exitOK
		case "--no-lint":
			opts.Lint = false
		default:
			fmt.Fprintf(stderr, "godon check: unknown argument %s\n\n%s", a, checkUsage)
			return exitUsage
		}
	}

	w, ok := openWorkspace(cwd(), stderr)
	if !ok {
		return exitUsage
	}
	env, err := w.env("check")
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}
	checks, err := w.driver.Checks(ctx, env, opts)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return exitToolError
	}
	code := exitOK
	for _, c := range checks {
		fmt.Fprintln(stdout, c.Line())
		if c.Failing() {
			code = exitFail
		}
	}
	return code
}
