// Package diff reads a repository's changes from git and splits them per
// file, so a caller can print a short summary and only the patches worth
// reading. It knows nothing about any one language: generated files are
// recognised by the "Code generated ... DO NOT EDIT" convention, lock files
// and vendored trees by their names.
package diff

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// emptyTree is git's well-known hash of the empty tree, the baseline before
// the first commit.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Kind says whether a file's patch is worth reading.
type Kind int

const (
	Source    Kind = iota // written by hand
	Generated             // carries a "Code generated ... DO NOT EDIT" header
	Lock                  // a dependency lock file such as go.sum
	Vendor                // a vendored or installed dependency tree
)

func (k Kind) String() string {
	switch k {
	case Generated:
		return "generated"
	case Lock:
		return "lock"
	case Vendor:
		return "vendor"
	default:
		return "source"
	}
}

// File is one changed file.
type File struct {
	Path    string // slash-separated, relative to the root; the new name of a rename
	OldPath string // set for a rename
	Status  string // M modified, A added, D deleted, R renamed, ? untracked
	Added   int
	Deleted int
	Binary  bool
	Kind    Kind
	Patch   string // this file's part of the unified diff
}

// Lines is the patch length, the cost of printing it.
func (f File) Lines() int {
	return strings.Count(f.Patch, "\n")
}

// Options selects what to compare.
type Options struct {
	Staged bool     // the index against HEAD (or Rev), like git diff --cached
	Rev    string   // a revision or range (main, main...HEAD); default HEAD
	Paths  []string // limit to these paths, relative to the root
}

// Collect returns the changed files under root. Without Staged or a range,
// untracked files count as added, since they are part of the change too.
func Collect(root string, o Options) ([]File, error) {
	args := []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"-M", "--src-prefix=a/", "--dst-prefix=b/"}
	if o.Staged {
		args = append(args, "--cached")
	}
	switch {
	case o.Rev != "":
		args = append(args, o.Rev)
	case !o.Staged:
		base := "HEAD"
		if _, err := git(root, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
			base = emptyTree
		}
		args = append(args, base)
	}
	args = append(args, "--")
	args = append(args, o.Paths...)

	out, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	files := parse(out)
	for i := range files {
		files[i].Kind = classify(files[i].Path, head(root, files[i]))
	}

	if !o.Staged && !strings.Contains(o.Rev, "..") {
		untracked, err := untracked(root, o.Paths)
		if err != nil {
			return nil, err
		}
		files = append(files, untracked...)
	}
	return files, nil
}

// parse splits a unified diff into files.
func parse(out string) []File {
	var files []File
	for _, section := range splitSections(out) {
		f := File{Status: "M", Patch: section}
		var minus, plus, renameTo, gitLine string
		inHunk := false
		for _, l := range strings.Split(strings.TrimSuffix(section, "\n"), "\n") {
			if inHunk {
				switch {
				case strings.HasPrefix(l, "+"):
					f.Added++
				case strings.HasPrefix(l, "-"):
					f.Deleted++
				}
				continue
			}
			switch {
			case strings.HasPrefix(l, "diff --git "):
				gitLine = strings.TrimPrefix(l, "diff --git ")
			case strings.HasPrefix(l, "new file mode"):
				f.Status = "A"
			case strings.HasPrefix(l, "deleted file mode"):
				f.Status = "D"
			case strings.HasPrefix(l, "rename from "):
				f.Status, f.OldPath = "R", unquote(strings.TrimPrefix(l, "rename from "))
			case strings.HasPrefix(l, "rename to "):
				renameTo = unquote(strings.TrimPrefix(l, "rename to "))
			case strings.HasPrefix(l, "Binary files "):
				f.Binary = true
			case strings.HasPrefix(l, "--- "):
				minus = headerPath(l, "a/")
			case strings.HasPrefix(l, "+++ "):
				plus = headerPath(l, "b/")
			case strings.HasPrefix(l, "@@"):
				inHunk = true
			}
		}
		switch {
		case plus != "":
			f.Path = plus
		case renameTo != "":
			f.Path = renameTo
		case minus != "":
			f.Path = minus
		default:
			f.Path = gitLinePath(gitLine)
		}
		files = append(files, f)
	}
	return files
}

// splitSections cuts the diff at each "diff --git" line.
func splitSections(out string) []string {
	var sections []string
	start := -1
	for i := 0; i < len(out); {
		next := strings.IndexByte(out[i:], '\n')
		if next < 0 {
			next = len(out) - i
		} else {
			next++
		}
		if strings.HasPrefix(out[i:], "diff --git ") {
			if start >= 0 {
				sections = append(sections, out[start:i])
			}
			start = i
		}
		i += next
	}
	if start >= 0 {
		sections = append(sections, out[start:])
	}
	return sections
}

// headerPath reads the path from a "--- a/x" or "+++ b/x" line; /dev/null is
// no path. git appends a tab after a name that contains a space.
func headerPath(line, prefix string) string {
	p := unquote(strings.TrimRight(line[4:], "\t"))
	if p == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(p, prefix)
}

// gitLinePath takes the new name from "a/x b/x", which is all a binary or
// mode-only change carries. Both halves are the same length when unrenamed.
func gitLinePath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if i := strings.Index(s, `" `); i >= 0 {
			return strings.TrimPrefix(unquote(s[i+2:]), "b/")
		}
	}
	if half := (len(s) - 1) / 2; len(s) > 1 && len(s)%2 == 1 && s[half] == ' ' {
		return strings.TrimPrefix(s[half+1:], "b/")
	}
	if i := strings.Index(s, " b/"); i >= 0 {
		return s[i+3:]
	}
	return s
}

func unquote(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// untracked lists files git does not know yet, each as an added-file patch.
func untracked(root string, paths []string) ([]File, error) {
	out, err := git(root, append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, p := range strings.Split(out, "\x00") {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue // removed between listing and reading
		}
		f := File{Path: p, Status: "?", Kind: classify(p, b)}
		if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			f.Binary = true
			f.Patch = fmt.Sprintf("diff --git a/%s b/%s\nnew file (untracked)\nBinary file %s\n", p, p, p)
			files = append(files, f)
			continue
		}
		body := strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		var sb strings.Builder
		fmt.Fprintf(&sb, "diff --git a/%s b/%s\nnew file (untracked)\n--- /dev/null\n+++ b/%s\n", p, p, p)
		if body != "" {
			lines := strings.Split(body, "\n")
			f.Added = len(lines)
			fmt.Fprintf(&sb, "@@ -0,0 +1,%d @@\n", len(lines))
			for _, l := range lines {
				sb.WriteString("+" + l + "\n")
			}
		}
		f.Patch = sb.String()
		files = append(files, f)
	}
	return files, nil
}

// head is the start of a file's content, for classify: the working-tree copy
// when there is one, otherwise the lines the patch removed.
func head(root string, f File) []byte {
	if fh, err := os.Open(filepath.Join(root, filepath.FromSlash(f.Path))); err == nil {
		defer fh.Close()
		buf := make([]byte, 4096)
		n, _ := fh.Read(buf) // a short or failed read only weakens the guess
		return buf[:n]
	}
	var sb strings.Builder
	for _, l := range strings.Split(f.Patch, "\n") {
		if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			sb.WriteString(l[1:] + "\n")
		}
	}
	return []byte(sb.String())
}

var lockFiles = map[string]bool{
	"go.sum": true, "go.work.sum": true,
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true,
	"Cargo.lock": true, "poetry.lock": true, "uv.lock": true, "Pipfile.lock": true,
	"composer.lock": true, "Gemfile.lock": true, "mix.lock": true, "pubspec.lock": true,
}

func classify(p string, content []byte) Kind {
	switch {
	case strings.HasPrefix(p, "vendor/") || strings.Contains("/"+p, "/node_modules/"):
		return Vendor
	case lockFiles[path.Base(p)]:
		return Lock
	}
	// Only the comment block a file opens with counts: a doc comment or a
	// test fixture that merely mentions the convention is not the marker.
	lines := strings.SplitN(string(content), "\n", 41)
	for _, l := range lines[:min(len(lines), 40)] {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		text, ok := commentText(t)
		if !ok {
			break
		}
		if generatedMarker.MatchString(text) {
			return Generated
		}
	}
	return Source
}

// generatedMarker is the Go convention (https://go.dev/s/generatedcode),
// with swag's "Package docs Code generated ..." and the @generated tag other
// ecosystems use.
var generatedMarker = regexp.MustCompile(`^((Package \S+ )?Code generated .*DO NOT EDIT\.?$|@generated\b)`)

func commentText(line string) (string, bool) {
	for _, p := range []string{"//", "/*", "#", "--", "*"} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(strings.TrimPrefix(line, p)), true
		}
	}
	return "", false
}

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("git: %w", err)
	}
	return string(out), nil
}
