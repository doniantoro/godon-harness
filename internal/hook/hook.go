// Package hook reads what Claude Code and Codex send a hook on stdin. The two
// agree on the envelope and differ in how an edit names its file: Claude's
// Edit/Write carry tool_input.file_path, Codex's apply_patch carries the
// patch text, whose file headers name the paths.
package hook

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

// Input is the subset of a hook payload godon uses.
type Input struct {
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	Cwd            string          `json:"cwd"`
	StopHookActive bool            `json:"stop_hook_active"`
}

// Read decodes a payload. An empty or malformed payload is an empty Input:
// a hook must never fail because of its own input.
func Read(r io.Reader) Input {
	var in Input
	b, err := io.ReadAll(r)
	if err != nil || len(b) == 0 {
		return in
	}
	_ = json.Unmarshal(b, &in) // tolerated: see the doc comment
	return in
}

// EditedFiles returns the absolute paths an edit tool wrote.
func (in Input) EditedFiles() []string {
	var ti struct {
		FilePath string          `json:"file_path"`
		Command  json.RawMessage `json:"command"`
		Input    string          `json:"input"`
	}
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &ti) // a shape we do not know names no file
	}
	if ti.FilePath != "" {
		return []string{in.abs(ti.FilePath)}
	}

	patch := ti.Input
	if patch == "" {
		patch = commandText(ti.Command)
	}
	var out []string
	for _, p := range PatchFiles(patch) {
		out = append(out, in.abs(p))
	}
	return out
}

// commandText accepts tool_input.command as a string or an argv array.
func commandText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(raw, &argv) == nil {
		return strings.Join(argv, "\n")
	}
	return ""
}

func (in Input) abs(p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) || in.Cwd == "" {
		return p
	}
	return filepath.Join(in.Cwd, p)
}

// PatchFiles lists the files an apply_patch body adds, updates or moves to.
// Deleted files are left out: there is nothing left to format.
func PatchFiles(patch string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				p = strings.TrimSpace(p)
				if p != "" && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}
