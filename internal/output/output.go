// Package output owns the directories godon writes results into. Everything
// lives under the OS temporary directory, one tree per repository, so results
// never land in the working tree and two repositories never mix.
package output

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keep is how many runs of each kind survive pruning.
const keep = 10

// Base is the per-repository output directory. GODON_OUT overrides the
// location; the repository name is always appended.
func Base(repoName string) string {
	root := os.Getenv("GODON_OUT")
	if root == "" {
		root = filepath.Join(os.TempDir(), "godon")
	}
	return filepath.Join(root, repoName)
}

// Run is one invocation's directory: base/<kind>-<timestamp>-<pid>.
type Run struct {
	Dir string
}

// NewRun creates a run directory of the given kind ("run", "check"), records
// it in base/latest-<kind>.txt — a file rather than a symlink, which Windows
// does not reliably allow — and prunes older runs of the same kind.
func NewRun(repoName, kind string) (*Run, error) {
	base := Base(repoName)
	name := fmt.Sprintf("%s-%s-%d", kind, time.Now().Format("20060102-150405"), os.Getpid())
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(base, "latest-"+kind+".txt"), []byte(dir+"\n"), 0o644); err != nil {
		return nil, err
	}
	prune(base, kind+"-")
	return &Run{Dir: dir}, nil
}

// Stage returns (and creates) a subdirectory of the run.
func (r *Run) Stage(name string) (string, error) {
	dir := filepath.Join(r.Dir, name)
	return dir, os.MkdirAll(dir, 0o755)
}

func prune(base, prefix string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	var runs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			runs = append(runs, e.Name())
		}
	}
	// Names embed a sortable timestamp, so lexical order is age order.
	sort.Sort(sort.Reverse(sort.StringSlice(runs)))
	for _, old := range runs[min(len(runs), keep):] {
		_ = os.RemoveAll(filepath.Join(base, old)) // best effort: a stale run is harmless
	}
}

// CacheDir is where godon keeps tools it builds (mockgen) and hook state,
// shared by every run of the repository.
func CacheDir(repoName string) (string, error) {
	dir := filepath.Join(Base(repoName), "cache")
	return dir, os.MkdirAll(dir, 0o755)
}
