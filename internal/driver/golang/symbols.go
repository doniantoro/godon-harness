package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doniantoro/godon-harness/internal/driver"
)

// FindSymbols implements driver.Driver by parsing every Go file under the
// root. A declaration is syntax, so the parser is exact where a text search
// guesses: it sees an interface method or a struct field together with its
// owning type, and it has no result limit to silently truncate at.
func (Driver) FindSymbols(root string, q driver.SymbolQuery) ([]driver.Symbol, error) {
	start := root
	if q.Path != "" && q.Path != "." {
		start = filepath.Join(root, filepath.FromSlash(q.Path))
	}

	var found []driver.Symbol
	fset := token.NewFileSet()
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != start && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !q.IncludeTests && (strings.HasSuffix(rel, "_test.go") || isMockPath(rel)) {
			return nil
		}
		file, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil // a file that does not parse declares nothing we can trust
		}
		for _, s := range declarations(fset, file, q.Name) {
			if q.Owner != "" && s.Owner != q.Owner {
				continue
			}
			s.File = rel
			found = append(found, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		return found[i].Line < found[j].Line
	})
	return found, nil
}

func isMockPath(rel string) bool {
	return strings.Contains("/"+rel, "/mocks/")
}

// declarations lists every declaration of name in one file.
func declarations(fset *token.FileSet, file *ast.File, name string) []driver.Symbol {
	var out []driver.Symbol
	add := func(pos token.Pos, owner, kind string) {
		out = append(out, driver.Symbol{Line: fset.Position(pos).Line, Owner: owner, Name: name, Kind: kind})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name {
				continue
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				add(d.Name.Pos(), receiverType(d.Recv.List[0].Type), "method")
			} else {
				add(d.Name.Pos(), "", "func")
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						add(s.Name.Pos(), "", typeKind(s.Type))
					}
					members(s, name, add)
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, n := range s.Names {
						if n.Name == name {
							add(n.Pos(), "", kind)
						}
					}
				}
			}
		}
	}
	return out
}

// members finds name among a struct's fields or an interface's methods.
func members(s *ast.TypeSpec, name string, add func(token.Pos, string, string)) {
	var list *ast.FieldList
	kind := "field"
	switch t := s.Type.(type) {
	case *ast.StructType:
		list = t.Fields
	case *ast.InterfaceType:
		list, kind = t.Methods, "method"
	default:
		return
	}
	if list == nil {
		return
	}
	for _, f := range list.List {
		for _, n := range f.Names {
			if n.Name == name {
				add(n.Pos(), s.Name.Name, kind)
			}
		}
	}
}

func receiverType(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr: // generic receiver T[K]
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

func typeKind(e ast.Expr) string {
	switch e.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	default:
		return "type"
	}
}

// SymbolDoc implements driver.Driver with go doc: the signature and its
// comment, without reading the file.
func (Driver) SymbolDoc(ctx context.Context, root string, s driver.Symbol) (string, error) {
	dir := "./" + path.Dir(s.File)
	out, code, err := run(ctx, root, "go", "doc", "-u", dir, s.Qualified())
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if code != 0 {
		return "", fmt.Errorf("go doc %s %s: %s", dir, s.Qualified(), text)
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "package ") {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(head(lines, 25), "\n")), nil
}
