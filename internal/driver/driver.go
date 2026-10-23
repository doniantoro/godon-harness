// Package driver is the seam between godon's commands and a language. A
// command asks the driver for the repository; the driver runs that
// language's tools and returns language-neutral results (report.Stage,
// report.Check, Symbol). Adding a language is adding a Driver.
package driver

import (
	"context"

	"github.com/doniantoro/godon-harness/internal/report"
)

// TestSpec describes one test stage.
type TestSpec struct {
	Name     string   // stage name shown in the summary
	Packages []string // language-specific package patterns
	Run      string   // test-function filter
	Focus    string   // spec filter (Ginkgo -focus for Go)
	Label    string   // label filter (Ginkgo -label-filter for Go)
	Race     bool
	Tags     []string // build tags
	Timeout  string   // e.g. "30m"; empty is the tool's default
	NoCache  bool     // force a fresh run (integration suites)
	Narrowed bool     // a filter is set, so zero matched tests is a failure
}

// SymbolQuery asks where an identifier is declared.
type SymbolQuery struct {
	Name         string // the declared identifier
	Owner        string // optional owning type, from Owner.Name
	Path         string // optional directory filter, relative to the root
	IncludeTests bool   // include test files and generated mocks
}

// Symbol is one declaration.
type Symbol struct {
	File  string // slash-separated, relative to the repository root
	Line  int
	Owner string // owning type for methods and fields, else empty
	Name  string
	Kind  string // func, method, struct, interface, type, field, const, var
}

// Qualified is Owner.Name, or Name when there is no owner.
func (s Symbol) Qualified() string {
	if s.Owner == "" {
		return s.Name
	}
	return s.Owner + "." + s.Name
}

// ContextQuery asks for what an agent needs to know about one declaration
// before reading or changing it.
type ContextQuery struct {
	Name  string // the declared identifier
	Owner string // optional owning type, from Owner.Name
	Path  string // optional directory filter, relative to the root
}

// Loc is a line range in a file.
type Loc struct {
	File    string // slash-separated, relative to the repository root
	Line    int
	EndLine int // equal to Line for a single line
}

// Ref is one related declaration or use.
type Ref struct {
	Name string // qualified, e.g. handler.Handler.Handle
	Loc  Loc
	Via  string // the interface a call goes through, when it does
	Note string // tests: the spec text or test function name
}

// SymbolContext is the answer to a ContextQuery. When the name does not
// resolve to exactly one declaration, Found is false and Candidates lists
// what matched (empty when nothing did).
type SymbolContext struct {
	Found      bool
	Candidates []Symbol

	Symbol    Symbol
	Signature string
	Loc       Loc // the whole declaration, doc comment included

	Implements      []Ref    // interfaces the type or method satisfies
	Implementations []Ref    // for an interface: the types that satisfy it
	Methods         []Ref    // for a type or interface
	Deps            []Ref    // repository declarations it uses
	External        []string // other packages it uses
	Callers         []Ref    // non-test uses, by enclosing declaration
	Tests           []Ref    // uses in tests
	Reads           []Loc    // the ranges worth reading, most useful first
	Warnings        []string // e.g. a package that does not build
}

// CheckOptions selects static checks.
type CheckOptions struct {
	Lint bool
}

// Env is what a driver may use besides the repository: where to write
// details and where to cache tools it builds.
type Env struct {
	Root     string
	RunDir   string
	CacheDir string
}

// Driver is one language.
type Driver interface {
	// Name is the language name used in messages.
	Name() string
	// Detect reports whether the repository at root uses this language.
	Detect(root string) bool
	// Handles reports whether a file belongs to this language (for hooks).
	Handles(path string) bool

	RunTests(ctx context.Context, env Env, spec TestSpec) (report.Stage, error)
	FindSymbols(root string, q SymbolQuery) ([]Symbol, error)
	SymbolDoc(ctx context.Context, root string, s Symbol) (string, error)
	Context(ctx context.Context, root string, q ContextQuery) (SymbolContext, error)
	Checks(ctx context.Context, env Env, opts CheckOptions) ([]report.Check, error)

	// Format rewrites one file in the language's canonical form. A file the
	// formatter cannot parse is returned as a *ParseError.
	Format(ctx context.Context, root, path string) error
	// StopCheck runs the quick gate before an agent ends its turn, over the
	// files that changed. It returns a non-empty problem text on failure.
	StopCheck(ctx context.Context, root string, changed []string) (string, error)
}

// ParseError is a file the formatter could not parse.
type ParseError struct {
	Path   string
	Detail string
}

func (e *ParseError) Error() string { return e.Path + ": " + e.Detail }

// Detect returns the first driver that recognises the repository.
func Detect(root string, drivers []Driver) (Driver, bool) {
	for _, d := range drivers {
		if d.Detect(root) {
			return d, true
		}
	}
	return nil, false
}
