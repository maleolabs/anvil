// Tests for the manifest command payload (internal/contracts/manifest.go):
// the wire shape of the activation/rollback command strings exchanged
// between the Core and adapters at packaging time (TS-P7-15, TS-P7-16,
// 005-adapter-command-contract §10.10).
package contracts

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestManifestCommandResult_RoundTrip verifies that ManifestCommandResult
// survives a JSON round-trip with all fields equal — the shape the Core
// and adapters exchange over the command contract.
//
// Reference: TS-P7-15, TS-P7-16
func TestManifestCommandResult_RoundTrip(t *testing.T) {
	in := ManifestCommandResult{
		ActivationCommands: []string{"php artisan migrate --force", "php artisan config:cache"},
		RollbackCommands:   []string{"php artisan migrate:rollback"},
	}
	roundTrip(t, in)
}

// TestManifestCommandResult_JSONFieldNames verifies ManifestCommandResult
// serializes to the expected snake_case field names, aligned with
// artifact.Manifest (activation_commands, rollback_commands — ADR-017).
//
// Reference: TS-P7-15, TS-P7-16
func TestManifestCommandResult_JSONFieldNames(t *testing.T) {
	in := ManifestCommandResult{
		ActivationCommands: []string{"php artisan migrate --force"},
		RollbackCommands:   []string{"php artisan migrate:rollback"},
	}
	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"activation_commands", "rollback_commands"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ManifestCommandResult JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestManifestCommandResult_OmitsEmptySlices verifies the omitempty
// behavior: empty command slices are omitted, so a framework without
// server activation (the hybrid model, ADR-016 — Flutter) serializes to
// an empty document — the packaging layer then drops the fields from the
// manifest, keeping it backward compatible.
//
// Reference: TS-P7-15, TS-P7-16, ADR-016
func TestManifestCommandResult_OmitsEmptySlices(t *testing.T) {
	data := roundTrip(t, ManifestCommandResult{})
	if m := jsonKeys(t, data); len(m) != 0 {
		t.Errorf("empty result must serialize to {} (got %v)", m)
	}

	var parsed ManifestCommandResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal empty result: %v", err)
	}
	if !reflect.DeepEqual(parsed, ManifestCommandResult{}) {
		t.Errorf("parsed = %#v, want zero value", parsed)
	}
}
