// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-02, EPIC-003
package artifact

import "testing"

// TestDeployableContentDir_IsDefined verifies that DeployableContentDir is
// defined and non-empty.
func TestDeployableContentDir_IsDefined(t *testing.T) {
	if DeployableContentDir == "" {
		t.Error("DeployableContentDir must not be empty")
	}
}

// TestManifestFile_IsDefined verifies that ManifestFile is defined and
// non-empty.
func TestManifestFile_IsDefined(t *testing.T) {
	if ManifestFile == "" {
		t.Error("ManifestFile must not be empty")
	}
}

// TestArtifactStructure_ConstantsDiffer verifies that the two constants
// have different values (they point to different things).
func TestArtifactStructure_ConstantsDiffer(t *testing.T) {
	if DeployableContentDir == ManifestFile {
		t.Error("DeployableContentDir and ManifestFile must not be equal")
	}
}

// TestArtifactStructure_ManifestIsJSON verifies that ManifestFile has a
// .json extension as expected for a JSON manifest.
func TestArtifactStructure_ManifestIsJSON(t *testing.T) {
	if len(ManifestFile) < 5 || ManifestFile[len(ManifestFile)-5:] != ".json" {
		t.Errorf("ManifestFile %q should end with .json", ManifestFile)
	}
}
