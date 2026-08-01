// Tests for the Laravel adapter's declared capabilities (TS-P7-13,
// TS-P7-14): the deployment model and the build phases the capability
// declaration exposes to the Core.
package laravel

import (
	"reflect"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// TestCapabilities_DeclaresDeploymentModel verifies that the capability
// declaration declares the "server" deployment model — Laravel releases
// deploy to a server and are activated in place (TS-P7-13 AC-3,
// ADR-016).
//
// Reference: TS-P7-13 AC-3, ADR-016
func TestCapabilities_DeclaresDeploymentModel(t *testing.T) {
	result := Capabilities()
	if result.Declaration.DeploymentModel != string(contracts.DeploymentModelServer) {
		t.Errorf("DeploymentModel = %q, want %q", result.Declaration.DeploymentModel, contracts.DeploymentModelServer)
	}
}

// TestCapabilities_DeclaresBuildPhases verifies that the capability
// declaration lists exactly the five build phases in build execution
// order — composer, npm, then the artisan optimization caches — matching
// the build phase table (TS-P7-14 AC-6).
//
// Reference: TS-P7-14 AC-6
func TestCapabilities_DeclaresBuildPhases(t *testing.T) {
	result := Capabilities()
	want := []string{PhaseComposer, PhaseNpm, PhaseConfigCache, PhaseRouteCache, PhaseViewCache}
	if !reflect.DeepEqual(result.Declaration.BuildPhases, want) {
		t.Errorf("BuildPhases = %v, want %v", result.Declaration.BuildPhases, want)
	}
}
