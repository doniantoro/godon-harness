package main

import (
	"fmt"
	"io"
	"os"

	"github.com/doniantoro/godon-harness/internal/config"
	"github.com/doniantoro/godon-harness/internal/driver"
	"github.com/doniantoro/godon-harness/internal/output"
	"github.com/doniantoro/godon-harness/internal/repo"
)

// workspace is the repository a command works on.
type workspace struct {
	root   string
	name   string
	driver driver.Driver
	config config.Config
}

// openWorkspace resolves the repository from dir and picks its driver.
func openWorkspace(dir string, stderr io.Writer) (*workspace, bool) {
	root, err := repo.Root(dir)
	if err != nil {
		fmt.Fprintln(stderr, "godon:", err)
		return nil, false
	}
	d, ok := driver.Detect(root, drivers)
	if !ok {
		fmt.Fprintf(stderr, "godon: no supported language in %s (looked for go.mod)\n", root)
		return nil, false
	}
	return &workspace{root: root, name: repo.Name(root), driver: d, config: config.Default()}, true
}

// env creates a run directory of the given kind and returns the driver Env.
func (w *workspace) env(kind string) (driver.Env, error) {
	r, err := output.NewRun(w.name, kind)
	if err != nil {
		return driver.Env{}, err
	}
	cache, err := output.CacheDir(w.name)
	if err != nil {
		return driver.Env{}, err
	}
	return driver.Env{Root: w.root, RunDir: r.Dir, CacheDir: cache}, nil
}

func cwd() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "."
}
