package skillpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
)

// ── Fixtures ─────────────────────────────────────────────────────────

// writeSkillDir writes a minimal but valid skill content directory
// (contentDir/<name>/SKILL.md) and returns the content dir.
func writeSkillDir(t *testing.T, contentDir, name, version, description string) string {
	t.Helper()
	dir := filepath.Join(contentDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return contentDir
}

// writeSpecs writes a skills.json declaration into contentDir.
func writeSpecs(t *testing.T, contentDir string, specs []SkillSpec) {
	t.Helper()
	doc := skillSpecsDocument{Skills: specs}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "skills.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ── AssetID ──────────────────────────────────────────────────────────

func TestAssetID(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    string
	}{
		{"overview", "1.0.0", "anvil-skill-overview-1-0-0"},
		{"laravel-conventions", "1.0.0", "anvil-skill-laravel-conventions-1-0-0"},
		{"flutter-delivery", "2.1.3", "anvil-skill-flutter-delivery-2-1-3"},
	}
	for _, tc := range cases {
		got, err := AssetID(tc.name, tc.version)
		if err != nil {
			t.Errorf("AssetID(%q, %q) = error %v, want %q", tc.name, tc.version, err, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("AssetID(%q, %q) = %q, want %q", tc.name, tc.version, got, tc.want)
		}
	}

	// A version with leading zeros and an invalid name must be rejected —
	// the identifier is metadata-bound and must satisfy the parser's
	// safe-identifier pattern (registry-metadata.md §4.8).
	if _, err := AssetID("Bad_Name!", "1.0.0"); err == nil {
		t.Error("AssetID with an invalid name: expected error")
	}
	if _, err := AssetID("overview", "01.0.0"); err == nil {
		t.Error("AssetID with a leading-zero version: expected error")
	}
	if _, err := AssetID("overview", "1.0"); err == nil {
		t.Error("AssetID with a non-semver version: expected error")
	}

	// The identifier must respect the registry parser's 128-byte cap on
	// skills[].asset — a release the packer produces must never be
	// rejected at metadata parse for an oversized asset identifier.
	// Max name (64) + max version (64) + prefix + separator exceeds the
	// cap; the realistic 1.0.0 stays under it.
	longVersion := "1." + strings.Repeat("9", 60) + ".0" // 64 bytes, valid semver
	if got, err := AssetID(strings.Repeat("a", 64), longVersion); err == nil {
		t.Errorf("AssetID with an oversized identifier %q: expected error", got)
	}
	if _, err := AssetID(strings.Repeat("a", 64), "1.0.0"); err != nil {
		t.Errorf("AssetID at the max name with version 1.0.0: unexpected error %v", err)
	}
}

// TestContractVersion verifies the manifest contract version is derived
// from the supported contract major, so the two cannot drift
// (skill-bundle-format.md §4.3).
func TestContractVersion(t *testing.T) {
	want := fmt.Sprintf("%d.0.0", skillbundle.SupportedContractMajor)
	if got := ContractVersion(); got != want {
		t.Errorf("ContractVersion = %q, want %q", got, want)
	}
}

// ── LoadSpecs ────────────────────────────────────────────────────────

func TestLoadSpecs(t *testing.T) {
	dir := t.TempDir()
	writeSpecs(t, dir, []SkillSpec{
		{Name: "one", Version: "1.0.0", Description: "first"},
		{Name: "two", Version: "2.0.0", Description: "second"},
	})
	specs, err := LoadSpecs(dir)
	if err != nil {
		t.Fatalf("LoadSpecs: %v", err)
	}
	if len(specs) != 2 || specs[0].Name != "one" || specs[1].Name != "two" {
		t.Errorf("LoadSpecs = %+v, want [one, two] in order", specs)
	}

	t.Run("missing-file", func(t *testing.T) {
		if _, err := LoadSpecs(t.TempDir()); err == nil {
			t.Error("missing skills.json: expected error")
		}
	})
	t.Run("empty-array", func(t *testing.T) {
		dir := t.TempDir()
		writeSpecs(t, dir, []SkillSpec{})
		if _, err := LoadSpecs(dir); err == nil {
			t.Error("empty skills.json: expected error")
		}
	})
	t.Run("duplicate-name", func(t *testing.T) {
		dir := t.TempDir()
		writeSpecs(t, dir, []SkillSpec{
			{Name: "one", Version: "1.0.0", Description: "a"},
			{Name: "one", Version: "2.0.0", Description: "b"},
		})
		if _, err := LoadSpecs(dir); err == nil {
			t.Error("duplicate skill names: expected error")
		}
	})
	t.Run("invalid-name", func(t *testing.T) {
		dir := t.TempDir()
		writeSpecs(t, dir, []SkillSpec{{Name: "Bad_Name", Version: "1.0.0", Description: "a"}})
		if _, err := LoadSpecs(dir); err == nil {
			t.Error("invalid skill name: expected error")
		}
	})
	t.Run("invalid-version", func(t *testing.T) {
		dir := t.TempDir()
		writeSpecs(t, dir, []SkillSpec{{Name: "one", Version: "1.0", Description: "a"}})
		if _, err := LoadSpecs(dir); err == nil {
			t.Error("invalid version: expected error")
		}
	})
	t.Run("empty-description", func(t *testing.T) {
		dir := t.TempDir()
		writeSpecs(t, dir, []SkillSpec{{Name: "one", Version: "1.0.0", Description: "  "}})
		if _, err := LoadSpecs(dir); err == nil {
			t.Error("empty description: expected error")
		}
	})
}

// ── PackSkill ────────────────────────────────────────────────────────

// TestPackSkill_BundleRoundTrips verifies the pack step end-to-end for one
// skill: the produced bundle passes the strict extractor, carries the
// original content, and its digest matches the emitted sha256 — the
// property the release metadata attests.
func TestPackSkill_BundleRoundTrips(t *testing.T) {
	const (
		stdID    = "anvil-standard-laravel"
		name     = "laravel-conventions"
		version  = "1.0.0"
		desc     = "Laravel conventions"
		standard = "anvil-standard-laravel"
	)
	dir := t.TempDir()
	writeSkillDir(t, dir, name, version, desc)

	// Extra content file: the inventory must carry it and the extractor
	// must restore it.
	extra := filepath.Join(dir, name, "references", "checks.md")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("# checks\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	packed, err := PackSkill(dir, stdID, SkillSpec{Name: name, Version: version, Description: desc})
	if err != nil {
		t.Fatalf("PackSkill: %v", err)
	}

	wantAsset := "anvil-skill-laravel-conventions-1-0-0"
	if packed.AssetID != wantAsset {
		t.Errorf("AssetID = %q, want %q", packed.AssetID, wantAsset)
	}

	// The bundle must extract through the strict extractor the install
	// gate uses, and restore the authored content byte-for-byte.
	staging := t.TempDir()
	ext, err := skillbundle.Extract(packed.Bundle, staging)
	if err != nil {
		t.Fatalf("packed bundle rejected by the strict extractor: %v", err)
	}
	if ext.Manifest.Name != name || ext.Manifest.Version != version || ext.Manifest.Source != standard {
		t.Errorf("extracted manifest = %+v, want name %s / version %s / source %s", ext.Manifest, name, version, standard)
	}
	if len(ext.Manifest.Files) != 2 {
		t.Errorf("extracted files = %v, want [SKILL.md, references/checks.md]", ext.Manifest.Files)
	}
	skillPath := filepath.Join(staging, name, "SKILL.md")
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("extracted SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(raw), "name: "+name) {
		t.Errorf("extracted SKILL.md lost the frontmatter:\n%s", raw)
	}
	checksPath := filepath.Join(staging, name, "references", "checks.md")
	if _, err := os.Stat(checksPath); err != nil {
		t.Errorf("extracted extra content file missing: %v", err)
	}

	// The emitted digest is the sha-256 of exactly the bundle bytes (the
	// value the metadata attests and the install gate verifies).
	if len(packed.SHA256Hex) != 64 {
		t.Errorf("SHA256Hex = %q, want 64 lowercase hex chars", packed.SHA256Hex)
	}
	if got := sha256Hex(packed.Bundle); got != packed.SHA256Hex {
		t.Errorf("SHA256Hex does not match the bundle bytes: %q vs %q", got, packed.SHA256Hex)
	}
}

// TestPackSkill_FrontmatterNameMismatch verifies the pack-time validation:
// a skill whose SKILL.md frontmatter name differs from the declared name
// is rejected at pack time (a packer must never emit a bundle its own
// extractor rejects).
func TestPackSkill_FrontmatterNameMismatch(t *testing.T) {
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "declared")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: other-name\ndescription: mismatch\n---\n# x\n"
	if err := os.WriteFile(filepath.Join(contentDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PackSkill(dir, "anvil-standard-laravel", SkillSpec{Name: "declared", Version: "1.0.0", Description: "d"})
	if err == nil {
		t.Fatal("frontmatter/manifest name mismatch: expected pack error")
	}
}

// TestPackSkill_SymlinkRejected verifies the packer rejects symlinks in
// the content tree — the extraction posture parity: the bundle format
// rejects link entries at extraction, so a link in the authored content is
// a pipeline defect, never silently dereferenced content.
func TestPackSkill_SymlinkRejected(t *testing.T) {
	if os.Getenv("ANVIL_TEST_NO_SYMLINK") != "" {
		t.Skip("symlink creation unavailable")
	}
	dir := t.TempDir()
	writeSkillDir(t, dir, "one", "1.0.0", "first")
	writeSpecs(t, dir, []SkillSpec{{Name: "one", Version: "1.0.0", Description: "first"}})
	link := filepath.Join(dir, "one", "linked.md")
	if err := os.Symlink(filepath.Join(dir, "one", "SKILL.md"), link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	_, err := PackStandard(dir, "anvil-standard-laravel")
	if err == nil {
		t.Fatal("symlink in the content tree: expected pack error")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlink rejection not explicit: %v", err)
	}
}

// TestPackStandard_PacksAllOrFails verifies PackStandard packs every
// declared skill and fails the whole pack when one skill is broken (the
// pipeline must never publish a partially packed skills[]).
func TestPackStandard_PacksAllOrFails(t *testing.T) {
	dir := t.TempDir()
	writeSkillDir(t, dir, "one", "1.0.0", "first")
	writeSkillDir(t, dir, "two", "1.0.0", "second")
	writeSpecs(t, dir, []SkillSpec{
		{Name: "one", Version: "1.0.0", Description: "first"},
		{Name: "two", Version: "1.0.0", Description: "second"},
	})
	packed, err := PackStandard(dir, "anvil-standard-laravel")
	if err != nil {
		t.Fatalf("PackStandard: %v", err)
	}
	if len(packed) != 2 {
		t.Fatalf("PackStandard = %d skills, want 2", len(packed))
	}
	if packed[0].Name != "one" || packed[1].Name != "two" {
		t.Errorf("PackStandard order = [%s, %s], want declaration order [one, two]", packed[0].Name, packed[1].Name)
	}

	// A declared-but-missing content dir fails the whole pack.
	writeSpecs(t, dir, []SkillSpec{
		{Name: "one", Version: "1.0.0", Description: "first"},
		{Name: "missing", Version: "1.0.0", Description: "no dir"},
	})
	if _, err := PackStandard(dir, "anvil-standard-laravel"); err == nil {
		t.Error("declared-but-missing skill dir: expected the whole pack to fail")
	}
}

// ── Metadata fragment ────────────────────────────────────────────────

// TestBuildFragment_Binding verifies the release-metadata fragment: every
// declared skill carries an asset identifier bound to an attested named
// digest of the same identifier — the binding the TS-021-04 parser
// enforces at consume time (registry-metadata.md §4.8).
func TestBuildFragment_Binding(t *testing.T) {
	dir := t.TempDir()
	writeSkillDir(t, dir, "one", "1.0.0", "first")
	writeSkillDir(t, dir, "two", "2.0.0", "second")
	writeSpecs(t, dir, []SkillSpec{
		{Name: "one", Version: "1.0.0", Description: "first"},
		{Name: "two", Version: "2.0.0", Description: "second"},
	})
	packed, err := PackStandard(dir, "anvil-standard-flutter")
	if err != nil {
		t.Fatal(err)
	}

	decls := SkillsDeclarations(packed)
	digests := NamedDigests(packed)
	frag := BuildFragment(packed)

	if len(decls) != 2 || len(digests) != 2 {
		t.Fatalf("declarations = %d, digests = %d; want 2 each", len(decls), len(digests))
	}

	// Declaration ↔ digest binding: same count, same order, each asset
	// declared exactly once and covered by a same-named digest whose value
	// is the sha-256 of the packed bundle.
	seen := map[string]bool{}
	for i, d := range decls {
		if seen[d.Asset] {
			t.Errorf("asset %q declared more than once", d.Asset)
		}
		seen[d.Asset] = true

		if d.Name != packed[i].Name || d.Version != packed[i].Version {
			t.Errorf("declaration[%d] = %+v, want name/version of packed skill %d", i, d, i)
		}
		if digests[i].Name != d.Asset {
			t.Errorf("digest[%d] name = %q, want the declared asset %q", i, digests[i].Name, d.Asset)
		}
		if digests[i].Digest != packed[i].SHA256Hex {
			t.Errorf("digest[%d] value = %q, want the packed sha-256 %q", i, digests[i].Digest, packed[i].SHA256Hex)
		}
		if digests[i].Algorithm != registry.DigestAlgorithmSHA256 || digests[i].Encoding != registry.DigestEncodingBase16 {
			t.Errorf("digest[%d] = %s/%s, want sha-256/base16", i, digests[i].Algorithm, digests[i].Encoding)
		}
	}
	if len(frag.Skills) != 2 || len(frag.Trust.ContentDigests) != 2 {
		t.Errorf("fragment = %d skills / %d digests, want 2/2", len(frag.Skills), len(frag.Trust.ContentDigests))
	}

	// The fragment shape mirrors the metadata schema: skills at the
	// document root, digests under trust.contentDigests (registry-metadata
	// §4.8/§4.7) — the pipeline merges it without re-mapping field names.
	raw, err := json.Marshal(frag)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Skills []json.RawMessage `json:"skills"`
		Trust  struct {
			ContentDigests []json.RawMessage `json:"contentDigests"`
		} `json:"trust"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("fragment does not decode with the metadata-schema shape: %v", err)
	}
	if len(shape.Skills) != 2 || len(shape.Trust.ContentDigests) != 2 {
		t.Errorf("fragment JSON shape = %d skills / %d trust.contentDigests, want 2/2", len(shape.Skills), len(shape.Trust.ContentDigests))
	}
}

// TestPackStandard_RealFixture verifies the committed standard-skills
// fixture content packs cleanly for both standards (ST-021-03 DoD: both
// standards' releases carry real, packable skill assets). This test is
// the authoring backstop: a frontmatter/manifest mismatch or an invalid
// declaration in the authored content fails here, before any release.
func TestPackStandard_RealFixture(t *testing.T) {
	base := filepath.Join("..", "..", "fixtures", "standard-skills")
	for _, std := range []struct {
		id   string
		want []string
	}{
		{id: "anvil-standard-laravel", want: []string{"laravel-conventions", "laravel-delivery"}},
		{id: "anvil-standard-flutter", want: []string{"flutter-conventions", "flutter-delivery"}},
	} {
		t.Run(std.id, func(t *testing.T) {
			contentDir := filepath.Join(base, std.id, "skills")
			packed, err := PackStandard(contentDir, std.id)
			if err != nil {
				t.Fatalf("packing the committed fixture content of %s failed: %v", std.id, err)
			}
			if len(packed) != len(std.want) {
				t.Fatalf("packed %d skills, want %d", len(packed), len(std.want))
			}
			for i, want := range std.want {
				if packed[i].Name != want {
					t.Errorf("packed[%d] = %q, want %q", i, packed[i].Name, want)
				}
				// Every packed bundle must extract (the packer's own
				// guarantee, re-checked here against the real content).
				staging := t.TempDir()
				if _, err := skillbundle.Extract(packed[i].Bundle, staging); err != nil {
					t.Errorf("bundle of %s rejected by the strict extractor: %v", want, err)
				}
			}
		})
	}
}

// sha256Hex is a test-local sha-256 helper (the packer's digest is already
// asserted against it in TestPackSkill_BundleRoundTrips).
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
