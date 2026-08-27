package output

import (
	"strings"
	"testing"
)

func TestRedactSecrets_KeyPath(t *testing.T) {
	if got := RedactSecrets("/home/user/.ssh/id_ed25519"); got == "/home/user/.ssh/id_ed25519" {
		t.Fatalf("RedactSecrets should redact key path, got %q", got)
	}
	if got := RedactSecrets("DEPLOY_SSH_KEY is secret"); !strings.Contains(got, "REDACTED") {
		t.Fatalf("should redact DEPLOY_SSH_KEY, got %q", got)
	}
	if got := RedactSecrets("/tmp/mykey.pem"); !strings.Contains(got, "REDACTED") {
		t.Fatalf("should redact .pem, got %q", got)
	}
	if got := RedactSecrets("plain log message"); got != "plain log message" {
		t.Fatalf("plain should not be redacted, got %q", got)
	}
}

func TestSanitizeLogLine_EnvLeak(t *testing.T) {
	t.Setenv("DEPLOY_SSH_KEY", "super-secret-12345")
	line := "connect with DEPLOY_SSH_KEY=super-secret-12345 host=1.2.3.4"
	s := SanitizeLogLine(line)
	if strings.Contains(s, "super-secret-12345") {
		t.Fatalf("sanitized leaked secret: %q", s)
	}
	if !strings.Contains(s, "REDACTED") {
		t.Fatalf("sanitized should contain REDACTED, got %q", s)
	}
	// key path leak
	t.Setenv("DEPLOY_SSH_KEY", "")
	line2 := "using key /home/user/.ssh/id_ed25519 for host"
	s2 := SanitizeLogLine(line2)
	if strings.Contains(s2, "/home/user/.ssh/id_ed25519") {
		t.Fatalf("sanitized leaked key path: %q", s2)
	}
	// private key content
	line3 := "-----BEGIN OPENSSH PRIVATE KEY----- abc"
	s3 := SanitizeLogLine(line3)
	if !strings.Contains(s3, "REDACTED") {
		t.Fatalf("should redact private key, got %q", s3)
	}
	// Ensure host env also redacted
	t.Setenv("DEPLOY_SERVER_HOST", "203.0.113.10")
	line4 := "host=203.0.113.10"
	s4 := SanitizeLogLine(line4)
	if strings.Contains(s4, "203.0.113.10") {
		t.Fatalf("should redact DEPLOY_SERVER_HOST value: %q", s4)
	}
}

func TestSanitizeLogLine_UsesEnvValue(t *testing.T) {
	// When DEPLOY_SSH_KEY set, any occurrence of its value must be redacted
	t.Setenv("DEPLOY_SSH_KEY", "/tmp/secret-key")
	s := SanitizeLogLine("key is /tmp/secret-key")
	if strings.Contains(s, "/tmp/secret-key") {
		t.Fatalf("env secret leaked: %q", s)
	}
	if !strings.Contains(s, "REDACTED") {
		t.Fatalf("expected REDACTED, got %q", s)
	}
}
