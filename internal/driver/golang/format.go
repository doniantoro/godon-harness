package golang

import (
	"bytes"
	"context"
	"go/format"
	"os"
	"os/exec"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
)

// Format implements driver.Driver. goimports is used when it is installed,
// so imports are fixed as well; otherwise the file is formatted in-process,
// which needs no external tool on any platform. Generated files are left
// alone — they are regenerated, not edited.
func (Driver) Format(ctx context.Context, root, file string) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if isGenerated(src) {
		return nil
	}

	if goimports, err := exec.LookPath("goimports"); err == nil {
		args := []string{"-w"}
		if m := modulePath(root); m != "" {
			args = append(args, "-local", m)
		}
		out, code, err := run(ctx, root, goimports, append(args, file)...)
		if err != nil {
			return err
		}
		if code != 0 {
			return &driver.ParseError{Path: file, Detail: firstLines(string(out), 5)}
		}
		return nil
	}

	formatted, err := format.Source(src)
	if err != nil {
		return &driver.ParseError{Path: file, Detail: firstLines(err.Error(), 5)}
	}
	if bytes.Equal(formatted, src) {
		return nil
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, formatted, info.Mode().Perm())
}

// isGenerated follows the Go convention: a "Code generated ... DO NOT EDIT."
// line before the package clause.
func isGenerated(src []byte) bool {
	for _, line := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "package ") {
			return false
		}
		if strings.HasPrefix(t, "// Code generated ") && strings.HasSuffix(t, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(head(lines, n), "\n")
}
