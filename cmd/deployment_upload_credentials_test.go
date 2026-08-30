// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-011-002, ST-P11-01, EPIC-011
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// This file closes the per-variable missing-credential gap identified
// in the ST-011-002 E2E verification audit: previously only the
// all-missing scenario was covered at command level
// (TestDeploymentUploadCommand_MissingCredentials). These tests verify
// that each required variable (DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER,
// DEPLOY_SSH_KEY), when unset or set-but-empty, produces a clear error
// whose reason names exactly that variable (ST-011-002 AC-3..AC-5,
// ST-P11-01 §3).
//
// The reason line is asserted, not just the raw variable name, because
// the command's Resolution guidance lists all three variables; the
// Reason is the part that specifically names the missing one.

// prepareUploadCredentialTest writes a valid artifact file for
// credential tests, returning the artifact path to use with
// executeCommand. No local server state is created: upload is
// env-only and works on a fresh runner (TD-006).
func prepareUploadCredentialTest(t *testing.T) (dir, artifactPath string) {
	t.Helper()
	dir = t.TempDir()

	artifactPath = filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}
	return dir, artifactPath
}

// unsetUploadEnvVar removes a single credential environment variable,
// restoring the previous value after the test. Tests using this helper
// must not call t.Parallel.
func unsetUploadEnvVar(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unsetenv %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

// setUploadEnvEmpty sets a single credential environment variable to an
// empty string (set-but-empty), restoring the previous value after the
// test. An empty credential is treated as missing: it is never usable,
// and reporting it as present would hide a CI/CD misconfiguration
// (TS-011-003, ADR-019).
func setUploadEnvEmpty(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Setenv(name, ""); err != nil {
		t.Fatalf("setenv %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

// TestDeploymentUploadCommand_MissingCredentialHost verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// with only DEPLOY_SERVER_HOST unset produces a clear error whose
// reason names exactly that variable (ST-011-002 AC-3).
//
// AC: Missing DEPLOY_SERVER_HOST produces clear error.
func TestDeploymentUploadCommand_MissingCredentialHost(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	unsetUploadEnvVar(t, envDeployServerHost)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for missing DEPLOY_SERVER_HOST, got nil")
	}
	if !contains(stderr, envDeployServerHost) {
		t.Errorf("stderr should name the missing variable DEPLOY_SERVER_HOST, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeployServerHost) {
		t.Errorf("stderr should report exactly DEPLOY_SERVER_HOST as missing, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_MissingCredentialUser verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// with only DEPLOY_SERVER_USER unset produces a clear error whose
// reason names exactly that variable (ST-011-002 AC-4).
//
// AC: Missing DEPLOY_SERVER_USER produces clear error.
func TestDeploymentUploadCommand_MissingCredentialUser(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	unsetUploadEnvVar(t, envDeployServerUser)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for missing DEPLOY_SERVER_USER, got nil")
	}
	if !contains(stderr, envDeployServerUser) {
		t.Errorf("stderr should name the missing variable DEPLOY_SERVER_USER, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeployServerUser) {
		t.Errorf("stderr should report exactly DEPLOY_SERVER_USER as missing, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_MissingCredentialKey verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// with only DEPLOY_SSH_KEY unset produces a clear error whose reason
// names exactly that variable (ST-011-002 AC-5).
//
// AC: Missing DEPLOY_SSH_KEY produces clear error.
func TestDeploymentUploadCommand_MissingCredentialKey(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	unsetUploadEnvVar(t, envDeploySSHKey)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for missing DEPLOY_SSH_KEY, got nil")
	}
	if !contains(stderr, envDeploySSHKey) {
		t.Errorf("stderr should name the missing variable DEPLOY_SSH_KEY, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeploySSHKey) {
		t.Errorf("stderr should report exactly DEPLOY_SSH_KEY as missing, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_EmptyCredentialHost verifies that a
// set-but-empty DEPLOY_SERVER_HOST is treated as missing and produces
// a clear error naming exactly that variable (ST-011-002 AC-3).
//
// AC: Missing DEPLOY_SERVER_HOST produces clear error (set-but-empty variant).
func TestDeploymentUploadCommand_EmptyCredentialHost(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	setUploadEnvEmpty(t, envDeployServerHost)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for empty DEPLOY_SERVER_HOST, got nil")
	}
	if !contains(stderr, envDeployServerHost) {
		t.Errorf("stderr should name the empty variable DEPLOY_SERVER_HOST, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeployServerHost) {
		t.Errorf("stderr should report exactly DEPLOY_SERVER_HOST as missing, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_EmptyCredentialUser verifies that a
// set-but-empty DEPLOY_SERVER_USER is treated as missing and produces
// a clear error naming exactly that variable (ST-011-002 AC-4).
//
// AC: Missing DEPLOY_SERVER_USER produces clear error (set-but-empty variant).
func TestDeploymentUploadCommand_EmptyCredentialUser(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	setUploadEnvEmpty(t, envDeployServerUser)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for empty DEPLOY_SERVER_USER, got nil")
	}
	if !contains(stderr, envDeployServerUser) {
		t.Errorf("stderr should name the empty variable DEPLOY_SERVER_USER, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeployServerUser) {
		t.Errorf("stderr should report exactly DEPLOY_SERVER_USER as missing, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_EmptyCredentialKey verifies that a
// set-but-empty DEPLOY_SSH_KEY is treated as missing and produces a
// clear error naming exactly that variable (ST-011-002 AC-5).
//
// AC: Missing DEPLOY_SSH_KEY produces clear error (set-but-empty variant).
func TestDeploymentUploadCommand_EmptyCredentialKey(t *testing.T) {
	_, artifactPath := prepareUploadCredentialTest(t)

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)
	setUploadEnvEmpty(t, envDeploySSHKey)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for empty DEPLOY_SSH_KEY, got nil")
	}
	if !contains(stderr, envDeploySSHKey) {
		t.Errorf("stderr should name the empty variable DEPLOY_SSH_KEY, got: %s", stderr)
	}
	if !contains(stderr, "missing SSH credential environment variables: "+envDeploySSHKey) {
		t.Errorf("stderr should report exactly DEPLOY_SSH_KEY as missing, got: %s", stderr)
	}
}
