package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/doniantoro/godon-harness/internal/driver"
)

func TestFormatContext(t *testing.T) {
	c := driver.SymbolContext{
		Found:     true,
		Symbol:    driver.Symbol{File: "service/service.go", Line: 16, Owner: "Service", Name: "Validate", Kind: "method"},
		Signature: "func (s *Service) Validate(id string) error",
		Loc:       driver.Loc{File: "service/service.go", Line: 15, EndLine: 25},
		Implements: []driver.Ref{
			{Name: "types.Validator.Validate", Loc: driver.Loc{File: "types/types.go", Line: 8, EndLine: 8}},
		},
		External: []string{"errors"},
		Callers: []driver.Ref{
			{Name: "handler.Handler.Handle", Loc: driver.Loc{File: "handler/handler.go", Line: 10, EndLine: 10}, Via: "types.Validator"},
		},
		Reads: []driver.Loc{
			{File: "service/service.go", Line: 15, EndLine: 27},
			{File: "handler/handler.go", Line: 9, EndLine: 11},
		},
		Warnings: []string{"incomplete: example.com/broken does not build (x)"},
	}
	for i := range 12 {
		c.Deps = append(c.Deps, driver.Ref{Name: fmt.Sprintf("repo.F%d", i), Loc: driver.Loc{File: "repo/repo.go", Line: i + 1, EndLine: i + 3}})
	}

	short, full := formatContext(c, 10)
	for _, want := range []string{
		"SYMBOL: Service.Validate  method  service/service.go:15-25",
		"  func (s *Service) Validate(id string) error",
		"Implements:\n  types.Validator.Validate  types/types.go:8",
		"  repo.F9  repo/repo.go:10-12",
		"  + 2 more",
		"  + external: errors",
		"handler.Handler.Handle  handler/handler.go:10  via types.Validator",
		"Relevant tests:\n  none",
		"Read:\n  service/service.go:15-27\n  handler/handler.go:9-11\n  (2 files, 16 lines)",
		"incomplete: example.com/broken",
	} {
		if !strings.Contains(short, want) {
			t.Errorf("output lacks %q:\n%s", want, short)
		}
	}
	if strings.Contains(short, "repo.F10") || !strings.Contains(full, "repo.F11") {
		t.Errorf("truncation: the summary must stop at 10 and the full text keep all 12")
	}
	if strings.Contains(short, "Implementations:") || strings.Contains(short, "Methods:") {
		t.Errorf("empty optional sections must be left out:\n%s", short)
	}
}
