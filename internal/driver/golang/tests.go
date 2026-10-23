package golang

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/report"
)

const ginkgoImport = "github.com/onsi/ginkgo/v2"

// RunTests implements driver.Driver with go test -json.
func (Driver) RunTests(ctx context.Context, env driver.Env, spec driver.TestSpec) (report.Stage, error) {
	dir := filepath.Join(env.RunDir, spec.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return report.Stage{}, err
	}

	pkgs := spec.Packages
	ginkgoFlags := ginkgoArgs(spec)
	// -ginkgo.* flags reach every test binary, and one without Ginkgo rejects
	// them as unknown, so a filtered run keeps only the Ginkgo packages.
	if len(ginkgoFlags) > 0 {
		var err error
		if pkgs, err = ginkgoPackages(ctx, env.Root, spec); err != nil {
			return report.Stage{}, err
		}
	}

	args := []string{"test", "-json"}
	if spec.Race {
		args = append(args, "-race")
	}
	if len(spec.Tags) > 0 {
		args = append(args, "-tags="+strings.Join(spec.Tags, ","))
	}
	if spec.NoCache {
		args = append(args, "-count=1")
	}
	if spec.Timeout != "" {
		args = append(args, "-timeout="+spec.Timeout)
	}
	args = append(args, pkgs...)
	if spec.Run != "" {
		args = append(args, "-run", spec.Run)
	}
	if len(ginkgoFlags) > 0 {
		args = append(args, "-args")
		args = append(args, ginkgoFlags...)
	}

	eventsPath := filepath.Join(dir, "events.json")
	stderrPath := filepath.Join(dir, "stderr.log")
	code, err := goTest(ctx, env.Root, args, eventsPath, stderrPath)
	if err != nil {
		return report.Stage{}, err
	}

	events, err := os.Open(eventsPath)
	if err != nil {
		return report.Stage{}, err
	}
	defer events.Close()
	p, err := parseEvents(events)
	if err != nil {
		return report.Stage{}, fmt.Errorf("read %s: %w", eventsPath, err)
	}
	stderr := readLines(stderrPath)
	if err := writeFullLog(filepath.Join(dir, "full.log"), p.fullLog, stderr); err != nil {
		return report.Stage{}, err
	}

	st := p.stage(spec.Name, modulePath(env.Root), code, stderr, spec.Narrowed)
	st.Dir = dir
	return st, nil
}

func goTest(ctx context.Context, root string, args []string, eventsPath, stderrPath string) (int, error) {
	stdout, err := os.Create(eventsPath)
	if err != nil {
		return 0, err
	}
	defer stdout.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return 0, err
	}
	defer stderr.Close()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	default:
		return 0, fmt.Errorf("run go test: %w", err)
	}
}

func ginkgoArgs(spec driver.TestSpec) []string {
	var out []string
	if spec.Focus != "" {
		out = append(out, "-ginkgo.focus="+spec.Focus)
	}
	if spec.Label != "" {
		out = append(out, "-ginkgo.label-filter="+spec.Label)
	}
	return out
}

// ginkgoPackages expands the spec's patterns to the packages whose tests
// import Ginkgo.
func ginkgoPackages(ctx context.Context, root string, spec driver.TestSpec) ([]string, error) {
	args := []string{"list"}
	if len(spec.Tags) > 0 {
		args = append(args, "-tags="+strings.Join(spec.Tags, ","))
	}
	args = append(args, "-f", `{{.ImportPath}} {{join .TestImports " "}} {{join .XTestImports " "}}`)
	args = append(args, spec.Packages...)
	out, code, err := run(ctx, root, "go", args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("go list %s: %s", strings.Join(spec.Packages, " "), strings.TrimSpace(string(out)))
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for _, imp := range fields[min(1, len(fields)):] {
			if imp == ginkgoImport {
				pkgs = append(pkgs, fields[0])
				break
			}
		}
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("-focus/-label need a Ginkgo suite, and none of %s imports %s", strings.Join(spec.Packages, " "), ginkgoImport)
	}
	return pkgs, nil
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(clean(string(b)), "\n"), "\n")
}

func writeFullLog(path string, log, stderr []string) error {
	var sb strings.Builder
	for _, l := range log {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	if len(stderr) > 0 {
		sb.WriteString("\n--- stderr ---\n")
		for _, l := range stderr {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
