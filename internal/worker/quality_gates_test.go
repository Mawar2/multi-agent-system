package worker

import "testing"

func TestNoTargets(t *testing.T) {
	benign := []string{
		`go: warning: "./..." matched no packages` + "\nno packages to test\n",
		"no go files to analyze",
		"matched no packages",
		"/bin/sh: golangci-lint: No such file or directory",
	}
	for _, out := range benign {
		if !noTargets(out) {
			t.Errorf("expected noTargets=true for %q", out)
		}
	}

	realFailures := []string{
		"--- FAIL: TestFoo\nFAIL\nexit status 1",
		"vet: ./x.go:3:2: undefined: bar",
		"",
	}
	for _, out := range realFailures {
		if noTargets(out) {
			t.Errorf("expected noTargets=false (real failure) for %q", out)
		}
	}
}
