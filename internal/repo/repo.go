// Package repo locates the repository godon is working on.
package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotFound means no repository root could be found from the start directory.
var ErrNotFound = errors.New("no git repository or go.mod found")

// Root returns the repository root for dir: the git top level when dir is
// inside a git work tree, otherwise the nearest ancestor holding a go.mod.
func Root(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = abs
	if out, err := cmd.Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Clean(top), nil
		}
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", ErrNotFound
		}
	}
}

// Name is the repository's directory name, used to keep each repository's
// output apart.
func Name(root string) string {
	return filepath.Base(root)
}
