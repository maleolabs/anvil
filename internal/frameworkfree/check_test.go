package frameworkfree

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot resolves the Core repository root (the directory containing
// go.mod) from the test package location.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test package directory")
		}
		dir = parent
	}
}

// writeTempModule creates an isolated module using the same module path as
// the Core repository (maleolabs.com/anvil) so `go list` reports the same
// import paths the check matches against. files maps relative paths to
// their contents.
func writeTempModule(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	files["go.mod"] = "module maleolabs.com/anvil\n\ngo 1.25\n"
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("creating %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

// TestCoreRuntimeIsFrameworkFree is the Core framework-free proof (ADR-026
// decision 4, Transition Plan §11.2): the import graph of the Anvil
// Runtime (Core) — every package under cmd/ and internal/ except the
// framework packages themselves — contains zero framework packages. It
// runs as part of `go test ./...`, so every CI run of the test suite
// re-checks the evidence; scripts/framework-free-check.sh is the
// documented single entry point for the governance review.
func TestCoreRuntimeIsFrameworkFree(t *testing.T) {
	if err := CheckCoreRuntime(repoRoot(t), "go"); err != nil {
		t.Fatalf("Core runtime import graph is not framework-free: %v", err)
	}
}

// TestCheckCoreRuntimeFailsWhenFrameworkPackageIntroduced verifies the
// check's failure mode: when a Core root imports a framework package, the
// check reports the violation with root attribution. This is what CI
// demonstrates when a framework package is (re)introduced into the
// runtime.
//
// The catalog is empty in the real repository — both first-party
// framework package sets left the Core module in the repository split
// (TS-016-01-01, TS-016-02-01) — so the test injects a synthetic
// framework package into the catalog for the duration of the test and
// exercises the failure mode against it.
func TestCheckCoreRuntimeFailsWhenFrameworkPackageIntroduced(t *testing.T) {
	withFrameworkPackage(t, "maleolabs.com/anvil/internal/synthetic")
	root := writeTempModule(t, map[string]string{
		"internal/synthetic/synthetic.go": "package synthetic\n",
		"cmd/leaky/main.go":               "package main\n\nimport _ \"maleolabs.com/anvil/internal/synthetic\"\n\nfunc main() {}\n",
		"cmd/clean/main.go":               "package main\n\nfunc main() {}\n",
	})

	err := CheckCoreRuntime(root, "go")
	if err == nil {
		t.Fatal("CheckCoreRuntime() = nil, want error when a Core root imports a framework package")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cmd/leaky") {
		t.Errorf("error = %q, want it to name the violating root cmd/leaky", msg)
	}
	if !strings.Contains(msg, "maleolabs.com/anvil/internal/synthetic") {
		t.Errorf("error = %q, want it to name the framework package maleolabs.com/anvil/internal/synthetic", msg)
	}
	if strings.Contains(msg, "cmd/clean") {
		t.Errorf("error = %q, must not report the clean root cmd/clean", msg)
	}
}

// TestCheckCoreRuntimeAllowsAdapterExclusions verifies that packages
// listed in the framework catalog — which legitimately carry framework
// knowledge as distribution content (ADR-009 §8.1) — are excluded from
// the Core roots and do not fail the check. The synthetic package stands
// in for the distribution-content shape the pre-split adapter binaries
// had; the catalog is empty in the real repository (TS-016-01-01,
// TS-016-02-01).
func TestCheckCoreRuntimeAllowsAdapterExclusions(t *testing.T) {
	withFrameworkPackage(t,
		"maleolabs.com/anvil/internal/synthetic",
		"maleolabs.com/anvil/cmd/synthetic-adapter",
	)
	root := writeTempModule(t, map[string]string{
		"internal/synthetic/synthetic.go": "package synthetic\n",
		"cmd/synthetic-adapter/main.go":   "package main\n\nimport _ \"maleolabs.com/anvil/internal/synthetic\"\n\nfunc main() {}\n",
		"cmd/clean/main.go":               "package main\n\nfunc main() {}\n",
	})

	if err := CheckCoreRuntime(root, "go"); err != nil {
		t.Fatalf("CheckCoreRuntime() = %v, want nil (catalog packages are excluded from the Core roots, ADR-009 §8.1)", err)
	}
}

// withFrameworkPackage temporarily extends the framework package catalog
// with the given import paths and restores the previous catalog when the
// test completes. The real catalog is empty (both first-party framework
// package sets left the Core module in TS-016-01-01 and TS-016-02-01);
// tests inject synthetic entries to exercise the check's failure and
// exclusion mechanics deterministically.
func withFrameworkPackage(t *testing.T, packages ...string) {
	t.Helper()
	previous := frameworkPackages
	frameworkPackages = append(append([]string{}, previous...), packages...)
	t.Cleanup(func() { frameworkPackages = previous })
}
