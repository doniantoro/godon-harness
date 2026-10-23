package main

import "testing"

func TestParseTestArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"a package and a run filter", []string{"./pkg", "-run", "TestX"}, false},
		{"--all alone", []string{"--all"}, false},
		{"--integration with a label", []string{"--integration", "-label", "smoke"}, false},
		{"no package and no mode", nil, true},
		{"--all with a package", []string{"--all", "./pkg"}, true},
		{"--integration with --with-all", []string{"--integration", "--with-all"}, true},
		{"a flag without its value", []string{"./pkg", "-run"}, true},
		{"an unknown flag", []string{"./pkg", "-v"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTestArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
