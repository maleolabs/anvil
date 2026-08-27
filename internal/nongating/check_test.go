package nongating

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

// TestLifecycleNonGating is the diagnostics non-gating proof (ADR-036 §3,
// TS-015-05-03): the lifecycle import graph — every package under
// ./internal/... except the diagnostics packages themselves, plus the
// lifecycle command files under cmd/ — contains zero diagnostics packages.
// It runs as part of `go test ./...`, so every CI run of the test suite
// re-checks the evidence; scripts/non-gating-check.sh is the documented
// single entry point for the governance review.
func TestLifecycleNonGating(t *testing.T) {
	if err := CheckLifecycleNonGating(repoRoot(t), "go"); err != nil {
		t.Fatalf("lifecycle import graph depends on diagnostics: %v", err)
	}
}

// lifecycleCommandStubs returns a minimal valid file for every entry in
// lifecycleCommandFiles. The file-level check requires every listed file
// to exist, so temp-module fixtures must always include the full set.
func lifecycleCommandStubs() map[string]string {
	stubs := map[string]string{}
	for _, rel := range lifecycleCommandFiles {
		stubs[rel] = "package cmd\n"
	}
	return stubs
}

// TestCheckLifecycleNonGatingFailsWhenLifecyclePackageImportsDiagnostics
// verifies the package-level failure mode: when an internal lifecycle
// package imports a diagnostics package, the check reports the violation
// with root attribution. This is what CI demonstrates when a diagnostics
// dependency is (re)introduced into the runtime layers.
func TestCheckLifecycleNonGatingFailsWhenLifecyclePackageImportsDiagnostics(t *testing.T) {
	files := lifecycleCommandStubs()
	files["internal/inspection/diagnostics.go"] = "package inspection\n"
	files["internal/runtime/lifecycle.go"] = "package runtime\n\nimport _ \"maleolabs.com/anvil/internal/inspection\"\n"
	files["internal/artifact/artifact.go"] = "package artifact\n"
	root := writeTempModule(t, files)

	err := CheckLifecycleNonGating(root, "go")
	if err == nil {
		t.Fatal("CheckLifecycleNonGating() = nil, want error when a lifecycle package imports a diagnostics package")
	}
	msg := err.Error()
	if !strings.Contains(msg, "internal/runtime") {
		t.Errorf("error = %q, want it to name the violating root internal/runtime", msg)
	}
	if !strings.Contains(msg, "maleolabs.com/anvil/internal/inspection") {
		t.Errorf("error = %q, want it to name the diagnostics package maleolabs.com/anvil/internal/inspection", msg)
	}
	if strings.Contains(msg, "internal/artifact") {
		t.Errorf("error = %q, must not report the clean root internal/artifact", msg)
	}
}

// TestCheckLifecycleNonGatingFailsWhenLifecycleCommandImportsDiagnostics
// verifies the file-level failure mode: when a lifecycle command file
// (cmd/) imports a diagnostics package, the check reports the violation
// naming the file. This covers the cmd package, which shares one Go
// package between lifecycle commands and diagnostics commands and therefore
// cannot be checked at package granularity.
func TestCheckLifecycleNonGatingFailsWhenLifecycleCommandImportsDiagnostics(t *testing.T) {
	files := lifecycleCommandStubs()
	files["internal/inspection/diagnostics.go"] = "package inspection\n"
	files["cmd/server_release_activate.go"] = "package cmd\n\nimport _ \"maleolabs.com/anvil/internal/inspection\"\n"
	root := writeTempModule(t, files)

	err := CheckLifecycleNonGating(root, "go")
	if err == nil {
		t.Fatal("CheckLifecycleNonGating() = nil, want error when a lifecycle command file imports a diagnostics package")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cmd/server_release_activate.go") {
		t.Errorf("error = %q, want it to name the violating file cmd/server_release_activate.go", msg)
	}
	if !strings.Contains(msg, "maleolabs.com/anvil/internal/inspection") {
		t.Errorf("error = %q, want it to name the diagnostics package maleolabs.com/anvil/internal/inspection", msg)
	}
	if strings.Contains(msg, "cmd/server_release.go") {
		t.Errorf("error = %q, must not report the clean file cmd/server_release.go", msg)
	}
}

// TestCheckLifecycleNonGatingAllowsDiagnosticsImportingLifecycle verifies
// the exclusion list in the check: the diagnostics packages themselves are
// not lifecycle roots and may import lifecycle packages (read-only
// inspection consumes runtime/server/release state — ADR-036 §3). If the
// exclusion list regressed, the diagnostics packages would be enumerated
// as lifecycle roots and this proof would fail.
func TestCheckLifecycleNonGatingAllowsDiagnosticsImportingLifecycle(t *testing.T) {
	files := lifecycleCommandStubs()
	files["internal/inspection/diagnostics.go"] = "package inspection\n\nimport _ \"maleolabs.com/anvil/internal/runtime\"\n"
	files["internal/output/diagnostic/diagnostic.go"] = "package diagnostic\n\nimport _ \"maleolabs.com/anvil/internal/inspection\"\n"
	files["internal/runtime/lifecycle.go"] = "package runtime\n"
	root := writeTempModule(t, files)

	if err := CheckLifecycleNonGating(root, "go"); err != nil {
		t.Fatalf("CheckLifecycleNonGating() = %v, want nil (diagnostics packages are excluded from the lifecycle roots and may consume lifecycle state read-only)", err)
	}
}

// TestLifecycleCommandFilesExistAndImportCorePackages guards the
// lifecycleCommandFiles list in the real repository: every listed file must
// exist and must import at least one Core package — the file-level check is
// load-bearing, and a stale list (renamed or removed command file, or a
// file that no longer participates in lifecycle transitions) must surface.
func TestLifecycleCommandFilesExistAndImportCorePackages(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range lifecycleCommandFiles {
		imports, err := commandFileImports(root, rel)
		if err != nil {
			t.Fatalf("%s must exist and parse: %v", rel, err)
		}

		coreImports := 0
		for _, imp := range imports {
			if strings.HasPrefix(imp, "maleolabs.com/anvil/") {
				coreImports++
			}
		}
		if coreImports == 0 {
			t.Errorf("%s imports no Core package — it may no longer be a lifecycle command; update lifecycleCommandFiles in check.go", rel)
		}
	}
}

// TestDiagnosticsPackagesConsumedOnlyByCmdLayer verifies the direction of
// the dependency boundary in the real repository: the diagnostics packages
// must actually be consumed by the CLI layer (cmd/), so the exclusion list
// in the check is load-bearing — the proof is not passing vacuously because
// the diagnostics packages vanished.
func TestDiagnosticsPackagesConsumedOnlyByCmdLayer(t *testing.T) {
	root := repoRoot(t)

	for _, dp := range diagnosticsPackages {
		consumers, err := consumersOf(root, "go", dp)
		if err != nil {
			t.Fatalf("resolve consumers of %s: %v", dp, err)
		}

		cliConsumers := 0
		for _, consumer := range consumers {
			if strings.HasPrefix(consumer, "maleolabs.com/anvil/cmd/") || consumer == "maleolabs.com/anvil/cmd" {
				cliConsumers++
			}
		}
		if cliConsumers == 0 {
			t.Errorf("diagnostics package %s has no cmd/ consumer — the diagnostics surface may be gone or the package list may be stale", dp)
		}
	}
}

// consumersOf returns every package in the module (excluding the package
// itself) that transitively imports importPath.
func consumersOf(workDir, goCmd, importPath string) ([]string, error) {
	out, err := runGo(workDir, goCmd, "list", "./cmd/...", "./internal/...")
	if err != nil {
		return nil, err
	}

	var consumers []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" || pkg == importPath {
			continue
		}
		deps, err := importGraph(workDir, goCmd, pkg)
		if err != nil {
			return nil, err
		}
		for _, dep := range deps {
			if dep == importPath {
				consumers = append(consumers, pkg)
				break
			}
		}
	}
	return consumers, nil
}
