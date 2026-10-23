package golang

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doniantoro/godon-harness/internal/report"
)

const mod = "example.com/m"

// stream builds a go test -json stream from compact event literals.
func stream(t *testing.T, events ...map[string]string) *parsed {
	t.Helper()
	var sb strings.Builder
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	p, err := parseEvents(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ev(action, pkg, test, output string) map[string]string {
	return map[string]string{"Action": action, "Package": mod + "/" + pkg, "Test": test, "Output": output}
}

func TestStageClassification(t *testing.T) {
	tests := []struct {
		name       string
		events     []map[string]string
		goExit     int
		narrowed   bool
		wantStatus string
		wantCats   []string
		wantTests  []string
	}{
		{
			name: "a passing package is PASSED",
			events: []map[string]string{
				ev("run", "a", "TestOK", ""), ev("pass", "a", "TestOK", ""), ev("pass", "a", "", ""),
			},
			wantStatus: report.Passed,
		},
		{
			name: "only the failing leaf subtest is reported, not its parent",
			events: []map[string]string{
				ev("run", "a", "TestSub", ""), ev("run", "a", "TestSub/bad", ""),
				ev("output", "a", "TestSub/bad", "    x_test.go:8: broke\n"),
				ev("output", "a", "TestSub/bad", "--- FAIL: TestSub/bad (0.00s)\n"),
				ev("fail", "a", "TestSub/bad", ""), ev("fail", "a", "TestSub", ""), ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed,
			wantCats: []string{"assertion"}, wantTests: []string{"TestSub/bad"},
		},
		{
			name: "a data race is classified race",
			events: []map[string]string{
				ev("run", "a", "TestR", ""),
				ev("output", "a", "TestR", "WARNING: DATA RACE\n"), ev("output", "a", "TestR", "==================\n"),
				ev("fail", "a", "TestR", ""), ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed, wantCats: []string{"race"}, wantTests: []string{"TestR"},
		},
		{
			name: "a goleak report in TestMain is a package-level leak",
			events: []map[string]string{
				ev("run", "a", "TestL", ""), ev("pass", "a", "TestL", ""),
				ev("output", "a", "", "goleak: Errors on successful test run: found unexpected goroutines:\n"),
				ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed, wantCats: []string{"leak"}, wantTests: []string{""},
		},
		{
			name: "a gomock expectation failure is classified mock",
			events: []map[string]string{
				ev("run", "a", "TestM", ""),
				ev("output", "a", "TestM", "    controller.go:97: missing call(s) to *mocks.MockX.Do()\n"),
				ev("output", "a", "TestM", "--- FAIL: TestM (0.00s)\n"),
				ev("fail", "a", "TestM", ""), ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed, wantCats: []string{"mock"}, wantTests: []string{"TestM"},
		},
		{
			name: "a test timeout is classified timeout",
			events: []map[string]string{
				ev("run", "a", "TestT", ""),
				ev("output", "a", "", "panic: test timed out after 1s\n"),
				ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed, wantCats: []string{"timeout"}, wantTests: []string{""},
		},
		{
			name: "a failed Ginkgo BeforeSuite is classified setup",
			events: []map[string]string{
				ev("run", "a", "TestSuite", ""),
				ev("output", "a", "TestSuite", "[BeforeSuite] [FAILED] [0.001 seconds]\n"),
				ev("output", "a", "TestSuite", "------------------------------\n"),
				ev("fail", "a", "TestSuite", ""), ev("fail", "a", "", ""),
			},
			goExit: 1, wantStatus: report.Failed, wantCats: []string{"setup"}, wantTests: []string{"TestSuite"},
		},
		{
			name: "a filter that ran no test is NO TESTS MATCHED, not PASSED",
			events: []map[string]string{
				ev("output", "a", "", "testing: warning: no tests to run\n"), ev("pass", "a", "", ""),
			},
			narrowed: true, wantStatus: report.NoMatches,
		},
		{
			name: "a Ginkgo focus that ran no spec is NO TESTS MATCHED",
			events: []map[string]string{
				ev("run", "a", "TestSuite", ""),
				ev("output", "a", "TestSuite", "Ran 0 of 12 Specs in 0.001 seconds\n"),
				ev("pass", "a", "TestSuite", ""), ev("pass", "a", "", ""),
			},
			narrowed: true, wantStatus: report.NoMatches,
		},
		{
			name:       "go test failing with no events is never reported green",
			events:     nil,
			goExit:     1,
			wantStatus: report.Failed, wantCats: []string{"unclassified"}, wantTests: []string{""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := stream(t, tc.events...).stage("s", mod, tc.goExit, nil, tc.narrowed)
			if st.Status != tc.wantStatus {
				t.Fatalf("status = %s, want %s", st.Status, tc.wantStatus)
			}
			var cats, tests []string
			for _, f := range st.Failures {
				cats = append(cats, f.Category)
				tests = append(tests, f.Test)
			}
			if strings.Join(cats, ",") != strings.Join(tc.wantCats, ",") {
				t.Fatalf("categories = %v, want %v", cats, tc.wantCats)
			}
			if strings.Join(tests, ",") != strings.Join(tc.wantTests, ",") {
				t.Fatalf("tests = %v, want %v", tests, tc.wantTests)
			}
		})
	}
}

func TestBuildFailure(t *testing.T) {
	input := strings.Join([]string{
		`{"ImportPath":"example.com/m/b [example.com/m/b.test]","Action":"build-output","Output":"b/x_test.go:5:9: undefined: y\n"}`,
		`{"ImportPath":"example.com/m/b [example.com/m/b.test]","Action":"build-fail"}`,
		`{"Action":"output","Package":"example.com/m/b","Output":"FAIL\texample.com/m/b [build failed]\n"}`,
		`{"Action":"fail","Package":"example.com/m/b","FailedBuild":"example.com/m/b [example.com/m/b.test]"}`,
	}, "\n")
	p, err := parseEvents(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	st := p.stage("s", mod, 1, nil, false)
	if len(st.Failures) != 1 || st.Failures[0].Category != "build" || st.Failures[0].Package != "b" {
		t.Fatalf("failures = %+v, want one build failure in package b", st.Failures)
	}
	if !strings.Contains(strings.Join(st.Failures[0].Lines, "\n"), "undefined: y") {
		t.Fatalf("build extract lost the compiler error: %v", st.Failures[0].Lines)
	}
}
