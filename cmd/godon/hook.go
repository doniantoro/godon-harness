package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/hook"
	"github.com/doniantoro/godon-harness/internal/output"
)

// exitBlock is the hook protocol's "blocking" code in Claude Code and Codex:
// stderr goes back to the agent.
const exitBlock = 2

// cmdHook runs as an agent hook. A hook must never break the agent's flow
// because of godon's own trouble — a missing repository, an unknown
// language, a git error — so every such case exits 0 silently. Only a real
// finding about the agent's work blocks.
func cmdHook(ctx context.Context, args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "godon hook: format|stop|session-start")
		return exitUsage
	}
	in := hook.Read(stdin)

	switch args[0] {
	case "session-start":
		// Reaching here means the launcher has the binary; that is the job.
		return exitOK
	case "format":
		return hookFormat(ctx, in, stderr)
	case "stop":
		return hookStop(ctx, in, stderr)
	default:
		fmt.Fprintf(stderr, "godon hook: unknown hook %q\n", args[0])
		return exitUsage
	}
}

func hookWorkspace(in hook.Input) (*workspace, bool) {
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		dir = in.Cwd
	}
	if dir == "" {
		dir = cwd()
	}
	return openWorkspace(dir, io.Discard)
}

// hookFormat formats each file the edit wrote. A file the formatter cannot
// parse is reported, so the agent learns at once that its edit broke syntax.
func hookFormat(ctx context.Context, in hook.Input, stderr io.Writer) int {
	files := in.EditedFiles()
	if len(files) == 0 {
		return exitOK
	}
	w, ok := hookWorkspace(in)
	if !ok {
		return exitOK
	}
	var broken []string
	for _, f := range files {
		if !w.driver.Handles(f) || isGeneratedDir(f) {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			continue
		}
		err := w.driver.Format(ctx, w.root, f)
		var pe *driver.ParseError
		switch {
		case errors.As(err, &pe):
			broken = append(broken, pe.Error())
		case err != nil:
			// The formatter itself failed to run: not the agent's problem.
		}
	}
	if len(broken) == 0 {
		return exitOK
	}
	fmt.Fprintf(stderr, "godon: the edit left a file that does not parse:\n%s\n", strings.Join(broken, "\n"))
	return exitBlock
}

func isGeneratedDir(f string) bool {
	slash := "/" + filepath.ToSlash(f)
	return strings.Contains(slash, "/vendor/")
}

// hookStop runs the driver's quick gate before the agent ends its turn — only
// when the files the driver handles changed since the last passing gate, so
// a question or a documentation edit costs nothing.
func hookStop(ctx context.Context, in hook.Input, stderr io.Writer) int {
	// Already continuing because of this hook: do not block again, or a
	// failure the agent cannot fix would loop. The user sees it instead.
	if in.StopHookActive {
		return exitOK
	}
	w, ok := hookWorkspace(in)
	if !ok {
		return exitOK
	}
	changes, ok := hook.GitChanges(w.root, w.driver.Handles)
	if !ok || len(changes.Files) == 0 {
		return exitOK
	}
	cache, err := output.CacheDir(w.name)
	if err != nil {
		return exitOK
	}
	stamp := filepath.Join(cache, "stop.ok")
	if prev, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(prev)) == changes.Fingerprint {
		return exitOK
	}

	problem, err := w.driver.StopCheck(ctx, w.root, changes.Files)
	if err != nil {
		return exitOK
	}
	if problem == "" {
		_ = os.WriteFile(stamp, []byte(changes.Fingerprint), 0o644) // a missing stamp only means checking again
		return exitOK
	}
	fmt.Fprintf(stderr, "godon: fix this before finishing:\n%s\n", problem)
	return exitBlock
}
