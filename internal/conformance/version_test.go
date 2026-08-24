package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// versionLinePath is the corpus version-line declaration document
// (version-line.md §6: the declaration is validated automatically; this
// test keeps the harness's declared contract version consistent with it
// so a version-line bump without a harness update fails CI).
var versionLinePath = filepath.Join("..", "..", "docs", "specification-corpus", "version-line.md")

// versionRowPattern matches the declaration's metadata row:
// | **Version** | 1.0.0 | (version-line.md metadata table).
var versionRowPattern = regexp.MustCompile(`\*\*Version\*\*\s*\|\s*([0-9]+\.[0-9]+\.[0-9]+)`)

// TestDeclaredContractVersionMatchesVersionLine pins DeclaredContractVersion
// to the corpus's version-line declaration (version-line.md metadata
// "Version" row; ADR-024 §3.1 — the specification carries its own
// semver line). If the corpus advances its version line and the
// harness's declared version is not updated with it, this check fails
// in CI instead of the harness silently validating against a stale
// contract version.
func TestDeclaredContractVersionMatchesVersionLine(t *testing.T) {
	data, err := os.ReadFile(versionLinePath)
	if err != nil {
		t.Fatalf("read version-line declaration %s: %v", versionLinePath, err)
	}

	match := versionRowPattern.FindSubmatch(data)
	if match == nil {
		t.Fatalf("no \"**Version**\" metadata row found in %s (expected the declaration | **Version** | <semver> |)", versionLinePath)
	}

	declared := string(match[1])
	if declared != DeclaredContractVersion {
		t.Errorf("declared contract version = %q, want %q (the harness must validate the contract version the corpus declares; update DeclaredContractVersion with the version-line bump)", DeclaredContractVersion, declared)
	}
}

// TestDeclaredContractVersionIsSemver guards the harness's constant
// shape: the declared contract version must be semver (major.minor.
// patch), the compatibility unit of ADR-024 §3.1.
func TestDeclaredContractVersionIsSemver(t *testing.T) {
	parts := strings.Split(DeclaredContractVersion, ".")
	if len(parts) != 3 {
		t.Fatalf("DeclaredContractVersion = %q, want semver major.minor.patch", DeclaredContractVersion)
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			t.Fatalf("DeclaredContractVersion = %q, want semver major.minor.patch", DeclaredContractVersion)
		}
	}
}
