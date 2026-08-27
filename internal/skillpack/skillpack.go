// Package skillpack is the standard-skills release packing step
// (ST-021-03; ADR-037 D2; skill-bundle-format.md; registry-metadata.md
// §4.8).
//
// A standard repo ships its authored skills as per-skill release assets in
// the standard's release channel: each skill is packed with
// skillbundle.CreateBundle into the deterministic
// anvil-skill-<name>-<version>.tar.gz archive, its SHA-256 computed, and
// the release metadata declares the skill (skills[].asset) bound to the
// attested named content digest (TS-021-04 / TS-014-04-04). The install
// gate then resolves the asset URL from the metadata, verifies the
// downloaded bytes against the attested named digest (fail-closed, no
// checksum fallback), and extracts through the strict bundle extractor.
//
// This package is the reference packer: the release pipeline of a standard
// repo runs it (directly, or through cmd/skillpack / the release script)
// and merges the emitted fragment — skills[] plus the named
// contentDigests under trust, mirroring the metadata schema — into the
// release metadata document BEFORE the standard's own signing step signs
// it. Packing and signing are deliberately separate: the packer never
// holds a signing key.
//
// Content layout consumed here (the fixture tree and the standard repos
// share it):
//
//	<contentDir>/            (e.g. .../anvil-standard-laravel/skills/)
//	  skills.json            [{name, version, description}] — the packer
//	                          input; one entry per skill of the release
//	                          (name+version become the bundle identity and
//	                          the asset identifier; the frontmatter name of
//	                          SKILL.md must equal the manifest name)
//	  <name>/SKILL.md        the skill content (agentskills.io, portable
//	                          frontmatter only — skill-bundle-format.md §5)
//	  <name>/…               optional extra content files, all under <name>/
//
// The release channel file of a skill is named exactly the metadata asset
// identifier — anvil-skill-<name>-<version> with dots normalized to
// hyphens — and carries the archive bytes (registry-metadata.md §4.8: the
// physical file is bound to the identifier by the release pipeline; the
// install gate fetches base + asset). The version stays in the safe
// identifier because the semver digits are unambiguous even when the name
// contains hyphens.
package skillpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
)

// assetNamePrefix is the fixed prefix of a skill release asset identifier
// (mirrors skillbundle.BundleNamePrefix).
const assetNamePrefix = "anvil-skill-"

// MaxAssetIDLength caps a skill release asset identifier at 128 bytes —
// the registry parser's cap on skills[].asset (registry-metadata.md §4.8;
// parse.go checkMaxLength(asset, 128)). The packer enforces it up front so
// a release it produces is never rejected at metadata parse for an
// oversized asset identifier.
const MaxAssetIDLength = 128

// AssetID returns the safe release-asset identifier of a skill bundle:
// anvil-skill-<name>-<version> with the version's dots normalized to
// hyphens (registry-metadata.md §4.8; registry.Skill.Asset: pattern
// ^[a-z0-9][a-z0-9-]*$, no dots — the install gate fetches
// <release base>/<asset>). The version is pinned to semver digits, so the
// trailing segment is an unambiguous split point even when the name itself
// contains hyphens.
func AssetID(name, version string) (string, error) {
	if !skillbundle.ValidateName(name) {
		return "", fmt.Errorf("cannot form a skill asset identifier: skill name %q is not valid (^[a-z0-9][a-z0-9-]*$, max %d bytes)", name, skillbundle.MaxNameLength)
	}
	if !skillbundle.ValidateVersion(version) {
		return "", fmt.Errorf("cannot form a skill asset identifier: version %q is not a valid semver without leading zeros", version)
	}
	id := assetNamePrefix + name + "-" + strings.ReplaceAll(version, ".", "-")
	if len(id) > MaxAssetIDLength {
		return "", fmt.Errorf(
			"cannot form a skill asset identifier: %q is %d bytes, exceeding the %d-byte cap on the metadata asset identifier (registry-metadata.md §4.8) — shorten the skill name or version", id, len(id), MaxAssetIDLength)
	}
	return id, nil
}

// ContractVersion returns the skill-bundle-format contract version the
// packer targets, derived from skillbundle.SupportedContractMajor so the
// manifest contract version and the implementation cannot drift
// (skill-bundle-format.md §4.3; ADR-024 §3.1 — the contract major is the
// unit of compatibility).
func ContractVersion() string {
	return fmt.Sprintf("%d.0.0", skillbundle.SupportedContractMajor)
}

// SkillSpec is the packer input for one skill: the skills.json entry a
// standard release declares. Version is the skill's own version (semver) —
// skills are versioned content and the version is part of the asset
// identifier and of the recorded identity in the installed-skills store
// (registry-metadata.md §4.8).
type SkillSpec struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// skillSpecsDocument is the on-disk shape of skills.json: a document
// carrying a `skills` array (the same shape as the release metadata's
// skills section and the fragment BuildFragment emits, so the authored
// declaration is directly comparable with what the pipeline publishes).
type skillSpecsDocument struct {
	Skills []SkillSpec `json:"skills"`
}

// LoadSpecs reads and validates the skills.json declaration of a
// standard's skills directory: one entry per skill, unique names, valid
// identifiers, valid semver versions, non-empty descriptions (the bundle
// manifest requires a non-empty description — a packer must never emit a
// bundle its own extractor rejects).
func LoadSpecs(contentDir string) ([]SkillSpec, error) {
	raw, err := os.ReadFile(filepath.Join(contentDir, "skills.json"))
	if err != nil {
		return nil, fmt.Errorf("cannot read the standard's skills.json at %s: %w", contentDir, err)
	}
	var doc skillSpecsDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("skills.json at %s is not a JSON document carrying a skills array: %w", contentDir, err)
	}
	specs := doc.Skills
	if len(specs) == 0 {
		return nil, fmt.Errorf("skills.json at %s declares no skills — a release that declares skills[] must pack at least one skill", contentDir)
	}
	seen := make(map[string]bool, len(specs))
	for i, s := range specs {
		if !skillbundle.ValidateName(s.Name) {
			return nil, fmt.Errorf("skills.json entry [%d]: skill name %q is not valid (^[a-z0-9][a-z0-9-]*$, max %d bytes)", i, s.Name, skillbundle.MaxNameLength)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("skills.json entry [%d]: skill name %q is declared more than once — names must be unique within one release", i, s.Name)
		}
		seen[s.Name] = true
		if !skillbundle.ValidateVersion(s.Version) {
			return nil, fmt.Errorf("skills.json entry [%d] (%s): version %q is not a valid semver without leading zeros", i, s.Name, s.Version)
		}
		if strings.TrimSpace(s.Description) == "" {
			return nil, fmt.Errorf("skills.json entry [%d] (%s): a non-empty description is required — the bundle manifest requires one and the release metadata declaration is advisory", i, s.Name)
		}
	}
	return specs, nil
}

// Skill is one packed skill of a standard release: the release-metadata
// declaration (name, version, asset, description) plus the material the
// release pipeline attaches to the release channel (the bundle bytes and
// their SHA-256).
type Skill struct {
	Name        string
	Version     string
	Description string
	// AssetID is the safe release-asset identifier the metadata declares
	// and the channel file is named after (AssetID of name+version).
	AssetID string
	// SHA256Hex is the lowercase-hex SHA-256 of Bundle — the value the
	// named content digest declares (sha-256, base16).
	SHA256Hex string
	// Bundle is the deterministic anvil-skill-<name>-<version>.tar.gz
	// archive bytes (skillbundle.CreateBundle).
	Bundle []byte
}

// PackSkill packs one skill's content directory (<contentDir>/<name>/
// containing SKILL.md and any extra content files) into its bundle
// (ST-021-03): the SKILL.md frontmatter is validated and its name must
// match the manifest name (skillbundle.CreateBundle enforces both at pack
// time — a packer must never emit a bundle its own extractor rejects).
func PackSkill(contentDir, standardID string, spec SkillSpec) (*Skill, error) {
	root := filepath.Join(contentDir, spec.Name)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("cannot pack skill %q: its content directory %s is missing: %w", spec.Name, root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cannot pack skill %q: %s is not a directory", spec.Name, root)
	}

	assetID, err := AssetID(spec.Name, spec.Version)
	if err != nil {
		return nil, fmt.Errorf("cannot pack skill %q: %w", spec.Name, err)
	}

	files, err := collectContentFiles(root, spec.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot pack skill %q: %w", spec.Name, err)
	}
	contents, err := readContentFiles(root, spec.Name, files)
	if err != nil {
		return nil, fmt.Errorf("cannot pack skill %q: %w", spec.Name, err)
	}

	manifest := skillbundle.Manifest{
		Name:            spec.Name,
		Version:         spec.Version,
		Source:          standardID,
		ContractVersion: ContractVersion(),
		Description:     spec.Description,
		Files:           files,
	}
	bundle, err := skillbundle.CreateBundle(manifest, contents)
	if err != nil {
		return nil, fmt.Errorf("cannot pack skill %q: %w", spec.Name, err)
	}

	sum := sha256.Sum256(bundle)
	return &Skill{
		Name:        spec.Name,
		Version:     spec.Version,
		Description: spec.Description,
		AssetID:     assetID,
		SHA256Hex:   hex.EncodeToString(sum[:]),
		Bundle:      bundle,
	}, nil
}

// PackStandard packs every skill a standard's skills directory declares
// (skills.json), in declaration order. A single failing skill fails the
// whole pack — the pipeline must not publish a release with a partially
// packed skills[] (a declared-but-unpacked skill would be uninstallable).
func PackStandard(contentDir, standardID string) ([]*Skill, error) {
	specs, err := LoadSpecs(contentDir)
	if err != nil {
		return nil, err
	}
	out := make([]*Skill, 0, len(specs))
	for _, s := range specs {
		packed, err := PackSkill(contentDir, standardID, s)
		if err != nil {
			return nil, err
		}
		out = append(out, packed)
	}
	return out, nil
}

// SkillsDeclarations converts the packed skills into the release metadata
// skills[] declarations (registry.Skill: name, version, asset, description
// — the shape the strict TS-021-04 parser consumes; the asset binding to
// the attested named digest is enforced by the parser at consume time).
func SkillsDeclarations(skills []*Skill) []registry.Skill {
	out := make([]registry.Skill, 0, len(skills))
	for _, s := range skills {
		out = append(out, registry.Skill{
			Name:        s.Name,
			Version:     s.Version,
			Asset:       s.AssetID,
			Description: s.Description,
		})
	}
	return out
}

// NamedDigests converts the packed skills into the attestation-bound named
// content-digest entries of the release metadata (TS-014-04-04): each
// entry binds the skill's asset identifier to the SHA-256 (base16) of the
// bundle bytes, so the install gate's VerifyAssetDigest verifies the
// downloaded asset against exactly what the pipeline packed — and the
// signature covers the name + digest bytes (F-2).
func NamedDigests(skills []*Skill) []registry.ContentDigest {
	out := make([]registry.ContentDigest, 0, len(skills))
	for _, s := range skills {
		out = append(out, registry.ContentDigest{
			Algorithm: registry.DigestAlgorithmSHA256,
			Encoding:  registry.DigestEncodingBase16,
			Digest:    s.SHA256Hex,
			Name:      s.AssetID,
		})
	}
	return out
}

// ReleaseMetadataFragment is the pack step's contribution to the standard's
// release metadata document: the skills[] declarations and the named
// content-digest entries the release pipeline must merge into the document
// BEFORE the standard's signing step signs it. The shape mirrors the
// metadata schema — skills sits at the document root and the digests sit
// under trust.contentDigests (registry-metadata.md §4.8, §4.7) — so the
// pipeline can merge the fragment into the release document without
// re-mapping field names. Emitted as JSON so the pipeline can merge it
// without running Go.
type ReleaseMetadataFragment struct {
	Skills []registry.Skill `json:"skills"`
	Trust  struct {
		ContentDigests []registry.ContentDigest `json:"contentDigests"`
	} `json:"trust"`
}

// BuildFragment assembles the release-metadata fragment for a packed
// skill set, in declaration order.
func BuildFragment(skills []*Skill) ReleaseMetadataFragment {
	var frag ReleaseMetadataFragment
	frag.Skills = SkillsDeclarations(skills)
	frag.Trust.ContentDigests = NamedDigests(skills)
	return frag
}

// collectContentFiles enumerates the content files of a skill directory as
// the bundle's manifest files[]: every regular file under <name>/, paths
// relativized to the content root and sorted for determinism. SKILL.md is
// included when present (the manifest validation requires it exactly once;
// a missing SKILL.md fails CreateBundle with an actionable error). The
// inventory safety rules (no traversal, within the content root) are
// enforced by CreateBundle's strict manifest parse.
//
// Symlinks are rejected outright, mirroring the extraction posture
// (skill-bundle-format.md §6.2: the extractor rejects link entries): a
// link in the content tree would either be silently dereferenced (packing
// bytes from outside the tree) or produce a bundle the extractor rejects —
// both are pipeline defects, not content.
func collectContentFiles(root, name string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("the skill content contains a symlink at %s — the bundle format rejects link entries at extraction (skill-bundle-format.md §6.2); commit the file, not a link", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(name+"/"+rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot enumerate the skill content: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("the skill directory %s carries no content files — a skill bundle carries at least <name>/SKILL.md", root)
	}
	return files, nil
}

// readContentFiles loads the content files of a skill directory into the
// bundle-content map keyed by the manifest path (<name>/<rel>). Every
// enumerated file must read; a read failure fails the pack (never a
// partial release).
func readContentFiles(root, name string, files []string) (map[string][]byte, error) {
	prefix := name + "/"
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		rel := strings.TrimPrefix(f, prefix)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("cannot read the skill content file %s: %w", f, err)
		}
		out[f] = data
	}
	return out, nil
}
