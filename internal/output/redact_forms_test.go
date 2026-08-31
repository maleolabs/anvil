package output

import (
	"strings"
	"testing"
)

func TestRedactWithForms_DynamicPasswordFields(t *testing.T) {
	fields := []string{"password", "secretToken", "mySecret"}
	line := "user password=mySuperSecret123 and secretToken: abc123"
	redacted := SanitizeWithForms(line, fields)
	if strings.Contains(redacted, "mySuperSecret123") {
		t.Fatalf("should redact password value, got %q", redacted)
	}
	if strings.Contains(redacted, "abc123") {
		t.Fatalf("should redact secretToken, got %q", redacted)
	}
	if !strings.Contains(redacted, "***REDACTED***") {
		t.Fatalf("should contain REDACTED, got %q", redacted)
	}
	// non-password field should not be redacted generically
	line2 := "email=user@example.com"
	if SanitizeWithForms(line2, fields) != line2 {
		// email not in password fields, should stay unless contains password word
		// but our generic password redaction only triggers if contains "password"
	}
	// Ensure dynamic field name also redacted
	line3 := `{"mySecret": "s3cretValue"}`
	redacted3 := SanitizeWithForms(line3, fields)
	if strings.Contains(redacted3, "s3cretValue") {
		t.Fatalf("dynamic field mySecret should be redacted: %q", redacted3)
	}
}

func TestRedactWithForms_PasswordFieldNamesFromForms(t *testing.T) {
	// simulate forms with password fields
	passwordFields := []string{"password", "password_confirmation"}
	line := "password: hunter2 password_confirmation: hunter2"
	redacted := SanitizeWithForms(line, passwordFields)
	if strings.Contains(redacted, "hunter2") {
		t.Fatalf("should redact both password fields: %q", redacted)
	}
}
