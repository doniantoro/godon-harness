// Command godon is the harness an AI coding agent (Claude Code, Codex) calls
// instead of raw tool commands. Each subcommand prints a few lines and writes
// the detail to files under the OS temporary directory, so the agent reads
// only what it needs.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/driver/golang"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes shared by the subcommands.
const (
	exitOK        = 0
	exitFail      = 1
	exitUsage     = 2
	exitNoTests   = 3
	exitNoChanges = 3 // godon diff: nothing changed
	exitToolError = 4
)

// drivers in detection order. A new language is one more entry.
var drivers = []driver.Driver{golang.New()}

const usage = `godon — a harness for AI coding agents

Usage:
  godon test <packages...> [-run REGEX] [-focus REGEX] [-label EXPR] [--with-all] [--race|--no-race]
  godon test --all
  godon test --integration [packages...] [-run REGEX] [-focus REGEX] [-label EXPR]
  godon symbol <Name|Type.Name> [path] [max] [--doc] [--tests]
  godon context <Name|Type.Name> [path]
  godon check [--no-lint]
  godon diff [--staged] [rev] [paths...]
  godon hook format|stop|session-start      (called by agent hooks, reads stdin)
  godon version

Run "godon <command> -h" for details.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "test":
		return cmdTest(ctx, args[1:], stdout, stderr)
	case "symbol":
		return cmdSymbol(ctx, args[1:], stdout, stderr)
	case "context":
		return cmdContext(ctx, args[1:], stdout, stderr)
	case "check":
		return cmdCheck(ctx, args[1:], stdout, stderr)
	case "diff":
		return cmdDiff(ctx, args[1:], stdout, stderr)
	case "hook":
		return cmdHook(ctx, args[1:], stdin, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, "godon", version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "godon: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}
