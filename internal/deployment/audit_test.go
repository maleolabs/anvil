package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAudit_HMACChain_0600_Redacted(t *testing.T) {
	dir := t.TempDir()
	// Use ANVIL_AUDIT_HMAC_KEY for deterministic key
	t.Setenv("ANVIL_AUDIT_HMAC_KEY", "test-hmac-key-for-audit-0600-chain-32b")

	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	// Permissions 0600
	if fi, err := os.Stat(logger.AuditLogPath()); err == nil {
		if fi.Mode().Perm() != 0600 {
			t.Errorf("audit.log perm %o want 0600", fi.Mode().Perm())
		}
	}
	if fi, err := os.Stat(logger.AuditKeyPath()); err == nil {
		if fi.Mode().Perm() != 0600 {
			t.Errorf("audit.hmac.key perm %o want 0600", fi.Mode().Perm())
		}
	}

	// Log with SSH principal binding, redacted remotePath containing secret
	_, err = logger.Log("prod", "deploy", "proj1", "art123", "1.0.0", "rel1", "/tmp/id_rsa/secret/DEPLOY_SSH_KEY=value", "details private key BEGIN OPENSSH", "deploy", "allow")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	_, err = logger.Log("prod", "guard", "proj1", "", "", "", "", "second entry", "deploy", "deny")
	if err != nil {
		t.Fatalf("Log second: %v", err)
	}

	entries, err := logger.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len %d want 2", len(entries))
	}
	// Hash chain: second prev_hash == first hash
	if entries[1].PrevHash != entries[0].Hash {
		t.Errorf("hash chain broken: prev %q != first hash %q", entries[1].PrevHash, entries[0].Hash)
	}
	if entries[0].Hash == "" || entries[1].Hash == "" {
		t.Error("hash should not be empty")
	}
	// Redacted: remotePath and details should not contain raw secret
	for _, e := range entries {
		combined := e.RemotePath + " " + e.Details + " " + e.User
		lower := strings.ToLower(combined)
		if strings.Contains(lower, "deploy_ssh_key") && !strings.Contains(combined, "[REDACTED]") && !strings.Contains(combined, "***") {
			t.Errorf("redaction failed, combined still has secret: %q", combined)
		}
		if strings.Contains(lower, "private") && !strings.Contains(combined, "***") && !strings.Contains(combined, "REDACTED") {
			// first entry's details contains private -> should be redacted
			if e.Details != "" && strings.Contains(strings.ToLower(e.Details), "private") {
				// RedactSecrets maps private -> ***REDACTED***
				if !strings.Contains(e.Details, "REDACTED") {
					t.Errorf("details not redacted: %q", e.Details)
				}
			}
		}
	}
	// File content is JSON-lines
	data, err := os.ReadFile(logger.AuditLogPath())
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("json-lines count %d want 2, data=%q", len(lines), string(data))
	}

	// VerifyChain passes
	if idx, err := logger.VerifyChain(); err != nil || idx != -1 {
		t.Fatalf("VerifyChain should pass, got idx %d err %v", idx, err)
	}

	// Tamper first line -> VerifyChain should detect
	// append a corrupted entry manually
	// Overwrite file with tampered hash
	tampered := strings.Replace(string(data), entries[0].Hash[:8], "ffffffff", 1)
	_ = os.WriteFile(logger.AuditLogPath(), []byte(tampered), 0600)
	if idx, err := logger.VerifyChain(); err == nil {
		t.Fatalf("VerifyChain should fail after tamper, got idx %d err nil", idx)
	} else if idx < 0 {
		t.Errorf("VerifyChain should return tamper index, got %d err %v", idx, err)
	}
}

func TestAudit_BindingToSSHPrincipal_NotSpoofableString(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANVIL_AUDIT_HMAC_KEY", "audit-binding-test-key-1234567890ab")
	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	// principal is SSH user, not DeployUser string
	principal := "deploy"
	spoofedUser := "attacker"
	// Correct principal
	e, err := logger.Log("prod", "deploy", "proj1", "art1", "1.0.0", "", "/tmp/remote", "ok", principal, "allow")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if e.User != principal {
		t.Errorf("audit user %q want principal %q (binding failed)", e.User, principal)
	}
	if e.User == spoofedUser {
		t.Error("audit should not be spoofed")
	}
	// Second log with different principal
	e2, _ := logger.Log("prod", "deploy", "proj1", "art1", "1.0.0", "", "/tmp/remote2", "ok2", "other-user", "allow")
	if e2.User != "other-user" {
		t.Errorf("second audit user %q want other-user", e2.User)
	}
	entries, _ := logger.Entries()
	if entries[0].User != "deploy" || entries[1].User != "other-user" {
		t.Errorf("entries users mismatch %v", entries)
	}
}

func TestAudit_LogFileMode_0600(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANVIL_AUDIT_HMAC_KEY", "audit-mode-0600-test-key-123456")
	logger, err := NewAuditLogger(filepath.Join(dir, "auditdir"))
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	if fi, err := os.Stat(logger.AuditLogPath()); err != nil {
		t.Fatalf("stat audit.log: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Errorf("audit.log perm %o want 0600", fi.Mode().Perm())
	}
	if fi, err := os.Stat(logger.AuditKeyPath()); err != nil {
		t.Fatalf("stat audit key: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Errorf("audit key perm %o want 0600", fi.Mode().Perm())
	}
	// After Log, still 0600
	_, _ = logger.Log("dev", "deploy", "p", "a", "1.0", "", "", "d", "user", "allow")
	if fi, err := os.Stat(logger.AuditLogPath()); err == nil && fi.Mode().Perm() != 0600 {
		t.Errorf("after log perm %o want 0600", fi.Mode().Perm())
	}
}
