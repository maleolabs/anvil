// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: ST-P9-01, TS-P9-01
package inspection

import (
	"testing"

	"maleolabs.com/anvil/internal/config"
)

// TestVerificationEngine_Verify_ConfigInspectionRuns verifies that Verify
// runs the ConfigInspector when a *config.Resolver is provided, producing
// the config component with real checks (completeness, validity,
// resolution) — not the vacuous pass returned when the resolver is absent.
//
// Reference: ST-P9-01, TS-P9-01
func TestVerificationEngine_Verify_ConfigInspectionRuns(t *testing.T) {
	resolver := config.NewResolver(
		map[string]interface{}{"project.name": "test-app"},
		nil, nil, nil,
	)

	engine := NewVerificationEngine(
		nil, // runtime inspector skipped
		NewDefaultConfigInspector(),
		nil, nil, nil,
	)

	result := engine.Verify(resolver)

	var configComponent *InspectionResult
	for i := range result.ComponentResults {
		if result.ComponentResults[i].Component == "config" {
			configComponent = &result.ComponentResults[i]
			break
		}
	}

	if configComponent == nil {
		t.Fatal("Verify() did not produce a config component with a resolver")
	}

	checkNames := make(map[string]bool)
	for _, check := range configComponent.Checks {
		checkNames[check.Name] = true
	}
	for _, want := range []string{"completeness", "validity", "resolution"} {
		if !checkNames[want] {
			t.Errorf("config component missing check %q; got %v", want, checkNames)
		}
	}

	if !configComponent.Passed {
		t.Error("config component should pass for a valid resolver")
	}
}

// TestVerificationEngine_Verify_ConfigFailureAffectsStatus verifies that a
// failing config component (missing required values) is reflected in the
// consolidated health status — the platform must not be reported healthy
// when configuration is broken.
//
// Reference: ST-P9-01
func TestVerificationEngine_Verify_ConfigFailureAffectsStatus(t *testing.T) {
	// Empty resolver: project.name (required) is missing.
	resolver := config.NewResolver(nil, nil, nil, nil)

	engine := NewVerificationEngine(
		nil,
		NewDefaultConfigInspector(),
		nil, nil, nil,
	)

	result := engine.Verify(resolver)

	if result.Status != HealthStatusUnhealthy {
		t.Errorf("Verify().Status = %q, want %q (only config component, failing)", result.Status, HealthStatusUnhealthy)
	}

	found := false
	for _, component := range result.ComponentResults {
		if component.Component == "config" && !component.Passed {
			found = true
			for _, check := range component.Checks {
				if check.Name == "completeness" && !check.Passed {
					return // completeness check correctly failed
				}
			}
		}
	}
	if !found {
		t.Error("expected a failing config component with a failed completeness check")
	}
}

// TestVerificationEngine_Verify_NonResolverSkipped verifies that a
// non-*config.Resolver value is treated as "no resolver": config
// inspection is skipped and the config component is not reported.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_NonResolverSkipped(t *testing.T) {
	engine := NewVerificationEngine(
		nil,
		NewDefaultConfigInspector(),
		nil, nil, nil,
	)

	result := engine.Verify("not-a-resolver")

	for _, component := range result.ComponentResults {
		if component.Component == "config" {
			t.Error("Verify() should not produce a config component without a *config.Resolver")
		}
	}
}

// TestVerificationEngine_Verify_NilResolverSkipped verifies that a nil
// resolver skips config inspection entirely (component absent).
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_NilResolverSkipped(t *testing.T) {
	engine := NewVerificationEngine(
		nil,
		NewDefaultConfigInspector(),
		nil, nil, nil,
	)

	result := engine.Verify(nil)

	if len(result.ComponentResults) != 0 {
		t.Errorf("Verify() produced %d components with nil resolver, want 0", len(result.ComponentResults))
	}
}

// TestIssuesFromComponents verifies that IssuesFromComponents converts
// every failed check into a DiagnosticIssue using the component mapping,
// while ignoring passing components and passing checks.
//
// Reference: ST-P9-01, TS-009-003 §7
func TestIssuesFromComponents(t *testing.T) {
	components := []InspectionResult{
		{
			Component: "runtime",
			Checks: []InspectionCheck{
				{Name: "active_symlink", Passed: false, Details: "active symlink does not exist"},
				{Name: "shared_resources", Passed: true, Details: "all shared directories exist"},
			},
			Passed: false,
		},
		{
			Component: "release",
			Checks: []InspectionCheck{
				{Name: "artifact_presence", Passed: false, Details: "release directories without artifacts"},
			},
			Passed: false,
		},
		{
			Component: "config",
			Checks: []InspectionCheck{
				{Name: "completeness", Passed: true, Details: "all required values present"},
			},
			Passed: true,
		},
	}

	issues := IssuesFromComponents(components)

	if len(issues) != 2 {
		t.Fatalf("IssuesFromComponents() = %d issues, want 2", len(issues))
	}

	if issues[0].Component != "runtime" || issues[0].Location != "active_symlink" {
		t.Errorf("issue 0 = %+v, want runtime/active_symlink", issues[0])
	}
	if issues[0].Severity != SeverityCritical {
		t.Errorf("issue 0 severity = %q, want %q (missing state)", issues[0].Severity, SeverityCritical)
	}
	if issues[1].Component != "release" || issues[1].Location != "artifact_presence" {
		t.Errorf("issue 1 = %+v, want release/artifact_presence", issues[1])
	}
}
