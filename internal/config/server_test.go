// Package config provides tests for server.targets configuration (sto:local-deploy-config AC1-AC4).
//
// Reference: sto:local-deploy-config, ADR-005, scp:local-deploy-mvp
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

func TestServerTarget_ValidIPAndHostname(t *testing.T) {
	validHosts := []string{"10.0.0.5", "192.168.1.10", "203.0.113.10", "example.com", "staging.example.com", "fe80::1"}
	for _, h := range validHosts {
		if err := ValidateHost(h); err != nil {
			t.Errorf("ValidateHost(%q) should pass, got %v", h, err)
		}
	}
	invalidHosts := []string{"", "not a host!!", "999.999.999.999", "bad_host!", "host with space"}
	for _, h := range invalidHosts {
		if err := ValidateHost(h); err == nil {
			t.Errorf("ValidateHost(%q) should fail", h)
		}
	}
}

func TestServerTarget_ValidUser(t *testing.T) {
	validUsers := []string{"deploy", "deploy_user", "user-1", "a"}
	for _, u := range validUsers {
		if err := ValidateUser(u); err != nil {
			t.Errorf("ValidateUser(%q) should pass, got %v", u, err)
		}
	}
	invalidUsers := []string{"", "-bad", ".bad", "user with space"}
	for _, u := range invalidUsers {
		if err := ValidateUser(u); err == nil {
			t.Errorf("ValidateUser(%q) should fail", u)
		}
	}
}

func TestExtractServerTargets_ValidStaging(t *testing.T) {
	flat := map[string]interface{}{
		"project.name":                          "test-app",
		"server.targets.staging.host":           "192.168.1.10",
		"server.targets.staging.user":           "deploy",
		"server.targets.staging.sshKeyPath":     "/tmp/id_ed25519",
		"server.targets.staging.knownHostsPath": "/tmp/known_hosts",
	}
	targets, errs := ExtractServerTargets(flat)
	if len(errs) != 0 {
		t.Fatalf("valid staging should have no errs, got %v", errs)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	st, ok := targets["staging"]
	if !ok || st.Host != "192.168.1.10" || st.User != "deploy" {
		t.Fatalf("unexpected staging target %+v", st)
	}
}

func TestExtractServerTargets_InvalidHost(t *testing.T) {
	flat := map[string]interface{}{
		"project.name":                "test-app",
		"server.targets.staging.host": "not a host!!",
		"server.targets.staging.user": "deploy",
	}
	_, errs := ExtractServerTargets(flat)
	if len(errs) == 0 {
		t.Fatal("invalid host should produce error")
	}
	found := false
	for _, e := range errs {
		if e.Key == "server.targets.staging.host" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error for server.targets.staging.host, got %v", errs)
	}
}

func TestExtractServerTargets_ProdMissingKnownHosts_AC2(t *testing.T) {
	flat := map[string]interface{}{
		"project.name":                   "test-app",
		"server.targets.production.host": "203.0.113.10",
		"server.targets.production.user": "deploy",
	}
	_, errs := ExtractServerTargets(flat)
	if len(errs) == 0 {
		t.Fatal("prod missing knownHostsPath should fail AC2")
	}
	found := false
	for _, e := range errs {
		if e.Key == "server.targets.production.knownHostsPath" && strings.Contains(e.Expected, "prod") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected prod knownHostsPath required error, got %v", errs)
	}
}

func TestExtractServerTargets_ProdAcceptNewRejected_AC2(t *testing.T) {
	flat := map[string]interface{}{
		"project.name":                       "test-app",
		"server.targets.prod.host":           "203.0.113.10",
		"server.targets.prod.user":           "deploy",
		"server.targets.prod.knownHostsPath": "/tmp/kh",
		"server.targets.prod.knownHostsMode": "accept-new",
	}
	_, errs := ExtractServerTargets(flat)
	if len(errs) == 0 {
		t.Fatal("prod accept-new should be rejected AC2")
	}
	found := false
	for _, e := range errs {
		if e.Key == "server.targets.prod.knownHostsMode" && strings.Contains(e.Expected, "strict") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected prod strict error, got %v", errs)
	}
}

func TestEffectiveSSHKeyPath_OverrideAndAgent(t *testing.T) {
	t.Setenv("DEPLOY_SSH_KEY", "super-secret-key-12345")
	if got := EffectiveSSHKeyPath("/tmp/cfg/key"); got != "super-secret-key-12345" {
		t.Fatalf("override failed got %q", got)
	}
	// Redacted in logs
	line := "connect DEPLOY_SSH_KEY=super-secret-key-12345 host=1.2.3.4"
	s := output.SanitizeLogLine(line)
	if strings.Contains(s, "super-secret") {
		t.Fatalf("sanitized leaked secret: %q", s)
	}
	t.Setenv("DEPLOY_SSH_KEY", "")
	if got := EffectiveSSHKeyPath("/tmp/cfg/key"); got != "/tmp/cfg/key" {
		t.Fatalf("cfg path failed got %q", got)
	}
	if got := EffectiveSSHKeyPath(""); got != "" {
		t.Fatalf("agent empty should be '', got %q", got)
	}
}

func TestExtractServerTargets_FrameworkFreeAC4(t *testing.T) {
	flat := map[string]interface{}{"project.name": "free"}
	_, errs := ExtractServerTargets(flat)
	if len(errs) != 0 {
		t.Fatalf("framework-free empty should have 0 errs, got %v", errs)
	}
}

func TestValidateServerTargetsInFlat_Integration(t *testing.T) {
	tmp := t.TempDir()
	oldwd, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(oldwd) }()
	xdg := filepath.Join(tmp, "xdg")
	_ = os.MkdirAll(filepath.Join(xdg, "anvil"), 0755)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: harness-test
  version: 1.0.0
server:
  targets:
    staging:
      host: 192.168.1.10
      user: deploy
      sshKeyPath: /tmp/key
      knownHostsPath: /tmp/kh
`), 0644)
	errs, err := ResolveAndValidate()
	if err != nil {
		t.Fatalf("valid staging resolve err %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("valid staging should have 0 errs, got %v", errs)
	}
	_ = os.WriteFile(filepath.Join(tmp, "anvil.yaml"), []byte(`
project:
  name: harness-test
  version: 1.0.0
server:
  targets:
    production:
      host: 203.0.113.10
      user: deploy
`), 0644)
	errs, err = ResolveAndValidate()
	if err != nil {
		t.Fatalf("prod missing kh resolve err %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("prod missing kh should have errs")
	}
}

func TestProdLiveRequiresKnownHosts(t *testing.T) {
	for _, env := range []string{"live", "Live", "LIVE", " live "} {
		if !isProdEnv(env) {
			t.Errorf("isProdEnv(%q) should be true (live is prod)", env)
		}
	}
	for _, env := range []string{"prod", "production", "live"} {
		flat := map[string]interface{}{
			"project.name":                    "test-app",
			"server.targets." + env + ".host": "203.0.113.10",
			"server.targets." + env + ".user": "deploy",
		}
		_, errs := ExtractServerTargets(flat)
		if len(errs) == 0 {
			t.Fatalf("%s without knownHostsPath should fail (prod live requires knownHosts)", env)
		}
		found := false
		for _, e := range errs {
			if e.Key == "server.targets."+env+".knownHostsPath" && strings.Contains(e.Expected, "prod") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %s knownHostsPath prod error, got %v", env, errs)
		}
	}
	// live accept-new must also be rejected like prod
	flat := map[string]interface{}{
		"project.name":                       "test-app",
		"server.targets.live.host":           "203.0.113.10",
		"server.targets.live.user":           "deploy",
		"server.targets.live.knownHostsPath": "/tmp/kh",
		"server.targets.live.knownHostsMode": "accept-new",
	}
	_, errs := ExtractServerTargets(flat)
	if len(errs) == 0 {
		t.Fatal("live accept-new should be rejected")
	}
	found := false
	for _, e := range errs {
		if e.Key == "server.targets.live.knownHostsMode" && strings.Contains(e.Expected, "strict") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected live strict error, got %v", errs)
	}
}
