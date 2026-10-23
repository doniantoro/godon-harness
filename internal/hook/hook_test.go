package hook

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPatchFiles(t *testing.T) {
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: internal/a.go",
		"@@",
		"-old",
		"+new",
		"*** Add File: internal/b.go",
		"+package internal",
		"*** Delete File: internal/c.go",
		"*** Update File: internal/d.go",
		"*** Move to: internal/e.go",
		"*** Update File: internal/a.go",
		"*** End Patch",
	}, "\n")

	got := PatchFiles(patch)
	want := []string{"internal/a.go", "internal/b.go", "internal/d.go", "internal/e.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PatchFiles = %v, want %v (deleted files left out, duplicates once)", got, want)
	}
}

func TestEditedFiles(t *testing.T) {
	cwd := filepath.FromSlash("/work/repo")
	tests := []struct {
		name    string
		payload string
		want    []string
	}{
		{
			name:    "Claude Write carries an absolute file_path",
			payload: `{"tool_name":"Write","tool_input":{"file_path":"/work/repo/main.go"}}`,
			want:    []string{filepath.FromSlash("/work/repo/main.go")},
		},
		{
			name:    "Codex apply_patch names files relative to cwd",
			payload: `{"tool_name":"apply_patch","cwd":"/work/repo","tool_input":{"command":"*** Begin Patch\n*** Update File: pkg/x.go\n*** End Patch"}}`,
			want:    []string{filepath.Join(cwd, "pkg", "x.go")},
		},
		{
			name:    "Codex apply_patch as an argv array",
			payload: `{"tool_name":"apply_patch","cwd":"/work/repo","tool_input":{"command":["apply_patch","*** Begin Patch\n*** Add File: y.go\n*** End Patch"]}}`,
			want:    []string{filepath.Join(cwd, "y.go")},
		},
		{
			name:    "an unknown tool input names no file",
			payload: `{"tool_name":"Bash","tool_input":{"command":"go test ./..."}}`,
			want:    nil,
		},
		{
			name:    "an empty payload names no file",
			payload: ``,
			want:    nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Read(strings.NewReader(tc.payload)).EditedFiles()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("EditedFiles = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadStopHookActive(t *testing.T) {
	if !Read(strings.NewReader(`{"stop_hook_active":true}`)).StopHookActive {
		t.Fatal("stop_hook_active was not read")
	}
}
