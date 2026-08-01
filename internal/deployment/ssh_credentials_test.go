// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P11-03, TS-011-003, EPIC-011, ADR-019
package deployment

import (
	"os"
	"strings"
	"testing"
)

// setEnv sets an environment variable and restores it after the test.
func setEnv(t *testing.T, name, value string) {
	t.Helper()
	t.Setenv(name, value)
}

// unsetEnv removes an environment variable and restores the previous
// value after the test. Tests using this helper must not call
// t.Parallel (consistent with t.Setenv semantics).
func unsetEnv(t *testing.T, name string) {
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

// TestReadSSHCredentialsFromEnv_AllPresent verifies that complete
// credentials are read correctly (TS-011-003 AC-1).
func TestReadSSHCredentialsFromEnv_AllPresent(t *testing.T) {
	setEnv(t, EnvDeployServerHost, "10.0.0.5")
	setEnv(t, EnvDeployServerUser, "deploy")
	setEnv(t, EnvDeploySSHKey, "/keys/id_ed25519")

	creds, err := ReadSSHCredentialsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Host != "10.0.0.5" {
		t.Errorf("Host = %q, want %q", creds.Host, "10.0.0.5")
	}
	if creds.User != "deploy" {
		t.Errorf("User = %q, want %q", creds.User, "deploy")
	}
	if creds.KeyPath != "/keys/id_ed25519" {
		t.Errorf("KeyPath = %q, want %q", creds.KeyPath, "/keys/id_ed25519")
	}
	if creds.Port != DefaultSSHPort {
		t.Errorf("Port = %d, want default %d", creds.Port, DefaultSSHPort)
	}
}

// TestReadSSHCredentialsFromEnv_AllMissing verifies that a missing
// required variable produces a clear error naming the variable
// (TS-011-003 AC-3, ST-P11-01 §3).
func TestReadSSHCredentialsFromEnv_AllMissing(t *testing.T) {
	unsetEnv(t, EnvDeployServerHost)
	unsetEnv(t, EnvDeployServerUser)
	unsetEnv(t, EnvDeploySSHKey)

	_, err := ReadSSHCredentialsFromEnv()
	if err == nil {
		t.Fatal("expected error when all required variables are missing, got nil")
	}
	for _, name := range []string{EnvDeployServerHost, EnvDeployServerUser, EnvDeploySSHKey} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q should name missing variable %s", err.Error(), name)
		}
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q should say 'missing'", err.Error())
	}
}

// TestReadSSHCredentialsFromEnv_PartialMissing verifies that only the
// actually missing variables are reported (TS-011-003 AC-3).
func TestReadSSHCredentialsFromEnv_PartialMissing(t *testing.T) {
	setEnv(t, EnvDeployServerHost, "10.0.0.5")
	setEnv(t, EnvDeployServerUser, "deploy")
	unsetEnv(t, EnvDeploySSHKey)

	_, err := ReadSSHCredentialsFromEnv()
	if err == nil {
		t.Fatal("expected error when DEPLOY_SSH_KEY is missing, got nil")
	}
	if strings.Contains(err.Error(), EnvDeployServerHost) {
		t.Errorf("error %q should not name %s (it was set)", err.Error(), EnvDeployServerHost)
	}
	if strings.Contains(err.Error(), EnvDeployServerUser) {
		t.Errorf("error %q should not name %s (it was set)", err.Error(), EnvDeployServerUser)
	}
	if !strings.Contains(err.Error(), EnvDeploySSHKey) {
		t.Errorf("error %q should name missing variable %s", err.Error(), EnvDeploySSHKey)
	}
}

// TestReadSSHCredentialsFromEnv_SetButEmptyIsMissing verifies that a
// set-but-empty required variable is treated as missing: an empty
// credential is never usable, and reporting it as present would hide a
// CI/CD misconfiguration.
func TestReadSSHCredentialsFromEnv_SetButEmptyIsMissing(t *testing.T) {
	setEnv(t, EnvDeployServerHost, "")
	setEnv(t, EnvDeployServerUser, "deploy")
	setEnv(t, EnvDeploySSHKey, "/keys/id_ed25519")

	_, err := ReadSSHCredentialsFromEnv()
	if err == nil {
		t.Fatal("expected error when DEPLOY_SERVER_HOST is empty, got nil")
	}
	if !strings.Contains(err.Error(), EnvDeployServerHost) {
		t.Errorf("error %q should name %s", err.Error(), EnvDeployServerHost)
	}
}

// TestReadSSHCredentialsFromEnv_CustomPort verifies that
// DEPLOY_SERVER_PORT overrides the default (TS-011-003 AC-4).
func TestReadSSHCredentialsFromEnv_CustomPort(t *testing.T) {
	setEnv(t, EnvDeployServerHost, "10.0.0.5")
	setEnv(t, EnvDeployServerUser, "deploy")
	setEnv(t, EnvDeploySSHKey, "/keys/id_ed25519")
	setEnv(t, EnvDeployServerPort, "2222")

	creds, err := ReadSSHCredentialsFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Port != 2222 {
		t.Errorf("Port = %d, want 2222", creds.Port)
	}
}

// TestReadSSHCredentialsFromEnv_InvalidPort verifies that a non-numeric
// or out-of-range DEPLOY_SERVER_PORT produces a clear error.
func TestReadSSHCredentialsFromEnv_InvalidPort(t *testing.T) {
	setEnv(t, EnvDeployServerHost, "10.0.0.5")
	setEnv(t, EnvDeployServerUser, "deploy")
	setEnv(t, EnvDeploySSHKey, "/keys/id_ed25519")

	for _, raw := range []string{"not-a-port", "0", "-1", "70000", "22.5"} {
		setEnv(t, EnvDeployServerPort, raw)
		_, err := ReadSSHCredentialsFromEnv()
		if err == nil {
			t.Errorf("expected error for DEPLOY_SERVER_PORT=%q, got nil", raw)
			continue
		}
		if !strings.Contains(err.Error(), EnvDeployServerPort) {
			t.Errorf("error %q should name %s", err.Error(), EnvDeployServerPort)
		}
	}
}
