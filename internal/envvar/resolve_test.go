// Package envvar resolves ${VAR} environment variable references in
// configuration values.
//
// Reference: TS-P11-02, TS-011-002, EPIC-011 §11.3, ADR-019
package envvar

import (
	"strings"
	"testing"
)

// TestResolve_WholeValueReference verifies that a value that is exactly
// "${VAR}" is substituted with the value of the VAR environment
// variable.
//
// Reference: TS-P11-02 AC-1, AC-2, ADR-019
func TestResolve_WholeValueReference(t *testing.T) {
	tests := []struct {
		name  string
		value string
		env   map[string]string
		want  string
	}{
		{
			name:  "host",
			value: "${DEPLOY_SERVER_HOST}",
			env:   map[string]string{"DEPLOY_SERVER_HOST": "203.0.113.10"},
			want:  "203.0.113.10",
		},
		{
			name:  "user",
			value: "${DEPLOY_SERVER_USER}",
			env:   map[string]string{"DEPLOY_SERVER_USER": "deploy"},
			want:  "deploy",
		},
		{
			name:  "port",
			value: "${DEPLOY_SERVER_PORT}",
			env:   map[string]string{"DEPLOY_SERVER_PORT": "2222"},
			want:  "2222",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			got, err := Resolve(tt.value)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// TestResolve_NoPlaceholder verifies that values without a "${...}"
// placeholder are returned unchanged.
//
// Reference: TS-P11-02 AC-1, ADR-019
func TestResolve_NoPlaceholder(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "plain_string", value: "127.0.0.1"},
		{name: "number", value: "22"},
		{name: "path", value: "/tmp/anvil-uploads"},
		{name: "dollar_without_braces", value: "$DEPLOY_SERVER_HOST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.value)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.value, err)
			}
			if got != tt.value {
				t.Errorf("Resolve(%q) = %q, want unchanged %q", tt.value, got, tt.value)
			}
		})
	}
}

// TestResolve_MissingVariable verifies that an unset variable produces
// an explicit error naming the variable, never a silent empty string.
//
// Reference: TS-P11-02 AC-3, EPIC-011 §11.3
func TestResolve_MissingVariable(t *testing.T) {
	const value = "${DEPLOY_SERVER_HOST}"
	_, err := Resolve(value)
	if err == nil {
		t.Fatalf("Resolve(%q) returned nil error, want explicit error", value)
	}
	if !strings.Contains(err.Error(), "DEPLOY_SERVER_HOST") {
		t.Errorf("error %q must name the missing variable", err)
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Errorf("error %q must state the variable is not set", err)
	}
}

// TestResolve_MalformedReference verifies that any value containing
// "${" that is not a whole-value "${VAR}" reference is rejected with
// an explicit error naming the original value. This prevents
// placeholders or secrets from leaking silently and enforces
// single-level-only resolution (EPIC-011 §11.3).
//
// Reference: TS-P11-02 AC-3, EPIC-011 §11.3
func TestResolve_MalformedReference(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty_braces", value: "${}"},
		{name: "unclosed", value: "${VAR"},
		{name: "trailing_text", value: "${VAR}extra"},
		{name: "leading_text", value: "prefix${VAR}"},
		{name: "embedded", value: "a${VAR}b"},
		{name: "double_reference", value: "${A}${B}"},
		{name: "nested", value: "${A${B}}"},
		{name: "stray_brace", value: "${VAR}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.value)
			if err == nil {
				t.Fatalf("Resolve(%q) returned nil error, want explicit error", tt.value)
			}
			if !strings.Contains(err.Error(), "partial or unsupported") {
				t.Errorf("error %q must state partial or unsupported reference", err)
			}
			if !strings.Contains(err.Error(), tt.value) {
				t.Errorf("error %q must mention the original value %q", err, tt.value)
			}
		})
	}
}

// TestResolve_EmptyButSet verifies that a variable that is set but
// empty resolves to "" without error — the operator explicitly set it.
//
// Reference: TS-P11-02 AC-2, ADR-019
func TestResolve_EmptyButSet(t *testing.T) {
	t.Setenv("DEPLOY_SERVER_PORT", "")
	got, err := Resolve("${DEPLOY_SERVER_PORT}")
	if err != nil {
		t.Fatalf("Resolve() returned error for set-but-empty variable: %v", err)
	}
	if got != "" {
		t.Errorf("Resolve() = %q, want empty string", got)
	}
}

// TestResolveAll_MixedValues verifies that a map mixing whole-value
// references and plain values resolves every entry correctly.
//
// Reference: TS-P11-02 AC-1, AC-2, ADR-019
func TestResolveAll_MixedValues(t *testing.T) {
	t.Setenv("DEPLOY_SERVER_HOST", "203.0.113.10")
	t.Setenv("DEPLOY_SERVER_USER", "deploy")

	values := map[string]string{
		"host": "${DEPLOY_SERVER_HOST}",
		"port": "2222",
		"user": "${DEPLOY_SERVER_USER}",
		"dir":  "/tmp/anvil-uploads",
	}

	resolved, err := ResolveAll(values)
	if err != nil {
		t.Fatalf("ResolveAll() returned error: %v", err)
	}

	want := map[string]string{
		"host": "203.0.113.10",
		"port": "2222",
		"user": "deploy",
		"dir":  "/tmp/anvil-uploads",
	}
	for key, wantValue := range want {
		if got := resolved[key]; got != wantValue {
			t.Errorf("ResolveAll()[%q] = %q, want %q", key, got, wantValue)
		}
	}
}

// TestResolveAll_ErrorPropagation verifies that a single unresolvable
// value fails the whole resolution with an explicit error, and that
// the input map is not mutated (no partial resolution leaks).
//
// Reference: TS-P11-02 AC-3
func TestResolveAll_ErrorPropagation(t *testing.T) {
	t.Setenv("DEPLOY_SERVER_HOST", "203.0.113.10")

	values := map[string]string{
		"host": "${DEPLOY_SERVER_HOST}",
		"key":  "${DEPLOY_SSH_KEY}",
		"port": "22",
	}

	_, err := ResolveAll(values)
	if err == nil {
		t.Fatal("ResolveAll() returned nil error, want error for missing variable")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DEPLOY_SSH_KEY") {
		t.Errorf("error %q must name the missing variable", msg)
	}
	if !strings.Contains(msg, "key") {
		t.Errorf("error %q must identify the failing map key", msg)
	}

	// The input map must be untouched despite the overall failure.
	if values["host"] != "${DEPLOY_SERVER_HOST}" {
		t.Error("input map mutated: host was resolved despite overall failure")
	}
}
