package golang

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	"github.com/doniantoro/godon-harness/internal/driver"
)

// Reads caps: the definition, then this many callers and dependencies.
const (
	readCallers = 3
	readDeps    = 4
)

// ginkgoNodes are the calls whose first argument names a spec.
var ginkgoNodes = map[string]bool{
	"Describe": true, "FDescribe": true, "PDescribe": true, "XDescribe": true,
	"Context": true, "FContext": true, "PContext": true, "XContext": true,
	"When": true, "FWhen": true, "PWhen": true, "XWhen": true,
	"It": true, "FIt": true, "PIt": true, "XIt": true, "Specify": true,
	"DescribeTable": true, "FDescribeTable": true, "Entry": true, "FEntry": true,
}

// Context implements driver.Driver. The declaration is found with the
// parser, as godon symbol does; everything else comes from type-checking the
// whole module, so a call through a consumer-declared interface is traced to
// the method behind it rather than missed.
func (d Driver) Context(ctx context.Context, root string, q driver.ContextQuery) (driver.SymbolContext, error) {
	var out driver.SymbolContext
	decls, err := d.FindSymbols(root, driver.SymbolQuery{Name: q.Name, Owner: q.Owner, Path: q.Path})
	if err != nil || len(decls) == 0 {
		return out, err
	}

	m, err := loadModule(ctx, root)
	if err != nil {
		return out, err
	}
	out.Warnings = m.warnings()

	type candidate struct {
		sym driver.Symbol
		obj types.Object
	}
	var cands []candidate
	for _, s := range decls {
		cands = append(cands, candidate{s, m.defAt(s.File, s.Line, s.Name)})
	}
	// An interface method and the method implementing it share a name; the
	// implementation is the one worth reading, and its context lists the
	// interface anyway.
	if len(cands) > 1 {
		var concrete []candidate
		for _, c := range cands {
			if !isInterfaceMethod(c.obj) {
				concrete = append(concrete, c)
			}
		}
		if len(concrete) > 0 {
			cands = concrete
		}
	}
	if len(cands) != 1 || cands[0].obj == nil {
		for _, c := range cands {
			out.Candidates = append(out.Candidates, c.sym)
		}
		return out, nil
	}

	target := cands[0].obj
	out.Found = true
	out.Symbol = cands[0].sym
	declNode, declLoc := m.declOf(target)
	out.Loc = declLoc
	out.Signature = m.signature(declNode, target)

	// via maps an interface method this method satisfies to its interface, so
	// a call through the interface counts as a call to the method.
	via := map[string]string{}
	switch obj := target.(type) {
	case *types.Func:
		if named := recvNamed(obj); named != nil && !types.IsInterface(named) {
			for _, it := range m.interfaces() {
				if !implements(named, it.iface) {
					continue
				}
				if mobj := methodByName(it.iface, obj.Name()); mobj != nil {
					name := qualified(mobj)
					out.Implements = append(out.Implements, driver.Ref{Name: name, Loc: m.lineOf(mobj.Pos())})
					via[m.key(mobj.Pos())] = qualified(it.obj)
				}
			}
		}
	case *types.TypeName:
		named, _ := obj.Type().(*types.Named)
		if named == nil {
			break
		}
		if iface, ok := named.Underlying().(*types.Interface); ok {
			for i := range iface.NumMethods() {
				mm := iface.Method(i)
				out.Methods = append(out.Methods, driver.Ref{Name: qualifiedIn(obj, mm), Loc: m.lineOf(mm.Pos())})
			}
			for _, tn := range m.concreteTypes() {
				if implements(tn.Type().(*types.Named), iface) {
					out.Implementations = append(out.Implementations, driver.Ref{Name: qualified(tn), Loc: m.lineOf(tn.Pos())})
				}
			}
		} else {
			for i := range named.NumMethods() {
				mm := named.Method(i)
				out.Methods = append(out.Methods, driver.Ref{Name: qualified(mm), Loc: m.lineOf(mm.Pos())})
			}
			for _, it := range m.interfaces() {
				if implements(named, it.iface) {
					out.Implements = append(out.Implements, driver.Ref{Name: qualified(it.obj), Loc: m.lineOf(it.obj.Pos())})
				}
			}
		}
	}

	var readable []driver.Loc
	out.Deps, out.External, readable = m.deps(declNode, target)
	callers, callerDecls := m.uses(target, via, declLoc, &out)
	out.Callers = callers

	out.Reads = append(out.Reads, declLoc)
	for _, c := range callers[:min(len(callers), readCallers)] {
		out.Reads = append(out.Reads, callerDecls[c.Name])
	}
	out.Reads = append(out.Reads, readable[:min(len(readable), readDeps)]...)
	out.Reads = mergeLocs(out.Reads)
	return out, nil
}

// module is the type-checked repository.
type module struct {
	root    string
	modPath string
	fset    *token.FileSet
	pkgs    []*packages.Package
	base    []*packages.Package // the non-test variant of each package
	files   map[string]fileInfo // absolute file name -> its syntax
}

type fileInfo struct {
	file *ast.File
	pkg  *packages.Package
}

func loadModule(ctx context.Context, root string) (*module, error) {
	m := &module{root: root, modPath: modulePath(root), fset: token.NewFileSet(), files: map[string]fileInfo{}}
	cfg := &packages.Config{
		Context: ctx,
		Dir:     root,
		Fset:    m.fset,
		Tests:   true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	m.pkgs = pkgs
	for _, p := range pkgs {
		if p.TypesInfo == nil || strings.HasSuffix(p.ID, ".test") {
			continue
		}
		isBase := p.ID == p.PkgPath
		if isBase {
			m.base = append(m.base, p)
		}
		for _, f := range p.Syntax {
			name := m.fset.Position(f.Package).Filename
			if _, seen := m.files[name]; !seen || isBase {
				m.files[name] = fileInfo{f, p}
			}
		}
	}
	return m, nil
}

func (m *module) rel(filename string) (string, bool) {
	r, err := filepath.Rel(m.root, filename)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func (m *module) key(pos token.Pos) string {
	p := m.fset.Position(pos)
	return p.Filename + ":" + strconv.Itoa(p.Offset)
}

func (m *module) inModule(obj types.Object) bool {
	return obj.Pkg() != nil && (obj.Pkg().Path() == m.modPath || strings.HasPrefix(obj.Pkg().Path(), m.modPath+"/"))
}

func (m *module) lineOf(pos token.Pos) driver.Loc {
	p := m.fset.Position(pos)
	rel, _ := m.rel(p.Filename)
	return driver.Loc{File: rel, Line: p.Line, EndLine: p.Line}
}

func (m *module) warnings() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range m.pkgs {
		if len(p.Errors) == 0 || seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		out = append(out, fmt.Sprintf("incomplete: %s does not build (%s)", p.PkgPath, firstLines(p.Errors[0].Msg, 1)))
	}
	sort.Strings(out)
	return out
}

// defAt finds the object declared as name on the given line.
func (m *module) defAt(rel string, line int, name string) types.Object {
	abs := filepath.Join(m.root, filepath.FromSlash(rel))
	fi, ok := m.files[abs]
	if !ok {
		return nil
	}
	for id, obj := range fi.pkg.TypesInfo.Defs {
		if obj == nil || id.Name != name {
			continue
		}
		if p := m.fset.Position(id.Pos()); p.Filename == abs && p.Line == line {
			return obj
		}
	}
	return nil
}

// declOf finds the declaration node of obj and its range, doc comment
// included.
func (m *module) declOf(obj types.Object) (ast.Node, driver.Loc) {
	loc := m.lineOf(obj.Pos())
	fi, ok := m.files[m.fset.Position(obj.Pos()).Filename]
	if !ok {
		return nil, loc
	}
	path, _ := astutil.PathEnclosingInterval(fi.file, obj.Pos(), obj.Pos())
	for i, n := range path {
		var start token.Pos
		switch d := n.(type) {
		case *ast.FuncDecl:
			start = d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
		case *ast.TypeSpec, *ast.ValueSpec:
			start = n.Pos()
			// A lone spec carries its doc comment on the GenDecl.
			if gd, ok := path[i+1].(*ast.GenDecl); ok && len(gd.Specs) == 1 {
				n, start = gd, gd.Pos()
				if gd.Doc != nil {
					start = gd.Doc.Pos()
				}
			}
		case *ast.Field:
			start = d.Pos()
		default:
			continue
		}
		loc.Line = m.fset.Position(start).Line
		loc.EndLine = m.fset.Position(n.End()).Line
		return n, loc
	}
	return nil, loc
}

// signature is a function's declaration without its body, or the first line
// of any other declaration.
func (m *module) signature(n ast.Node, obj types.Object) string {
	if fd, ok := n.(*ast.FuncDecl); ok {
		cp := *fd
		cp.Body, cp.Doc = nil, nil
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, m.fset, &cp); err == nil {
			sig := strings.Join(strings.Fields(buf.String()), " ")
			return strings.NewReplacer("( ", "(", ", )", ")").Replace(sig)
		}
	}
	p := m.fset.Position(obj.Pos())
	src, err := os.ReadFile(p.Filename)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if p.Line-1 < len(lines) {
		return strings.TrimSpace(lines[p.Line-1])
	}
	return ""
}

type namedIface struct {
	obj   *types.TypeName
	iface *types.Interface
}

// interfaces are the module's non-empty named interfaces, generated mocks
// left out.
func (m *module) interfaces() []namedIface {
	var out []namedIface
	m.eachType(func(tn *types.TypeName) {
		if it, ok := tn.Type().Underlying().(*types.Interface); ok && it.NumMethods() > 0 {
			out = append(out, namedIface{tn, it})
		}
	})
	return out
}

// concreteTypes are the module's named non-interface types.
func (m *module) concreteTypes() []*types.TypeName {
	var out []*types.TypeName
	m.eachType(func(tn *types.TypeName) {
		if !types.IsInterface(tn.Type()) {
			out = append(out, tn)
		}
	})
	return out
}

func (m *module) eachType(f func(*types.TypeName)) {
	for _, p := range m.base {
		if p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			if _, ok := tn.Type().(*types.Named); !ok {
				continue
			}
			if rel, ok := m.rel(m.fset.Position(tn.Pos()).Filename); !ok || isMockPath(rel) {
				continue
			}
			f(tn)
		}
	}
}

// deps lists what the declaration uses: repository declarations with their
// location, and other packages by path. readable is where those declarations
// are worth opening — interface methods are only a signature — with the
// declaration's own package first, since its helpers say most about it.
func (m *module) deps(n ast.Node, target types.Object) (refs []driver.Ref, external []string, readable []driver.Loc) {
	if n == nil {
		return nil, nil, nil
	}
	fi, ok := m.files[m.fset.Position(target.Pos()).Filename]
	if !ok {
		return nil, nil, nil
	}
	var own types.Object
	if fn, ok := target.(*types.Func); ok {
		if named := recvNamed(fn); named != nil {
			own = named.Obj()
		}
	}
	var elsewhere []driver.Loc
	seen := map[types.Object]bool{target: true, own: true}
	ast.Inspect(n, func(x ast.Node) bool {
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		obj := fi.pkg.TypesInfo.Uses[id]
		if obj == nil || seen[obj] || obj.Pkg() == nil {
			return true
		}
		switch o := obj.(type) {
		case *types.Func, *types.TypeName:
		case *types.Var:
			if o.IsField() || o.Parent() != o.Pkg().Scope() {
				return true
			}
		default:
			return true
		}
		seen[obj] = true
		if !m.inModule(obj) {
			if p := obj.Pkg().Path(); !contains(external, p) {
				external = append(external, p)
			}
			return true
		}
		_, loc := m.declOf(obj)
		refs = append(refs, driver.Ref{Name: qualified(obj), Loc: loc})
		switch {
		case isInterfaceMethod(obj):
		case obj.Pkg() == target.Pkg():
			readable = append(readable, loc)
		default:
			elsewhere = append(elsewhere, loc)
		}
		return true
	})
	sort.Strings(external)
	return refs, external, append(readable, elsewhere...)
}

// uses fills out.Tests and returns the non-test callers, one per enclosing
// declaration, with each caller's declaration range for Reads.
func (m *module) uses(target types.Object, via map[string]string, self driver.Loc, out *driver.SymbolContext) ([]driver.Ref, map[string]driver.Loc) {
	targetKey := m.key(target.Pos())
	seenPos := map[string]bool{}
	seenCaller := map[string]bool{}
	seenTest := map[string]bool{}
	callerDecls := map[string]driver.Loc{}
	var callers []driver.Ref

	for _, p := range m.pkgs {
		if p.TypesInfo == nil {
			continue
		}
		// A file is parsed once per package variant that holds it, so a
		// position is only meaningful against this package's own syntax.
		syntax := map[string]*ast.File{}
		for _, f := range p.Syntax {
			syntax[m.fset.Position(f.Package).Filename] = f
		}
		for id, obj := range p.TypesInfo.Uses {
			k := m.key(obj.Pos())
			through, isVia := via[k]
			if k != targetKey && !isVia {
				continue
			}
			pos := m.fset.Position(id.Pos())
			if seenPos[pos.String()] {
				continue
			}
			seenPos[pos.String()] = true
			rel, ok := m.rel(pos.Filename)
			if !ok || isMockPath(rel) {
				continue
			}
			if rel == self.File && pos.Line >= self.Line && pos.Line <= self.EndLine {
				continue // recursion, or the declaration naming itself
			}
			file, ok := syntax[pos.Filename]
			if !ok {
				continue
			}
			ref := driver.Ref{Loc: driver.Loc{File: rel, Line: pos.Line, EndLine: pos.Line}, Via: through}

			if strings.HasSuffix(rel, "_test.go") {
				ref.Note = specName(file, id.Pos())
				ref.Name = ref.Note
				if k := rel + "\x00" + ref.Note; !seenTest[k] {
					seenTest[k] = true
					out.Tests = append(out.Tests, ref)
				}
				continue
			}
			name, declLoc := m.enclosing(file, id.Pos())
			ref.Name = name
			if !seenCaller[name] {
				seenCaller[name] = true
				callerDecls[name] = declLoc
				callers = append(callers, ref)
			}
		}
	}
	byLoc := func(refs []driver.Ref) {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Loc.File != refs[j].Loc.File {
				return refs[i].Loc.File < refs[j].Loc.File
			}
			return refs[i].Loc.Line < refs[j].Loc.Line
		})
	}
	byLoc(callers)
	byLoc(out.Tests)
	return callers, callerDecls
}

// enclosing names the top-level declaration containing pos.
func (m *module) enclosing(f *ast.File, pos token.Pos) (string, driver.Loc) {
	pkg := f.Name.Name
	for _, d := range f.Decls {
		if pos < d.Pos() || pos >= d.End() {
			continue
		}
		start := d.Pos()
		name := pkg
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			name = pkg + "." + d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = pkg + "." + receiverType(d.Recv.List[0].Type) + "." + d.Name.Name
			}
		case *ast.GenDecl:
			if len(d.Specs) > 0 {
				switch s := d.Specs[0].(type) {
				case *ast.TypeSpec:
					name = pkg + "." + s.Name.Name
				case *ast.ValueSpec:
					name = pkg + "." + s.Names[0].Name
				}
			}
		}
		rel, _ := m.rel(m.fset.Position(d.Pos()).Filename)
		return name, driver.Loc{File: rel, Line: m.fset.Position(start).Line, EndLine: m.fset.Position(d.End()).Line}
	}
	return pkg, m.lineOf(pos)
}

// specName is the innermost Ginkgo spec text around pos, or the test
// function's name.
func specName(f *ast.File, pos token.Pos) string {
	path, _ := astutil.PathEnclosingInterval(f, pos, pos)
	for _, n := range path {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 0 && ginkgoNodes[callName(call.Fun)] {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					return s
				}
			}
		}
		if fd, ok := n.(*ast.FuncDecl); ok {
			return fd.Name.Name
		}
	}
	return ""
}

func callName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func isInterfaceMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	return recv != nil && types.IsInterface(recv.Type())
}

// recvNamed is a method's receiver type without the pointer.
func recvNamed(fn *types.Func) *types.Named {
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return nil
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}

func implements(named *types.Named, iface *types.Interface) bool {
	if named.TypeParams().Len() > 0 {
		return false
	}
	return types.Implements(named, iface) || types.Implements(types.NewPointer(named), iface)
}

func methodByName(iface *types.Interface, name string) *types.Func {
	for i := range iface.NumMethods() {
		if mm := iface.Method(i); mm.Name() == name {
			return mm
		}
	}
	return nil
}

// qualified is pkg.Name, or pkg.Type.Name for a method.
func qualified(obj types.Object) string {
	pkg := ""
	if obj.Pkg() != nil {
		pkg = obj.Pkg().Name() + "."
	}
	if fn, ok := obj.(*types.Func); ok {
		if named := recvNamed(fn); named != nil {
			return pkg + named.Obj().Name() + "." + fn.Name()
		}
	}
	return pkg + obj.Name()
}

// qualifiedIn names an interface's method after the interface it was asked
// for, even when it is inherited from an embedded one.
func qualifiedIn(owner *types.TypeName, mm *types.Func) string {
	return owner.Pkg().Name() + "." + owner.Name() + "." + mm.Name()
}

// mergeLocs drops duplicates and folds ranges in one file that overlap or
// are a blank line apart, keeping the first occurrence's place in the order.
func mergeLocs(locs []driver.Loc) []driver.Loc {
	var out []driver.Loc
	for _, l := range locs {
		if l.File == "" {
			continue
		}
		merged := false
		for i := range out {
			o := &out[i]
			if o.File == l.File && l.Line <= o.EndLine+2 && l.EndLine >= o.Line-2 {
				o.Line, o.EndLine = min(o.Line, l.Line), max(o.EndLine, l.EndLine)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, l)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
