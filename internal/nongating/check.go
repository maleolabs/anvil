// Package nongating provides the diagnostics non-gating proof (ADR-036 §3,
// TS-015-05-03): a check over the lifecycle import graph that demonstrates
// zero dependency on diagnostics results. The check is the re-checkable
// evidence for the governance review (ADR-036 §3, §6) — the import graph,
// not the claim.
//
// Two properties are enforced:
//
//  1. No lifecycle code path depends on diagnostics (ADR-036 §3: "Lifecycle
//     operations never depend on diagnostics"; no lifecycle transition
//     requires a diagnostics result to proceed). Every package under
//     ./internal/... — the runtime and server layers — must not import a
//     diagnostics package; the diagnostics surfaces are CLI-layer
//     consumers only. The cmd/ package is a single Go package shared by
//     lifecycle commands and diagnostics commands, so the lifecycle
//     command files (cmd/server_release_*.go, cmd/deployment_install.go,
//     cmd/deployment_activate.go, cmd/deployment_rollback.go) are checked
//     individually at file granularity.
//
//  2. Diagnostics cannot gate: the diagnostics packages must never become
//     a dependency of the lifecycle layers — same import-graph check, which
//     makes the guarantee structural rather than behavioral.
//
// The read-only half of the guarantee (diagnostics cannot mutate lifecycle
// state) is enforced behaviorally by the diagnostics-surface snapshot test
// (cmd/non_gating_guarantee_test.go): every diagnostics command runs
// against a populated lifecycle tree and the tree must be byte-identical
// afterwards.
//
// The check runs as part of `go test ./...`
// (TestLifecycleNonGating) and through the documented entry point
// scripts/non-gating-check.sh.
package nongating

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// diagnosticsPackages are the packages that implement the diagnostics /
// observability surface (ADR-036 §3, TS-015-05-01/02). They must never
// appear in the lifecycle import graph — lifecycle code paths contain no
// dependency on diagnostics results, and state queries observe but never
// gate.
//
//   - internal/inspection: the diagnostic inspection engines consumed by
//     the diagnostics commands (server doctor, server readiness, system
//     inspect);
//   - internal/output/diagnostic: the diagnostic output rendering shared
//     by the diagnostics commands.
//
// These packages are excluded from the lifecycle roots AND forbidden as
// dependencies of every lifecycle root. Introducing a new diagnostics
// package (internal/<diagnostics> or internal/output/<diagnostics>)
// requires extending this list — a lifecycle package that imports it then
// fails this check. The governance review checks this list (ADR-036 §3,
// §6; TS-015-05-03).
var diagnosticsPackages = []string{
	"maleolabs.com/anvil/internal/inspection",
	"maleolabs.com/anvil/internal/output/diagnostic",
}

// lifecycleCommandFiles are the cmd/ files that implement lifecycle
// transitions — the CLI surfaces over the coordinator Install/Activate/
// Rollback paths (internal/server/coordinator.go) and the runtime
// lifecycle. The cmd package is a single Go package shared with the
// diagnostics commands, so the package-level import graph cannot
// distinguish them; these files are checked individually.
//
// The list is load-bearing: a lifecycle command file that gains a
// diagnostics import fails the check, and a file that disappears (renamed
// or removed) fails the existence assertion in the check. Adding a new
// lifecycle command file requires extending this list.
var lifecycleCommandFiles = []string{
	"cmd/server_release.go",
	"cmd/server_release_activate.go",
	"cmd/server_release_rollback.go",
	"cmd/server_release_cleanup.go",
	"cmd/deployment_install.go",
	"cmd/deployment_activate.go",
	"cmd/deployment_rollback.go",
}

// isDiagnosticsPackage reports whether importPath is a diagnostics package.
func isDiagnosticsPackage(importPath string) bool {
	for _, dp := range diagnosticsPackages {
		if importPath == dp {
			return true
		}
	}
	return false
}

// CheckLifecycleNonGating verifies that the lifecycle import graph contains
// zero diagnostics packages.
//
// workDir must be the module root (the directory containing go.mod) of the
// Core repository; goCmd is the Go toolchain binary (usually "go").
//
// The check enforces two structural boundaries (ADR-036 §3,
// TS-015-05-03):
//
//  1. Package level: every package under ./internal/... — excluding the
//     diagnostics packages themselves and this check package — must not
//     transitively import a diagnostics package. The runtime and server
//     layers never see diagnostics; diagnostics is a CLI-layer surface.
//
//  2. File level: every lifecycle command file under cmd/ (the CLI
//     surfaces over the coordinator Install/Activate/Rollback transitions)
//     must not import a diagnostics package. The cmd package is one Go
//     package shared with the diagnostics commands, so the lifecycle
//     files are checked individually via the Go parser.
//
// It returns nil when the lifecycle import graph is diagnostics-free, or
// an error listing every violation with root attribution.
func CheckLifecycleNonGating(workDir, goCmd string) error {
	var violations []string

	// 1. Package level: lifecycle layers under ./internal/...
	roots, err := lifecycleInternalRoots(workDir, goCmd)
	if err != nil {
		return err
	}
	for _, root := range roots {
		deps, err := importGraph(workDir, goCmd, root)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if isDiagnosticsPackage(dep) {
				violations = append(violations, fmt.Sprintf("%s imports diagnostics package %s", root, dep))
			}
		}
	}

	// 2. File level: lifecycle command files under cmd/.
	for _, rel := range lifecycleCommandFiles {
		imports, err := commandFileImports(workDir, rel)
		if err != nil {
			return err
		}
		for _, imp := range imports {
			if isDiagnosticsPackage(imp) {
				violations = append(violations, fmt.Sprintf("%s imports diagnostics package %s", rel, imp))
			}
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf(
			"lifecycle code paths import %d diagnostics package(s):\n  %s\n"+
				"Diagnostics is read-only observability and must never gate lifecycle operations (ADR-036 §3). "+
				"Remove the import — no lifecycle transition may depend on a diagnostics result.",
			len(violations), strings.Join(violations, "\n  "))
	}
	return nil
}

// lifecycleInternalRoots lists every package under ./internal/... — the
// lifecycle layers (runtime, server, release, and their dependencies) —
// excluding the diagnostics packages themselves and this check package.
func lifecycleInternalRoots(workDir, goCmd string) ([]string, error) {
	out, err := runGo(workDir, goCmd, "list", "./internal/...")
	if err != nil {
		return nil, fmt.Errorf("enumerate internal packages: %w", err)
	}

	var roots []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" || isDiagnosticsPackage(pkg) || pkg == "maleolabs.com/anvil/internal/nongating" {
			continue
		}
		roots = append(roots, pkg)
	}
	sort.Strings(roots)
	return roots, nil
}

// commandFileImports returns the import paths declared by a single Go
// source file (relative to workDir), parsed with the Go parser so the
// result is syntactic fact, not string matching.
//
// The file must exist: the lifecycle command file list is load-bearing,
// and a missing file means the list (or the command layout) is stale.
func commandFileImports(workDir, rel string) ([]string, error) {
	path := filepath.Join(workDir, rel)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	if f == nil || len(f.Imports) == 0 {
		// A lifecycle command file with no imports would be a code smell,
		// but the real failure mode is a missing file — caught by
		// parser.ParseFile above. An empty import set is a valid result.
		return nil, nil
	}

	var imports []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("parse import path in %s: %w", rel, err)
		}
		imports = append(imports, path)
	}
	return imports, nil
}

// importGraph returns the transitive import graph of a single package
// (including the package itself), sorted. An error means the package or
// one of its dependencies cannot be loaded — the tree is broken and the
// check must fail.
func importGraph(workDir, goCmd, pkg string) ([]string, error) {
	out, err := runGo(workDir, goCmd, "list", "-deps", "-f", "{{.ImportPath}}", pkg)
	if err != nil {
		return nil, fmt.Errorf("resolve import graph of %s: %w", pkg, err)
	}

	var deps []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if dep := strings.TrimSpace(line); dep != "" {
			deps = append(deps, dep)
		}
	}
	sort.Strings(deps)
	return deps, nil
}

// runGo executes the Go toolchain in workDir. GOWORK is disabled so the
// check never resolves a workspace outside the module under inspection.
func runGo(workDir, goCmd string, args ...string) ([]byte, error) {
	cmd := exec.Command(goCmd, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", goCmd, strings.Join(args, " "), err, string(out))
	}
	return out, nil
}
