package skillbundle

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ── Test helpers ─────────────────────────────────────────────────────

// validManifest returns a valid manifest document.
func validManifest() map[string]any {
	return map[string]any{
		"name":            "anvil-cli-usage",
		"version":         "1.0.0",
		"source":          "anvil",
		"contractVersion": "1.0.0",
		"description":     "How to use the Anvil CLI",
		"files": []string{
			"anvil-cli-usage/SKILL.md",
			"anvil-cli-usage/lifecycle.md",
		},
	}
}

// manifestBytes marshals a manifest document map to JSON bytes.
func manifestBytes(m map[string]any) []byte {
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return data
}

// validManifestBytes returns the JSON bytes of a valid manifest.
func validManifestBytes() []byte { return manifestBytes(validManifest()) }

// wantManifestError asserts that ParseManifest rejects the document with
// a problem on the given field (or, when field is empty, at all), and
// that errors.As reaches a *ManifestError.
func wantManifestError(t *testing.T, data []byte, field, messageSubstr string) {
	t.Helper()
	_, err := ParseManifest(data)
	if err == nil {
		t.Fatalf("ParseManifest accepted the document; want a rejection (field %q)", field)
	}
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("errors.As(*ManifestError) failed; got %T: %v", err, err)
	}
	if field != "" {
		for _, ve := range me.Errors {
			if ve.Field == field {
				if messageSubstr != "" && !strings.Contains(ve.Message, messageSubstr) {
					t.Fatalf("field %q problem message %q does not contain %q", field, ve.Message, messageSubstr)
				}
				return
			}
		}
		t.Fatalf("no rejection on field %q; got %v", field, me.Error())
	}
}

// ── Happy path ───────────────────────────────────────────────────────

func TestParseManifest_Valid(t *testing.T) {
	md, err := ParseManifest(validManifestBytes())
	if err != nil {
		t.Fatalf("ParseManifest rejected a valid manifest: %v", err)
	}
	if md.Name != "anvil-cli-usage" || md.Version != "1.0.0" || md.Source != "anvil" ||
		md.ContractVersion != "1.0.0" || md.Description == "" {
		t.Fatalf("parsed manifest mismatch: %+v", md)
	}
	if len(md.Files) != 2 {
		t.Fatalf("Files = %v, want 2 entries", md.Files)
	}
	if md.SkillRoot() != "anvil-cli-usage" || md.SkillMarkdownPath() != "anvil-cli-usage/SKILL.md" {
		t.Fatalf("SkillRoot/SkillMarkdownPath mismatch: %q %q", md.SkillRoot(), md.SkillMarkdownPath())
	}
}

func TestParseManifest_RequiredFields(t *testing.T) {
	for _, field := range []string{"name", "version", "source", "contractVersion", "description", "files"} {
		m := validManifest()
		delete(m, field)
		wantManifestError(t, manifestBytes(m), field, "required field is missing")
	}
}

func TestParseManifest_UnknownFields(t *testing.T) {
	m := validManifest()
	m["extraField"] = "nope"
	wantManifestError(t, manifestBytes(m), "extraField", "unknown field")
}

func TestParseManifest_NamePattern(t *testing.T) {
	for _, bad := range []string{"Bad_Name", "-foo", "foo_bar", "Foo", "foo bar", "foo/bar", "foo..bar"} {
		m := validManifest()
		m["name"] = bad
		wantManifestError(t, manifestBytes(m), "name", "skill name convention")
	}
	// Hyphens are allowed inside and trailing; digits are allowed after a
	// valid first character; leading digits are allowed.
	for _, good := range []string{"foo-", "9-fine-9", "a", "a0"} {
		m := validManifest()
		m["name"] = good
		m["files"] = []string{good + "/SKILL.md"}
		if _, err := ParseManifest(manifestBytes(m)); err != nil {
			t.Errorf("ParseManifest rejected valid name %q: %v", good, err)
		}
	}
	m := validManifest()
	m["name"] = strings.Repeat("a", MaxNameLength+1)
	wantManifestError(t, manifestBytes(m), "name", "cap")
}

func TestParseManifest_NameTypes(t *testing.T) {
	m := validManifest()
	m["name"] = 42
	wantManifestError(t, manifestBytes(m), "name", "must be a string")
	m["name"] = nil
	wantManifestError(t, manifestBytes(m), "name", "found null")
}

func TestParseManifest_VersionPattern(t *testing.T) {
	for _, bad := range []string{"1.0", "v1.0.0", "01.0.0", "1.0.0-beta", "1.0.0.0", "1..0", "", "1.0.0 "} {
		m := validManifest()
		m["version"] = bad
		wantManifestError(t, manifestBytes(m), "version", "semver")
	}
	// Leading zeros in the major are rejected (no leading zeros rule).
	m := validManifest()
	m["version"] = "01.2.3"
	wantManifestError(t, manifestBytes(m), "version", "semver")
}

func TestParseManifest_SourcePattern(t *testing.T) {
	for _, bad := range []string{"Bad_Source", "-x", "x y"} {
		m := validManifest()
		m["source"] = bad
		wantManifestError(t, manifestBytes(m), "source", "standard-id convention")
	}
}

func TestParseManifest_ContractVersion(t *testing.T) {
	for _, bad := range []string{"2", "1.0", "1.0.0-beta"} {
		m := validManifest()
		m["contractVersion"] = bad
		wantManifestError(t, manifestBytes(m), "contractVersion", "semver")
	}
	// Unsupported contract major is rejected (ADR-024 §3.1: the contract
	// major is the unit of compatibility).
	m := validManifest()
	m["contractVersion"] = "2.0.0"
	wantManifestError(t, manifestBytes(m), "contractVersion", "supports major 1")
}

func TestParseManifest_Description(t *testing.T) {
	m := validManifest()
	m["description"] = "   "
	wantManifestError(t, manifestBytes(m), "description", "must not be empty")
	m = validManifest()
	m["description"] = strings.Repeat("x", MaxDescriptionLength+1)
	wantManifestError(t, manifestBytes(m), "description", "cap")
}

func TestParseManifest_FilesRules(t *testing.T) {
	// Empty inventory.
	m := validManifest()
	m["files"] = []string{}
	wantManifestError(t, manifestBytes(m), "files", "at least one entry")

	// Duplicate entries.
	m = validManifest()
	m["files"] = []string{"anvil-cli-usage/SKILL.md", "anvil-cli-usage/SKILL.md"}
	wantManifestError(t, manifestBytes(m), "files[1]", "duplicate")

	// Missing <name>/SKILL.md.
	m = validManifest()
	m["files"] = []string{"anvil-cli-usage/lifecycle.md"}
	wantManifestError(t, manifestBytes(m), "files", "must contain")

	// Non-string entry.
	m = validManifest()
	m["files"] = []any{"anvil-cli-usage/SKILL.md", 7}
	wantManifestError(t, manifestBytes(m), "files[1]", "must be a string")
}

func TestParseManifest_FilesPathSafety(t *testing.T) {
	bad := []string{
		"../evil",
		"anvil-cli-usage/../evil",
		"/etc/passwd",
		"anvil-cli-usage/..",
		"anvil-cli-usage/.",
		"anvil-cli-usage/./SKILL.md",
		`anvil-cli-usage\SKILL.md`,
		"anvil-cli-usage//SKILL.md",
		"C:\\evil",
		"other/SKILL.md",
		"anvil-cli-usage/SKILL.md/", // trailing slash is not a valid file path
	}
	for _, entry := range bad {
		m := validManifest()
		m["files"] = []string{entry}
		wantManifestError(t, manifestBytes(m), "files[0]", "")
	}
	// A path too deep exceeds the depth cap (short components keep the
	// path under the length cap so the depth rule is what fires).
	m := validManifest()
	m["files"] = []string{"anvil-cli-usage/SKILL.md", strings.Repeat("a/", MaxPathDepth) + "deep.md"}
	wantManifestError(t, manifestBytes(m), "files[1]", "depth cap")
	// A path too long exceeds the length cap.
	m = validManifest()
	m["files"] = []string{"anvil-cli-usage/SKILL.md", "anvil-cli-usage/" + strings.Repeat("x", MaxFilePathLength) + ".md"}
	wantManifestError(t, manifestBytes(m), "files[1]", "byte cap")
}

func TestParseManifest_MalformedDocument(t *testing.T) {
	// Not decodable JSON.
	wantManifestError(t, []byte("{not json"), "document", "not decodable JSON")
	// Not an object.
	wantManifestError(t, []byte("[1,2,3]"), "document", "must be a JSON object")
	// Null.
	wantManifestError(t, []byte("null"), "document", "must be a JSON object")
	// Trailing data after the document.
	wantManifestError(t, append(validManifestBytes(), []byte(" extra")...), "document", "trailing data")
}

func TestParseManifest_AggregatesProblems(t *testing.T) {
	// A document with several independent problems reports them all in
	// one pass.
	m := validManifest()
	m["name"] = "Bad_Name"
	delete(m, "description")
	m["files"] = []string{"anvil-cli-usage/SKILL.md", "../evil"}
	_, err := ParseManifest(manifestBytes(m))
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("errors.As(*ManifestError) failed: %v", err)
	}
	if len(me.Errors) < 2 {
		t.Fatalf("expected at least 2 aggregated problems, got %d: %v", len(me.Errors), me.Error())
	}

	// With a valid name, the content-inventory problems aggregate too.
	m = validManifest()
	delete(m, "description")
	m["files"] = []string{"anvil-cli-usage/SKILL.md", "anvil-cli-usage/SKILL.md", "../evil"}
	_, err = ParseManifest(manifestBytes(m))
	if !errors.As(err, &me) {
		t.Fatalf("errors.As(*ManifestError) failed: %v", err)
	}
	if len(me.Errors) < 3 {
		t.Fatalf("expected at least 3 aggregated problems, got %d: %v", len(me.Errors), me.Error())
	}
}

func TestValidateNameAndVersion(t *testing.T) {
	for _, ok := range []string{"anvil", "anvil-standard-laravel", "a", "9x"} {
		if !ValidateName(ok) {
			t.Errorf("ValidateName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "-x", "X", "x_y", "x y", strings.Repeat("a", MaxNameLength+1)} {
		if ValidateName(bad) {
			t.Errorf("ValidateName(%q) = true, want false", bad)
		}
	}
	for _, ok := range []string{"1.0.0", "10.20.30", "0.0.1"} {
		if !ValidateVersion(ok) {
			t.Errorf("ValidateVersion(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "1.0", "v1.0.0", "01.0.0", "1.0.0-beta", "1.0.0.0"} {
		if ValidateVersion(bad) {
			t.Errorf("ValidateVersion(%q) = true, want false", bad)
		}
	}
}
