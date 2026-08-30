// Package frameworkfree provides the Core framework-free proof (ADR-026
// decision 4): a check over the Anvil Runtime (Core) import graph that
// demonstrates zero framework packages. The check is the re-checkable
// evidence for the governance review (ADR-026 §3 decision 4, Transition
// Plan §11.2) — the import graph, not the claim.
//
// The check runs as part of `go test ./...` (TestCoreRuntimeIsFrameworkFree)
// and through the documented entry point scripts/framework-free-check.sh.
package frameworkfree

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// frameworkPackages are the distribution-content packages that carry
// framework knowledge. They must never appear in the Core runtime import
// graph (ADR-026 decision 1 and decision 4).
//
// The catalog is EMPTY: both first-party framework package sets left the
// Core module in the repository split (ADR-025 §6.2) — internal/laravel
// and cmd/laravel-adapter in TS-016-01-01, internal/flutter and
// cmd/flutter-adapter in TS-016-02-01. They now live in the
// anvil-standard-laravel and anvil-standard-flutter repositories, so no
// framework package can appear in the Core import graph at all.
//
// The list stays as the enforcement mechanism: framework packages are
// excluded from the Core runtime roots AND forbidden as dependencies of
// every Core root. Reintroducing a framework package
// (internal/<framework> or cmd/<framework>-adapter) requires adding it
// to this list — a Core package that imports it then fails this check.
// The governance review checks this list (ADR-026 §3, Transition Plan
// §11.2).
var frameworkPackages = []string{}

// isFrameworkPackage reports whether importPath is a framework package.
func isFrameworkPackage(importPath string) bool {
	for _, fp := range frameworkPackages {
		if importPath == fp {
			return true
		}
	}
	return false
}

// CheckCoreRuntime verifies that the Core runtime import graph contains
// zero framework packages.
//
// workDir must be the module root (the directory containing go.mod) of the
// Core repository; goCmd is the Go toolchain binary (usually "go"). The
// check enumerates every package under ./cmd/... and ./internal/... — the
// Core runtime roots — excluding the framework packages themselves, and
// verifies that none of the remaining roots transitively imports a
// framework package (ADR-026 decision 4; ADR-009 §8.1).
//
// It returns nil when the Core import graph is framework-free, or an error
// listing every violation with root attribution.
func CheckCoreRuntime(workDir, goCmd string) error {
	roots, err := coreRootPackages(workDir, goCmd)
	if err != nil {
		return err
	}

	var violations []string
	for _, root := range roots {
		deps, err := importGraph(workDir, goCmd, root)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if isFrameworkPackage(dep) {
				violations = append(violations, fmt.Sprintf("%s imports framework package %s", root, dep))
			}
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf(
			"Core runtime imports %d framework package(s):\n  %s\n"+
				"Framework packages are distribution content and must not be imported by the Anvil Runtime (ADR-026 decision 4). "+
				"Remove the import or move the package to the adapter layer (ADR-009 §8.1).",
			len(violations), strings.Join(violations, "\n  "))
	}
	return nil
}

// coreRootPackages lists every package under ./cmd/... and ./internal/...
// (the Core runtime roots), excluding the framework packages themselves —
// the adapter binaries and the framework capability packages are
// distribution content (ADR-009 §8.1), not Core runtime roots.
func coreRootPackages(workDir, goCmd string) ([]string, error) {
	out, err := runGo(workDir, goCmd, "list", "./cmd/...", "./internal/...")
	if err != nil {
		return nil, fmt.Errorf("enumerate Core packages: %w", err)
	}

	var roots []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" || isFrameworkPackage(pkg) {
			continue
		}
		roots = append(roots, pkg)
	}
	sort.Strings(roots)
	return roots, nil
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
