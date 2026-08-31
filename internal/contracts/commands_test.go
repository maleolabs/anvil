// Tests for the stable command names of the adapter command contract
// (internal/contracts/commands.go). Command names are part of the stable
// contract — they never change between Core versions without a documented
// deprecation path (ADR-010 §9.5).
package contracts

import (
	"encoding/json"
	"testing"
)

// TestCommandConstants verifies the exact values of all command-name
// constants. The values are the wire contract — adapters and the Core
// interoperate only when both sides agree on these strings. A change to a
// value is a contract break; this test pins them.
func TestCommandConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "CommandCapabilities", got: CommandCapabilities, want: "capabilities"},
		{name: "CommandActivation", got: CommandActivation, want: "activate"},
		{name: "CommandVerification", got: CommandVerification, want: "verify"},
		{name: "CommandConfigExtension", got: CommandConfigExtension, want: "extension"},
		{name: "CommandConfigValidation", got: CommandConfigValidation, want: "validate"},
		{name: "CommandBuild", got: CommandBuild, want: "build"},
		{name: "CommandManifest", got: CommandManifest, want: "manifest"},
		{name: "CommandTemplate", got: CommandTemplate, want: "template"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestCommandConstantsDistinct verifies that every command name is unique —
// a duplicate would make command dispatch ambiguous for adapters.
func TestCommandConstantsDistinct(t *testing.T) {
	values := []string{
		CommandCapabilities,
		CommandActivation,
		CommandVerification,
		CommandConfigExtension,
		CommandConfigValidation,
		CommandBuild,
		CommandManifest,
		CommandTemplate,
	}
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		if _, dup := seen[v]; dup {
			t.Errorf("duplicate command name %q — command names must be unique", v)
		}
		seen[v] = struct{}{}
	}
}

// TestConfigPayloadRoundTrip verifies that the config extension contract
// payloads (TS-P7-03) survive a JSON round trip — the shape the Core and
// adapters exchange over the command contract.
func TestConfigPayloadRoundTrip(t *testing.T) {
	original := ConfigValidationRequest{
		Values: []ConfigValue{
			{Key: "framework.laravel.migrations.path", Value: "database/migrations"},
			{Key: "framework.laravel.cache.store", Value: "file"},
			{Key: "framework.laravel.version", Value: "11.0.0"},
		},
	}

	data := roundTrip(t, original)

	var decoded ConfigValidationRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ConfigValidationRequest: %v", err)
	}

	if len(decoded.Values) != len(original.Values) {
		t.Fatalf("decoded Values length = %d, want %d", len(decoded.Values), len(original.Values))
	}
	for i := range original.Values {
		if decoded.Values[i] != original.Values[i] {
			t.Errorf("decoded Values[%d] = %#v, want %#v", i, decoded.Values[i], original.Values[i])
		}
	}
}
