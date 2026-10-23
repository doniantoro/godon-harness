package hook

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Changes describes the uncommitted state of the files a driver handles.
type Changes struct {
	Files       []string // slash-separated, relative to the root
	Fingerprint string   // changes when any of those files' content changes
}

// GitChanges lists tracked-and-modified plus untracked files under root that
// keep reports true for, and fingerprints their content. Outside a git work
// tree it returns ok=false: there is no baseline to compare against.
func GitChanges(root string, keep func(string) bool) (Changes, bool) {
	tracked, ok := git(root, "diff", "--name-only", "HEAD")
	if !ok {
		return Changes{}, false
	}
	untracked, ok := git(root, "ls-files", "--others", "--exclude-standard")
	if !ok {
		return Changes{}, false
	}

	h := sha256.New()
	var files []string
	for _, f := range append(lines(tracked), lines(untracked)...) {
		if !keep(f) {
			continue
		}
		files = append(files, f)
		h.Write([]byte(f + "\x00")) // hash.Hash.Write never returns an error
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			h.Write(b)
		} else {
			h.Write([]byte("\x00deleted"))
		}
	}
	return Changes{Files: files, Fingerprint: hex.EncodeToString(h.Sum(nil))}, true
}

func git(root string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	return string(out), err == nil
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
