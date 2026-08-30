// Package server provides models and utilities for managing Anvil Server
// Runtime configuration.
//
// Reference: TS-P5-11, TS-P5-12, ADR-013
package server

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultProjectRegistry verifies that DefaultProjectRegistry returns the
// expected compiled-in defaults.
func TestDefaultProjectRegistry(t *testing.T) {
	cfg := DefaultProjectRegistry()

	if cfg.Project.ID != "" {
		t.Errorf("DefaultProjectRegistry().Project.ID = %q, want empty string",
			cfg.Project.ID)
	}
	if cfg.Project.DisplayName != "" {
		t.Errorf("DefaultProjectRegistry().Project.DisplayName = %q, want empty string",
			cfg.Project.DisplayName)
	}
	if cfg.Project.InstallRoot != "" {
		t.Errorf("DefaultProjectRegistry().Project.InstallRoot = %q, want empty string",
			cfg.Project.InstallRoot)
	}
	if cfg.Project.Adapter != "" {
		t.Errorf("DefaultProjectRegistry().Project.Adapter = %q, want empty string",
			cfg.Project.Adapter)
	}
	if cfg.Project.Standard != "" {
		t.Errorf("DefaultProjectRegistry().Project.Standard = %q, want empty string",
			cfg.Project.Standard)
	}
}

// TestValidateProjectRegistry_Valid verifies that a valid project config
// passes validation without error.
func TestValidateProjectRegistry_Valid(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "my-project",
			DisplayName: "My Project",
			InstallRoot: "/var/www/my-project",
			Adapter:     "laravel",
		},
	}

	if err := ValidateProjectRegistry(cfg); err != nil {
		t.Errorf("ValidateProjectRegistry(valid config) returned unexpected error: %v", err)
	}
}

// TestValidateProjectRegistry_CanonicalStandardValid verifies that a
// project declaring the canonical project.standard key passes validation
// (TS-019-02-01).
func TestValidateProjectRegistry_CanonicalStandardValid(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "standard-project",
			DisplayName: "Standard Project",
			InstallRoot: "/var/www/standard-project",
			Standard:    "laravel",
		},
	}

	if err := ValidateProjectRegistry(cfg); err != nil {
		t.Errorf("ValidateProjectRegistry(project.standard) returned unexpected error: %v", err)
	}
}

// TestValidateProjectRegistry_BothStandardAndAdapter verifies that
// declaring both the canonical project.standard key and the legacy
// project.adapter key is rejected: the rename policy is explicit, never
// a silent preference (ADR-032, TS-019-02-01).
func TestValidateProjectRegistry_BothStandardAndAdapter(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "both-project",
			InstallRoot: "/var/www/both-project",
			Standard:    "laravel",
			Adapter:     "node",
		},
	}

	err := ValidateProjectRegistry(cfg)
	if err == nil {
		t.Fatal("ValidateProjectRegistry with both project.standard and project.adapter expected error, got nil")
	}
	if err != ErrStandardAdapterConflict {
		t.Errorf("ValidateProjectRegistry returned %v, want ErrStandardAdapterConflict", err)
	}
}

// TestValidateProjectRegistry_MissingID verifies that a project config with
// an empty project.id returns an error.
func TestValidateProjectRegistry_MissingID(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			InstallRoot: "/var/www/my-project",
		},
	}

	err := ValidateProjectRegistry(cfg)
	if err == nil {
		t.Error("ValidateProjectRegistry expected error for empty ID, got nil")
	}
	if err != ErrProjectIDRequired {
		t.Errorf("ValidateProjectRegistry returned %v, want ErrProjectIDRequired", err)
	}
}

// TestValidateProjectRegistry_MissingInstallRoot verifies that a project
// config with an empty project.install_root returns an error.
func TestValidateProjectRegistry_MissingInstallRoot(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "my-project",
			InstallRoot: "",
		},
	}

	err := ValidateProjectRegistry(cfg)
	if err == nil {
		t.Error("ValidateProjectRegistry expected error for empty InstallRoot, got nil")
	}
	if err != ErrInstallRootRequired {
		t.Errorf("ValidateProjectRegistry returned %v, want ErrInstallRootRequired", err)
	}
}

// TestValidateProjectRegistry_AllFields verifies that a project config with
// all fields set passes validation.
func TestValidateProjectRegistry_AllFields(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "full-project",
			DisplayName: "Full Project",
			InstallRoot: "/opt/projects/full",
			Adapter:     "node",
		},
	}

	if err := ValidateProjectRegistry(cfg); err != nil {
		t.Errorf("ValidateProjectRegistry(all fields) returned unexpected error: %v", err)
	}
}

// TestProjectRegistry_ValidateMethod verifies the convenience Validate method
// on ProjectRegistry delegates correctly.
func TestProjectRegistry_ValidateMethod(t *testing.T) {
	valid := ProjectRegistry{
		Project: ProjectSection{
			ID:          "validate-test",
			InstallRoot: "/opt/test",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() on valid project registry returned unexpected error: %v", err)
	}

	invalid := ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			InstallRoot: "",
		},
	}
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() on invalid project registry should return error, got nil")
	}
}

// TestProjectRegistry_String verifies the String method produces a readable
// summary.
func TestProjectRegistry_String(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "str-test",
			DisplayName: "String Test",
			InstallRoot: "/opt/str-test",
			Adapter:     "laravel",
		},
	}

	s := cfg.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
}

// TestProjectRegistry_MarshalUnmarshal verifies that ProjectRegistry can be
// marshaled to YAML and unmarshaled back without data loss.
func TestProjectRegistry_MarshalUnmarshal(t *testing.T) {
	original := ProjectRegistry{
		Project: ProjectSection{
			ID:          "test-project",
			DisplayName: "Test Project",
			InstallRoot: "/var/www/test",
			Adapter:     "laravel",
		},
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	var restored ProjectRegistry
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("yaml.Unmarshal returned unexpected error: %v", err)
	}

	if restored.Project.ID != original.Project.ID {
		t.Errorf("ID after round-trip = %q, want %q",
			restored.Project.ID, original.Project.ID)
	}
	if restored.Project.DisplayName != original.Project.DisplayName {
		t.Errorf("DisplayName after round-trip = %q, want %q",
			restored.Project.DisplayName, original.Project.DisplayName)
	}
	if restored.Project.InstallRoot != original.Project.InstallRoot {
		t.Errorf("InstallRoot after round-trip = %q, want %q",
			restored.Project.InstallRoot, original.Project.InstallRoot)
	}
	if restored.Project.Adapter != original.Project.Adapter {
		t.Errorf("Adapter after round-trip = %q, want %q",
			restored.Project.Adapter, original.Project.Adapter)
	}
	if restored.Project.Standard != original.Project.Standard {
		t.Errorf("Standard after round-trip = %q, want %q",
			restored.Project.Standard, original.Project.Standard)
	}
}

// TestProjectRegistry_MarshalUnmarshal_CanonicalStandard verifies that the
// canonical project.standard key round-trips through YAML and survives a
// Load-style unmarshal (TS-019-02-01).
func TestProjectRegistry_MarshalUnmarshal_CanonicalStandard(t *testing.T) {
	original := ProjectRegistry{
		Project: ProjectSection{
			ID:          "standard-project",
			DisplayName: "Standard Project",
			InstallRoot: "/var/www/standard",
			Standard:    "laravel",
		},
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}
	if !strings.Contains(string(data), "standard: laravel") {
		t.Errorf("marshaled registry does not carry the canonical standard key: %s", data)
	}

	var restored ProjectRegistry
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("yaml.Unmarshal returned unexpected error: %v", err)
	}
	if restored.Project.Standard != "laravel" {
		t.Errorf("Standard after round-trip = %q, want %q", restored.Project.Standard, "laravel")
	}
	if restored.Project.Adapter != "" {
		t.Errorf("Adapter after round-trip = %q, want empty (canonical key declared)", restored.Project.Adapter)
	}
}

// TestProjectRegistry_MarshalUnmarshal_OmitEmpty verifies that marshaling
// a project config with empty optional fields omits them from YAML output.
func TestProjectRegistry_MarshalUnmarshal_OmitEmpty(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "minimal-project",
			InstallRoot: "/opt/minimal",
			// DisplayName and Adapter omitted — should be omitted from YAML
			// output.
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	// Verify that optional fields are not present in the YAML output.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal to map returned unexpected error: %v", err)
	}

	projectSection, ok := raw["project"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'project' key in marshaled YAML")
	}

	if _, exists := projectSection["display_name"]; exists {
		t.Error("display_name should be omitted when empty, but was present in YAML output")
	}
	if _, exists := projectSection["adapter"]; exists {
		t.Error("adapter should be omitted when empty, but was present in YAML output")
	}
	if _, exists := projectSection["standard"]; exists {
		t.Error("standard should be omitted when empty, but was present in YAML output")
	}
}

// TestProjectRegistry_MarshalOmitsDemotedKeys verifies that the demoted
// ownership and shared-links configuration keys are never written by Anvil
// (ADR-031 §3, TS-015-04-02): the marshaled registry contains no owner,
// group, or shared_links keys.
func TestProjectRegistry_MarshalOmitsDemotedKeys(t *testing.T) {
	cfg := DefaultProjectRegistry()
	cfg.Project.ID = "demotion-test"
	cfg.Project.InstallRoot = "/opt/demotion-test"

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	for _, demotedKey := range []string{"owner", "group", "shared_links"} {
		if strings.Contains(string(data), demotedKey) {
			t.Errorf("marshaled registry contains demoted key %q — ownership/shared-links keys must never be written (ADR-031 §3)", demotedKey)
		}
	}
}

// TestProjectSection_StandardName_CanonicalKey verifies that resolution
// honors the canonical project.standard key when present (TS-019-02-01,
// ADR-032).
func TestProjectSection_StandardName_CanonicalKey(t *testing.T) {
	p := ProjectSection{Standard: "laravel"}
	if got := p.StandardName(); got != "laravel" {
		t.Errorf("StandardName() = %q, want %q", got, "laravel")
	}
}

// TestProjectSection_StandardName_LegacyFallback verifies that resolution
// falls back to the legacy project.adapter key during the deprecation
// window, so projects declaring project.adapter today keep working
// (TS-019-02-01, TS-019-02-02, ADR-032).
func TestProjectSection_StandardName_LegacyFallback(t *testing.T) {
	p := ProjectSection{Adapter: "node"}
	if got := p.StandardName(); got != "node" {
		t.Errorf("StandardName() = %q, want %q (legacy project.adapter honored)", got, "node")
	}
}

// TestProjectSection_StandardName_CanonicalWins verifies that when both
// keys are present (hand-edited registry file — registration validation
// rejects the combination), the canonical project.standard key wins
// deterministically.
func TestProjectSection_StandardName_CanonicalWins(t *testing.T) {
	p := ProjectSection{Standard: "laravel", Adapter: "node"}
	if got := p.StandardName(); got != "laravel" {
		t.Errorf("StandardName() = %q, want %q (canonical key wins)", got, "laravel")
	}
}

// TestProjectSection_StandardName_Empty verifies that a project declaring
// neither key resolves to an empty standard (no silent default).
func TestProjectSection_StandardName_Empty(t *testing.T) {
	p := ProjectSection{}
	if got := p.StandardName(); got != "" {
		t.Errorf("StandardName() = %q, want empty", got)
	}
}

// ── WarnIfLegacyAdapter (TS-019-02-02) ────────────────────────────────
//
// During the deprecation window the legacy project.adapter key is read
// as an alias: its value is honored and maps to project.standard
// semantics, and every read emits a deprecation warning naming
// project.standard (ADR-032 §7). These tests pin the window behavior
// (warning emitted, canonical-only projects untouched); at window end
// the removal flips this surface to post-removal expectations per
// TS-019-02-02 — see the removal checklist on
// StandardAdapterAliasWarning.

// TestProjectSection_WarnIfLegacyAdapter_EmitsWarning verifies that a
// project declaring the legacy project.adapter key gets a deprecation
// warning naming project.standard and pointing at the v2 migration
// guide (TS-019-02-02 DoD: alias-with-warning on read).
func TestProjectSection_WarnIfLegacyAdapter_EmitsWarning(t *testing.T) {
	p := ProjectSection{Adapter: "node"}
	var buf bytes.Buffer
	p.WarnIfLegacyAdapter(&buf)

	got := buf.String()
	if got == "" {
		t.Fatal("WarnIfLegacyAdapter must emit a warning for a project.adapter declaration")
	}
	if !strings.Contains(got, "project.adapter is deprecated") {
		t.Errorf("warning %q must announce the project.adapter deprecation", got)
	}
	if !strings.Contains(got, "project.standard") {
		t.Errorf("warning %q must name the replacement key project.standard", got)
	}
	if !strings.Contains(got, "docs/migration-guide-v2.md") {
		t.Errorf("warning %q must point at the v2 migration guide", got)
	}
	if strings.Contains(got, "ADR") || strings.Contains(got, "window") {
		t.Errorf("warning %q must not carry governance jargon in user-facing text", got)
	}
}

// TestProjectSection_WarnIfLegacyAdapter_AliasAlongsideCanonicalWarns
// verifies that a project declaring project.adapter alongside the
// canonical project.standard key (hand-edited registry file —
// registration validation rejects the combination at write) still gets
// the deprecation warning on read: the alongside case is not silent
// (TS-019-02-02: alias declared "only, or alongside" — every read
// warns).
func TestProjectSection_WarnIfLegacyAdapter_AliasAlongsideCanonicalWarns(t *testing.T) {
	p := ProjectSection{Standard: "laravel", Adapter: "node"}
	var buf bytes.Buffer
	p.WarnIfLegacyAdapter(&buf)

	if buf.String() == "" {
		t.Error("WarnIfLegacyAdapter must warn when project.adapter is declared alongside project.standard")
	}
	if got := p.StandardName(); got != "laravel" {
		t.Errorf("StandardName() = %q, want %q (canonical key still wins on read)", got, "laravel")
	}
}

// TestProjectSection_WarnIfLegacyAdapter_NoWarningForStandardOnly
// verifies that a project declaring only the canonical project.standard
// key never warns — the canonical path is unaffected by the
// deprecation and by the window-end removal.
func TestProjectSection_WarnIfLegacyAdapter_NoWarningForStandardOnly(t *testing.T) {
	p := ProjectSection{Standard: "laravel"}
	var buf bytes.Buffer
	p.WarnIfLegacyAdapter(&buf)

	if buf.String() != "" {
		t.Errorf("WarnIfLegacyAdapter wrote %q for a canonical-only project, want no warning", buf.String())
	}
}

// TestProjectSection_WarnIfLegacyAdapter_NoWarningForEmpty verifies that
// a project declaring neither key never warns.
func TestProjectSection_WarnIfLegacyAdapter_NoWarningForEmpty(t *testing.T) {
	p := ProjectSection{}
	var buf bytes.Buffer
	p.WarnIfLegacyAdapter(&buf)

	if buf.String() != "" {
		t.Errorf("WarnIfLegacyAdapter wrote %q for an empty project, want no warning", buf.String())
	}
}
