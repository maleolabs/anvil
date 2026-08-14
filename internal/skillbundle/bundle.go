package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Skill bundle packaging and asset-name convention (TS-021-01; ADR-037
// D2; skill-bundle-format.md §2).
//
// The asset name of a skill bundle is pinned:
//
//	anvil-skill-<name>-<version>.tar.gz
//
// where <name> is the skill name (^[a-z0-9][a-z0-9-]*$) and <version>
// the bundle version (semver). Because the version is pinned to semver
// (digits and dots only), the trailing semver is an unambiguous split
// point even when the name itself contains hyphens.
//
// CreateBundle is the deterministic packer: it exists for tests and for
// the standard release packaging (ST-021-03) — it produces archives the
// extractor accepts without a round-trip surprise. It is a packer, not a
// validation surface: the manifest is validated through the same strict
// parse the extractor uses, and the sizes are capped at pack time so a
// produced bundle is never rejected at extraction for size.

// BundleNamePrefix is the fixed prefix of a skill bundle asset name
// (anvil-skill-<name>-<version>.tar.gz).
const BundleNamePrefix = "anvil-skill-"

// bundleNameSuffix is the fixed archive extension of a skill bundle
// asset.
const bundleNameSuffix = ".tar.gz"

// BundleFileName returns the canonical asset name for a skill bundle:
// anvil-skill-<name>-<version>.tar.gz (ADR-037 D2). A malformed name or
// version is rejected with an actionable error.
func BundleFileName(name, version string) (string, error) {
	if !ValidateName(name) {
		return "", fmt.Errorf("cannot form a bundle asset name: skill name %q is not valid (^[a-z0-9][a-z0-9-]*$, max %d bytes)", name, MaxNameLength)
	}
	if !ValidateVersion(version) {
		return "", fmt.Errorf("cannot form a bundle asset name: version %q is not a valid semver without leading zeros", version)
	}
	return BundleNamePrefix + name + "-" + version + bundleNameSuffix, nil
}

// ParseBundleFileName reverses BundleFileName: it splits an asset name
// anvil-skill-<name>-<version>.tar.gz back into the skill name and
// version, validating both. The version is the trailing semver segment;
// everything before the last '-' that precedes it is the name.
//
// A malformed name, a name that is not a valid identifier, or a version
// that is not semver is rejected with an actionable error.
func ParseBundleFileName(filename string) (name, version string, err error) {
	if !strings.HasPrefix(filename, BundleNamePrefix) {
		return "", "", fmt.Errorf("not a skill bundle asset name: %q does not start with %q (expected anvil-skill-<name>-<version>.tar.gz; skill-bundle-format.md §2)", filename, BundleNamePrefix)
	}
	if !strings.HasSuffix(filename, bundleNameSuffix) {
		return "", "", fmt.Errorf("not a skill bundle asset name: %q does not end with %q (expected anvil-skill-<name>-<version>.tar.gz; skill-bundle-format.md §2)", filename, bundleNameSuffix)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(filename, BundleNamePrefix), bundleNameSuffix)
	lastDash := strings.LastIndexByte(inner, '-')
	if lastDash <= 0 {
		return "", "", fmt.Errorf("not a skill bundle asset name: %q has no <name>-<version> split (expected anvil-skill-<name>-<version>.tar.gz; skill-bundle-format.md §2)", filename)
	}
	name = inner[:lastDash]
	version = inner[lastDash+1:]
	if !ValidateName(name) {
		return "", "", fmt.Errorf("skill bundle asset %q carries an invalid skill name %q (^[a-z0-9][a-z0-9-]*$, max %d bytes)", filename, name, MaxNameLength)
	}
	if !ValidateVersion(version) {
		return "", "", fmt.Errorf("skill bundle asset %q carries an invalid version %q (expected semver without leading zeros)", filename, version)
	}
	return name, version, nil
}

// CreateBundle packs a manifest and its content files into a
// deterministic skill bundle archive: a single-member gzip-compressed tar
// carrying manifest.json first, then every content file in manifest.Files
// order. The archive is the reference layout of skill-bundle-format.md
// §2.
//
// The manifest is validated through the same strict parse the extractor
// uses (ParseManifest), so a bundle produced here always passes the
// manifest stage of Extract. contents must declare exactly the manifest's
// files[] (the inventory is the packer's input): a missing key is an
// error, and the archive is written in the manifest's declared order.
//
// Sizes are capped at pack time (per-asset MaxAssetSize, total
// MaxTotalSize, MaxFileCount files), so a produced bundle is never
// rejected at extraction for size. The output is byte-deterministic for
// equal inputs: pinned headers (zeroed ownership and timestamps), mode
// 0644, and a gzip stream without timestamps.
//
// CreateBundle is a packer, not a consumption surface: extraction-side
// validation (Extract) is the only gate that applies at install.
func CreateBundle(manifest Manifest, contents map[string][]byte) ([]byte, error) {
	// Validate the manifest through the same parse the extractor uses —
	// a packer must never emit a bundle its own extractor rejects.
	mdJSON, err := marshalManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("create skill bundle: cannot encode the manifest: %w", err)
	}
	parsed, err := ParseManifest(mdJSON)
	if err != nil {
		return nil, fmt.Errorf("create skill bundle: the manifest is invalid: %w", err)
	}

	if len(parsed.Files) > MaxFileCount {
		return nil, fmt.Errorf("create skill bundle: %d content files exceed the %d-file cap", len(parsed.Files), MaxFileCount)
	}
	var total int64
	for _, f := range parsed.Files {
		data, ok := contents[f]
		if !ok {
			return nil, fmt.Errorf("create skill bundle: content for %q is missing — contents must declare exactly the manifest's files[]", f)
		}
		if len(data) > MaxAssetSize {
			return nil, fmt.Errorf("create skill bundle: %s is %d bytes, exceeding the %d-byte per-asset cap", f, len(data), MaxAssetSize)
		}
		total += int64(len(data))
		if total > MaxTotalSize {
			return nil, fmt.Errorf("create skill bundle: the content totals %d bytes, exceeding the %d-byte total cap", total, MaxTotalSize)
		}
	}
	for f := range contents {
		found := false
		for _, declared := range parsed.Files {
			if f == declared {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("create skill bundle: content for %q is not declared in the manifest's files[] — contents must declare exactly the manifest's files[]", f)
		}
	}

	// Validate the SKILL.md through the same frontmatter parse the
	// extractor applies at install: a packer must never emit a bundle its
	// own extractor rejects, and the frontmatter name must match the
	// manifest name (skill-bundle-format.md §5).
	skillContent := contents[parsed.SkillMarkdownPath()]
	fm, err := ParseFrontmatter(skillContent)
	if err != nil {
		return nil, fmt.Errorf("create skill bundle: the SKILL.md frontmatter is rejected by the portable-field validation: %w", err)
	}
	if fm.Name != parsed.Name {
		return nil, fmt.Errorf("create skill bundle: the SKILL.md frontmatter name %q does not match the manifest name %q (skill-bundle-format.md §5.1)", fm.Name, parsed.Name)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := writeBundleEntry(tw, ManifestFileName, mdJSON); err != nil {
		return nil, fmt.Errorf("create skill bundle: write manifest entry: %w", err)
	}
	for _, f := range parsed.Files {
		if err := writeBundleEntry(tw, f, contents[f]); err != nil {
			return nil, fmt.Errorf("create skill bundle: write content entry %s: %w", f, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("create skill bundle: finalize archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("create skill bundle: finalize compression: %w", err)
	}
	return buf.Bytes(), nil
}

// marshalManifest encodes a Manifest as its JSON document.
func marshalManifest(m Manifest) ([]byte, error) {
	return json.Marshal(m)
}

// writeBundleEntry writes one regular-file entry with pinned header
// fields (zeroed ownership and timestamps), so bundles are deterministic.
//
// The tar format is pinned to USTAR: the bundle format rejects PAX and
// GNU extended headers at extraction (skill-bundle-format.md §2), so the
// packer must never emit them. A path that fits USTAR's prefix+name split
// (up to 255 bytes) is encoded with a plain USTAR header; a longer path
// fails hard with the tar writer's field-too-long error rather than being
// silently upgraded to an extended header the extractor would reject.
func writeBundleEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Time{},
		Format:   tar.FormatUSTAR,
	}); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}
