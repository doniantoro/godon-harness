// Package golang is godon's Go driver: go test -json, a go/parser symbol
// index, gofmt/goimports, go vet, mockgen freshness and golangci-lint.
package golang

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
)

// Driver is the Go driver. It has no state.
type Driver struct{}

var _ driver.Driver = Driver{}

// New returns the Go driver.
func New() Driver { return Driver{} }

// Name implements driver.Driver.
func (Driver) Name() string { return "go" }

// Detect implements driver.Driver: a go.mod at the root.
func (Driver) Detect(root string) bool {
	_, err := os.Stat(filepath.Join(root, "go.mod"))
	return err == nil
}

// Handles implements driver.Driver.
func (Driver) Handles(path string) bool { return strings.HasSuffix(path, ".go") }

// modulePath reads the module line of root/go.mod.
func modulePath(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// requires reports whether root/go.mod requires the module path.
func requires(root, module string) bool {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "require "))
		if len(fields) > 0 && fields[0] == module {
			return true
		}
	}
	return false
}

// run executes a command in dir and returns its combined output and exit
// code. A command that could not start at all is an error.
func run(ctx context.Context, dir string, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return buf.Bytes(), 0, nil
	case errors.As(err, &exit):
		return buf.Bytes(), exit.ExitCode(), nil
	default:
		return buf.Bytes(), -1, err
	}
}

// exe appends the platform's executable suffix.
func exe(name string) string {
	if filepath.Separator == '\\' {
		return name + ".exe"
	}
	return name
}

// skipDir reports directories no Go walk should enter.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" || name == "node_modules" ||
		(len(name) > 1 && (name[0] == '.' || name[0] == '_'))
}
