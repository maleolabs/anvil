package deployment

import (
	"bytes"
	"maleolabs.com/anvil/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuard_DevAllowWithoutConfirm(t *testing.T) {
	creds := SSHCredentials{Host: "1.2.3.4", User: "deploy", KeyPath: "/tmp/key"}
	for _, env := range []string{"dev", "development", "local", "DEV", " Dev "} {
		if err := CheckDeployGuard(env, false, creds, nil, nil); err != nil {
			t.Errorf("dev env %q without confirm should PASS, got %v", env, err)
		}
		if err := CheckDeployGuardWithDryRun(env, false, false, creds, nil, nil); err != nil {
			t.Errorf("dev dryRun false without confirm should PASS, got %v", err)
		}
		// dry-run also PASS
		if err := CheckDeployGuardWithDryRun(env, false, true, creds, nil, nil); err != nil {
			t.Errorf("dev dryRun true without confirm should PASS, got %v", err)
		}
	}
}

func TestGuard_StagingRequiresConfirm(t *testing.T) {
	creds := SSHCredentials{Host: "1.2.3.4", User: "deploy", KeyPath: "/tmp/key"}
	// without confirm => REJECT
	if err := CheckDeployGuard("staging", false, creds, nil, nil); err == nil {
		t.Fatal("staging without --confirm should REJECT")
	} else if !strings.Contains(err.Error(), "--confirm") {
		t.Errorf("staging REJECT should mention --confirm, got %v", err)
	}
	// staging dry-run without confirm => PASS (verification-only)
	if err := CheckDeployGuardWithDryRun("staging", false, true, creds, nil, nil); err != nil {
		t.Errorf("staging dry-run without confirm should PASS, got %v", err)
	}
	// with confirm => PASS
	if err := CheckDeployGuard("staging", true, creds, nil, nil); err != nil {
		t.Errorf("staging with --confirm should PASS, got %v", err)
	}
	// prod-like other case-insensitive
	if err := CheckDeployGuard("STAGING", false, creds, nil, nil); err == nil {
		t.Error("STAGING uppercase without confirm should REJECT")
	}
}

func TestGuard_ProdCIEnforcement_AllowlistedAndConfirm(t *testing.T) {
	// Use isolated tmp for config
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(oldwd) }()

	// Ensure framework-free handling doesn't hide prod allowlist test — need actual target
	// Create anvil.yaml with prod target without allowlist
	xdg := filepath.Join(tmp, "xdg")
	_ = os.MkdirAll(filepath.Join(xdg, "anvil"), 0755)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: guard-prod-test
  version: 1.0.0
server:
  targets:
    prod:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
`), 0644)

	creds := SSHCredentials{Host: "203.0.113.10", User: "deploy", KeyPath: "/tmp/key", KnownHostsPath: "/tmp/kh", KnownHostsMode: KnownHostsModeStrict}

	// prod local without allowlist even with --confirm => REJECT (CI-only)
	if err := CheckDeployGuard("prod", true, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("prod local without allowlist should REJECT even with --confirm")
	} else if !strings.Contains(strings.ToLower(err.Error()), "allowlist") {
		t.Errorf("REJECT should mention allowlist, got %v", err)
	}

	// Now add allowlist with deploy principal
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: guard-prod-test
  version: 1.0.0
server:
  targets:
    prod:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
      allowlist:
        - deploy
`), 0644)
	// without --confirm still REJECT
	if err := CheckDeployGuard("prod", false, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("prod allowlisted without --confirm should REJECT")
	}
	// with --confirm but no stdin yes => REJECT (prompt expects yes)
	if err := CheckDeployGuard("prod", true, creds, bytes.NewBufferString("no\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("prod allowlisted with --confirm but answer no should REJECT")
	}
	// with --confirm + yes => PASS
	if err := CheckDeployGuard("prod", true, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err != nil {
		t.Errorf("prod allowlisted with --confirm + yes should PASS, got %v", err)
	}
	// also allowLocal bool
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: guard-prod-test
  version: 1.0.0
server:
  targets:
    prod:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
      allowLocal: true
`), 0644)
	if err := CheckDeployGuard("prod", true, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err != nil {
		t.Errorf("prod allowLocal true with yes should PASS, got %v", err)
	}

	// CI mode: allow even without allowlist (but still require --confirm)
	t.Setenv("CI", "true")
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: guard-prod-test
  version: 1.0.0
server:
  targets:
    prod:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
`), 0644)
	if err := CheckDeployGuard("prod", false, creds, nil, nil); err == nil {
		t.Error("prod CI without --confirm should still REJECT (confirm required)")
	}
	if err := CheckDeployGuard("prod", true, creds, nil, nil); err != nil {
		t.Errorf("prod CI with --confirm should PASS (no allowlist needed), got %v", err)
	}
	t.Setenv("CI", "")
	// Env var override allow
	t.Setenv("ANVIL_PROD_ALLOW_LOCAL", "true")
	if err := CheckDeployGuard("prod", true, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err != nil {
		t.Errorf("prod with ANVIL_PROD_ALLOW_LOCAL should PASS, got %v", err)
	}
	t.Setenv("ANVIL_PROD_ALLOW_LOCAL", "")
}

func TestGuard_NoRBACBypass_SpoofableStringIgnored(t *testing.T) {
	// Guard must bind to SSH principal, not DeployUser string; tampering creds.User is the only effective principal.
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(oldwd) }()
	xdg := filepath.Join(tmp, "xdg2")
	_ = os.MkdirAll(filepath.Join(xdg, "anvil"), 0755)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: rbac-test
  version: 1.0.0
server:
  targets:
    prod:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
      allowlist:
        - legitimate-user
`), 0644)
	spoofed := SSHCredentials{Host: "203.0.113.10", User: "attacker", KeyPath: "/tmp/key", KnownHostsPath: "/tmp/kh"}
	legit := SSHCredentials{Host: "203.0.113.10", User: "legitimate-user", KeyPath: "/tmp/key", KnownHostsPath: "/tmp/kh"}
	if err := CheckDeployGuard("prod", true, spoofed, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("attacker principal should REJECT")
	}
	if err := CheckDeployGuard("prod", true, legit, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err != nil {
		t.Fatalf("legitimate principal should PASS, got %v", err)
	}
}

func TestGuard_ProdDryRunBypassesConfirmAndAllowlist(t *testing.T) {
	creds := SSHCredentials{Host: "203.0.113.10", User: "deploy"}
	// even prod without confirm and without allowlist, dry-run => PASS
	if err := CheckDeployGuardWithDryRun("prod", false, true, creds, nil, nil); err != nil {
		t.Errorf("prod dry-run should PASS without confirm/allowlist, got %v", err)
	}
	if err := CheckDeployGuardWithDryRun("production", false, true, creds, nil, nil); err != nil {
		t.Errorf("production dry-run should PASS, got %v", err)
	}
}

func TestIsCI_Detection(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITLAB_CI", "")
	if IsCI() {
		t.Error("IsCI should be false when no CI vars")
	}
	t.Setenv("CI", "true")
	if !IsCI() {
		t.Error("IsCI should be true when CI=true")
	}
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "true")
	if !IsCI() {
		t.Error("IsCI should be true when GITHUB_ACTIONS=true")
	}
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITLAB_CI", "true")
	if !IsCI() {
		t.Error("IsCI should be true when GITLAB_CI=true")
	}
	t.Setenv("GITLAB_CI", "")
}

func TestProdLiveRequiresKnownHosts(t *testing.T) {
	// ClassifyEnv must treat live as prod
	for _, env := range []string{"live", "Live", "LIVE", " live ", "production", "prod"} {
		if got := ClassifyEnv(env); got != EnvProd {
			t.Errorf("ClassifyEnv(%q) = %q want prod", env, got)
		}
		if got := isProdEnvGuard(env); !got {
			t.Errorf("isProdEnvGuard(%q) should be true", env)
		}
	}
	// live must require knownHosts via config ExtractServerTargets (prod strict)
	flat := map[string]interface{}{
		"project.name":             "test-app",
		"server.targets.live.host": "203.0.113.10",
		"server.targets.live.user": "deploy",
	}
	_, errs := config.ExtractServerTargets(flat)
	found := false
	for _, e := range errs {
		if e.Key == "server.targets.live.knownHostsPath" {
			found = true
		}
	}
	if !found {
		t.Errorf("live without knownHostsPath should produce prod error, got %v", errs)
	}
	// Guard: live behaves like prod (CI-only default, requires allowlist + confirm)
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(oldwd) }()
	xdg := filepath.Join(tmp, "xdg-live")
	_ = os.MkdirAll(filepath.Join(xdg, "anvil"), 0755)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: guard-live-test
  version: 1.0.0
server:
  targets:
    live:
      host: 203.0.113.10
      user: deploy
      knownHostsPath: /tmp/kh
`), 0644)
	creds := SSHCredentials{Host: "203.0.113.10", User: "deploy"}
	if err := CheckDeployGuard("live", true, creds, bytes.NewBufferString("yes\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("live local without allowlist should REJECT (prod semantics)")
	}
}
