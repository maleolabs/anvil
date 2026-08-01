// Tests for the Flutter adapter's declared capabilities (TS-P7-20,
// TS-P7-21): the deployment model, the absence of activation phases, and
// the build phases the capability declaration exposes to the Core.
package flutter

import (
	"reflect"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// TestFramework_IsFlutter verifies the adapter declares "flutter" as its
// framework name — the value a project records in its registry to select
// this adapter and the segment the Core resolves the executable by
// (TS-P7-20 AC-2, 005-adapter-command-contract §10).
//
// Reference: TS-P7-20 AC-2
func TestFramework_IsFlutter(t *testing.T) {
	if Framework != "flutter" {
		t.Errorf("Framework = %q, want %q", Framework, "flutter")
	}
}

// TestCapabilities_DeclaresDeploymentModel verifies that the capability
// declaration declares the "hybrid" deployment model — Flutter releases
// are built and packaged for distribution, not deployed to a server and
// activated in place (TS-P7-20 AC-3, ADR-016, EPIC-007 §7.3).
//
// Reference: TS-P7-20 AC-3, ADR-016
func TestCapabilities_DeclaresDeploymentModel(t *testing.T) {
	result := Capabilities()
	if result.Declaration.DeploymentModel != string(contracts.DeploymentModelHybrid) {
		t.Errorf("DeploymentModel = %q, want %q", result.Declaration.DeploymentModel, contracts.DeploymentModelHybrid)
	}
}

// TestCapabilities_NoActivationPhases verifies that the declaration
// lists no activation phases — the hybrid model has no server activation
// (TS-P7-20 AC-5, EPIC-007 §7.3). The `activate` command is absent from
// the dispatcher as well (command_test.go).
//
// Reference: TS-P7-20 AC-5
func TestCapabilities_NoActivationPhases(t *testing.T) {
	result := Capabilities()
	if len(result.Declaration.ActivationPhases) != 0 {
		t.Errorf("ActivationPhases = %v, want none for the hybrid model", result.Declaration.ActivationPhases)
	}
}

// TestCapabilities_DeclaresBuildPhases verifies that the capability
// declaration lists the three build targets in build execution order —
// web, apk, ios — matching the build target table (TS-P7-21 AC-4,
// TS-P7-20 AC-3).
//
// Reference: TS-P7-21 AC-4
func TestCapabilities_DeclaresBuildPhases(t *testing.T) {
	result := Capabilities()
	want := []string{TargetWeb, TargetApk, TargetIos}
	if !reflect.DeepEqual(result.Declaration.BuildPhases, want) {
		t.Errorf("BuildPhases = %v, want %v", result.Declaration.BuildPhases, want)
	}
}

// TestCapabilities_DeclaresVerificationChecks verifies that the
// declaration lists the two Flutter verification checks — pubspec_yaml
// and lib_directory (TS-P7-25) — and no diagnostic commands.
//
// Reference: TS-P7-20, TS-P7-25
func TestCapabilities_DeclaresVerificationChecks(t *testing.T) {
	result := Capabilities()
	checks := result.Declaration.VerificationChecks
	if len(checks) != 2 {
		t.Errorf("VerificationChecks = %v, want the two TS-P7-25 checks (pubspec_yaml, lib_directory)", checks)
	}
	for i, want := range []string{CheckPubspecYaml, CheckLibDirectory} {
		if checks[i].Name != want {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, want)
		}
	}
	if len(result.Declaration.DiagnosticCommands) != 0 {
		t.Errorf("DiagnosticCommands = %v, want none (owned by TS-P7-25/26)", result.Declaration.DiagnosticCommands)
	}
}
