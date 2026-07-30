package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProvision_CreatesRuntime verifies that Provision returns a result with
// valid metadata including a unique RuntimeID.
//
// Reference: ST-P5-01 AC-1
func TestProvision_CreatesRuntime(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result, err := provisioner.Provision("test-runtime", EnvProduction, dir)
	if err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Provision() returned nil result")
	}

	// Verify metadata fields.
	if result.Metadata.Name != "test-runtime" {
		t.Errorf("Metadata.Name = %q, want %q", result.Metadata.Name, "test-runtime")
	}
	if result.Metadata.Environment != EnvProduction {
		t.Errorf("Metadata.Environment = %q, want %q", result.Metadata.Environment, EnvProduction)
	}
	if result.Metadata.InstallPath != dir {
		t.Errorf("Metadata.InstallPath = %q, want %q", result.Metadata.InstallPath, dir)
	}
	if result.Metadata.ID.String() == "" {
		t.Error("Metadata.ID is empty, expected a generated RuntimeID")
	}
	if err := ValidateRuntimeID(result.Metadata.ID.String()); err != nil {
		t.Errorf("Metadata.ID is not a valid RuntimeID: %v", err)
	}
}

// TestProvision_CreatesDirectories verifies that Provision creates the full
// directory structure under the install path.
//
// Reference: ST-P5-01 AC-2
func TestProvision_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	_, err := provisioner.Provision("dir-test", EnvStaging, dir)
	if err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	// Verify all directories were created.
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %s was not created: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("path %s exists but is not a directory", d)
		}
	}
}

// TestProvision_SetsProvisionedStage verifies that the lifecycle starts at
// StageProvisioned.
//
// Reference: ST-P5-01 AC-3
func TestProvision_SetsProvisionedStage(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result, err := provisioner.Provision("stage-test", EnvDevelopment, dir)
	if err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	if result.Lifecycle == nil {
		t.Fatal("Provision() returned nil Lifecycle")
	}

	if got := result.Lifecycle.Stage(); got != StageProvisioned {
		t.Errorf("Lifecycle.Stage() = %s, want %s", got, StageProvisioned)
	}
}

// TestProvision_RequiresName verifies that Provision returns an error when
// the name is empty.
//
// Reference: ST-P5-01
func TestProvision_RequiresName(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	_, err := provisioner.Provision("", EnvProduction, dir)
	if err == nil {
		t.Fatal("Provision() should have returned an error for empty name")
	}
}

// TestProvision_RequiresValidEnv verifies that Provision returns an error
// when an invalid environment type is provided.
//
// Reference: ST-P5-01
func TestProvision_RequiresValidEnv(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	_, err := provisioner.Provision("test-runtime", EnvironmentType("invalid"), dir)
	if err == nil {
		t.Fatal("Provision() should have returned an error for invalid environment type")
	}
}

// TestProvision_RequiresInstallPath verifies that Provision returns an error
// when the install path is empty.
//
// Reference: ST-P5-01
func TestProvision_RequiresInstallPath(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	_, err := provisioner.Provision("test-runtime", EnvProduction, "")
	if err == nil {
		t.Fatal("Provision() should have returned an error for empty install path")
	}
}

// TestProvision_UniqueIDs verifies that two Provision calls generate
// different RuntimeIDs.
//
// Reference: ST-P5-01 AC-1
func TestProvision_UniqueIDs(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result1, err := provisioner.Provision("runtime-a", EnvProduction, filepath.Join(dir, "a"))
	if err != nil {
		t.Fatalf("first Provision() failed: %v", err)
	}

	result2, err := provisioner.Provision("runtime-b", EnvProduction, filepath.Join(dir, "b"))
	if err != nil {
		t.Fatalf("second Provision() failed: %v", err)
	}

	if result1.Metadata.ID == result2.Metadata.ID {
		t.Error("two provisions generated the same RuntimeID")
	}
}

// TestRetire_TransitionsToRetired verifies that Retire transitions the
// lifecycle to StageRetired when the current stage allows it.
//
// Reference: ST-P5-01 AC-4
func TestRetire_TransitionsToRetired(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result, err := provisioner.Provision("retire-test", EnvProduction, dir)
	if err != nil {
		t.Fatalf("Provision() failed: %v", err)
	}

	// Transition through Provisioned → Ready first (valid transition).
	if err := result.Lifecycle.Transition(StageReady); err != nil {
		t.Fatalf("Transition to Ready failed: %v", err)
	}

	// Now Retire should succeed (Ready → Retired is valid).
	if err := provisioner.Retire(result.Lifecycle); err != nil {
		t.Fatalf("Retire() returned unexpected error: %v", err)
	}

	if got := result.Lifecycle.Stage(); got != StageRetired {
		t.Errorf("after Retire, Stage() = %s, want %s", got, StageRetired)
	}
}

// TestRetire_FromActive verifies that Retire also works from StageActive.
//
// Reference: ST-P5-01 AC-4
func TestRetire_FromActive(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result, err := provisioner.Provision("retire-active", EnvProduction, dir)
	if err != nil {
		t.Fatalf("Provision() failed: %v", err)
	}

	// Transition: Provisioned → Ready → Active.
	if err := result.Lifecycle.Transition(StageReady); err != nil {
		t.Fatalf("Transition to Ready failed: %v", err)
	}
	if err := result.Lifecycle.Transition(StageActive); err != nil {
		t.Fatalf("Transition to Active failed: %v", err)
	}

	// Retire from Active.
	if err := provisioner.Retire(result.Lifecycle); err != nil {
		t.Fatalf("Retire() from Active returned unexpected error: %v", err)
	}

	if got := result.Lifecycle.Stage(); got != StageRetired {
		t.Errorf("after Retire from Active, Stage() = %s, want %s", got, StageRetired)
	}
}

// TestRetire_FromProvisioned_Blocked verifies that Retire returns an error
// when called on a lifecycle in StageProvisioned (Provisioned → Retired is
// not a valid transition).
//
// Reference: ST-P5-01
func TestRetire_FromProvisioned_Blocked(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	result, err := provisioner.Provision("retire-blocked", EnvProduction, dir)
	if err != nil {
		t.Fatalf("Provision() failed: %v", err)
	}

	// Retire from Provisioned should fail.
	err = provisioner.Retire(result.Lifecycle)
	if err == nil {
		t.Fatal("Retire() from Provisioned should have returned an error")
	}

	if got := result.Lifecycle.Stage(); got != StageProvisioned {
		t.Errorf("after failed Retire, Stage() = %s, want %s", got, StageProvisioned)
	}
}

// TestNewProvisioner verifies that NewProvisioner returns a non-nil
// Provisioner.
func TestNewProvisioner(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	p := NewProvisioner(cfg)
	if p == nil {
		t.Fatal("NewProvisioner() returned nil")
	}
}

// TestProvision_MultipleRuntimes verifies that multiple Runtimes can be
// provisioned in separate directories without conflict.
func TestProvision_MultipleRuntimes(t *testing.T) {
	baseDir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	dirA := filepath.Join(baseDir, "runtime-a")
	dirB := filepath.Join(baseDir, "runtime-b")

	resultA, err := provisioner.Provision("runtime-a", EnvDevelopment, dirA)
	if err != nil {
		t.Fatalf("Provision(runtime-a) failed: %v", err)
	}

	resultB, err := provisioner.Provision("runtime-b", EnvStaging, dirB)
	if err != nil {
		t.Fatalf("Provision(runtime-b) failed: %v", err)
	}

	if resultA.Metadata.ID == resultB.Metadata.ID {
		t.Error("two provisions generated the same RuntimeID")
	}

	if resultA.Metadata.InstallPath == resultB.Metadata.InstallPath {
		t.Error("two provisions should have different install paths")
	}

	// Verify each runtime's directories exist.
	for _, dir := range []string{dirA, dirB} {
		cfg.InstallRoot = dir
		for _, d := range cfg.AllDirs() {
			if _, err := os.Stat(d); err != nil {
				t.Errorf("directory %s was not created: %v", d, err)
			}
		}
	}
}

// TestProvision_AllEnvTypes verifies that all valid environment types
// are accepted by Provision.
func TestProvision_AllEnvTypes(t *testing.T) {
	baseDir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	provisioner := NewProvisioner(cfg)

	envs := []EnvironmentType{EnvDevelopment, EnvStaging, EnvProduction}
	for _, env := range envs {
		t.Run(string(env), func(t *testing.T) {
			dir := filepath.Join(baseDir, string(env))
			result, err := provisioner.Provision("test-"+string(env), env, dir)
			if err != nil {
				t.Fatalf("Provision() with env %q failed: %v", env, err)
			}
			if result.Metadata.Environment != env {
				t.Errorf("Metadata.Environment = %q, want %q", result.Metadata.Environment, env)
			}
		})
	}
}
