package golang

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StopCheck implements driver.Driver: go vet over the packages that hold a
// changed file. vet compiles the package and its tests, so it catches what
// would break the build as well as vet's own findings, and it stays fast
// because it never looks at the rest of the repository.
func (Driver) StopCheck(ctx context.Context, root string, changed []string) (string, error) {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range changed {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			continue // deleted
		}
		dir := "./" + slashDir(f)
		if dir == "./." {
			dir = "."
		}
		if !seen[dir] {
			seen[dir] = true
			pkgs = append(pkgs, dir)
		}
	}
	if len(pkgs) == 0 {
		return "", nil
	}
	sort.Strings(pkgs)

	out, code, err := run(ctx, root, "go", append([]string{"vet"}, pkgs...)...)
	if err != nil {
		return "", err
	}
	if code == 0 {
		return "", nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasPrefix(l, "# ") {
			lines = append(lines, l)
		}
	}
	return "go vet failed on the changed packages:\n" + strings.Join(head(lines, 15), "\n"), nil
}
