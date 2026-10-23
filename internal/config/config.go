// Package config holds godon's per-repository settings. For now every
// repository gets the defaults below; reading them from a file in the
// repository is the next design step.
package config

import "runtime"

// Config is everything that may differ between repositories.
type Config struct {
	Test        Test
	Integration Integration
}

// Test configures unit-test runs.
type Test struct {
	// Race runs tests with the race detector. It needs cgo and a C compiler,
	// which Windows machines often lack, so it defaults off there.
	Race bool
}

// Integration configures the integration-tagged suite.
type Integration struct {
	Tags     []string
	Packages []string
	Timeout  string
}

// Default is the configuration used when a repository sets nothing.
func Default() Config {
	return Config{
		Test: Test{Race: runtime.GOOS != "windows"},
		Integration: Integration{
			Tags:     []string{"integration"},
			Packages: []string{"./..."},
			Timeout:  "30m",
		},
	}
}
