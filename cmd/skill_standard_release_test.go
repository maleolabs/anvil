package cmd

// ── ST-021-03: Standard skills release E2E (real fixture assets) ─────
//
// The W3 exit criteria (ticket doc §4.3): the standard-skill install path
// of T-003 (resolve pinned standard → gates → trust anchors before fetch
// → fetch → VerifyAssetDigest → strict extract → record → targets) is
// verified END-TO-END against a fixture release carrying REAL skill
// assets — bundles built through the release pack step
// (internal/skillpack, ST-021-03) from the COMMITTED authored content
// (fixtures/standard-skills/), not test strings.
//
// The fixture release mirrors a standard repo release (ADR-030): the
// release archive + per-skill assets live in one release download
// directory, the registry index document declares skills[] with the
// packer's attestation-bound named digests (TS-014-04-04) signed over the
// canonical payload, trust anchors pin the publisher key, and the
// installed-standard record carries the declarations (the record IS the
// registry, ADR-037 D3).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillpack"
)

// ── Fixture release construction ─────────────────────────────────────

// skillReleasePacked packs the COMMITTED authored content of a standard
// (fixtures/standard-skills/<id>/skills) through the release pack step —
// the same internal/skillpack machinery the release pipeline runs. The
// packed bundles are the REAL skill assets the fixture release carries.
func skillReleasePacked(t *testing.T, stdID string) []*skillpack.Skill {
	t.Helper()
	contentDir := filepath.Join("..", "fixtures", "standard-skills", stdID, "skills")
	packed, err := skillpack.PackStandard(contentDir, stdID)
	if err != nil {
		t.Fatalf("pack the committed %s skill content: %v", stdID, err)
	}
	if len(packed) == 0 {
		t.Fatalf("%s packs no skills", stdID)
	}
	return packed
}

// skillReleaseServer serves a fixture standard's release archive and its
// per-skill assets over TLS — the release download directory of one tag
// (ADR-030). The physical file of each asset is named the metadata asset
// identifier, so the install gate's base+asset fetch resolves.
func skillReleaseServer(t *testing.T, stdVersion string, packed []*skillpack.Skill) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/" + stdVersion + "/release.tar.gz":
			_, _ = w.Write([]byte("fixture release content"))
		default:
			for _, s := range packed {
				if r.URL.Path == "/releases/"+stdVersion+"/"+s.AssetID {
					_, _ = w.Write(s.Bundle)
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// skillReleaseMetadata builds the attested index document of a fixture
// standard (release content digest + the packer's named skill digests,
// Ed25519-signed over the canonical payload — the composition consumers
// verify), the trust anchors, and the installed-standard record carrying
// the parser-validated skills[] declarations. It must run AFTER the test
// environment is isolated (skillReleaseEnv), so the record lands in the
// test's store.
func skillReleaseMetadata(t *testing.T, stdID, stdVersion string, packed []*skillpack.Skill, serverURL string) registry.Metadata {
	t.Helper()
	pub, priv := installTestKeypair(t)
	md := installTestRelease(t, stdID, stdVersion,
		serverURL+"/releases/"+stdVersion+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"},
		[]byte("fixture release content"), pub, priv,
		skillpack.NamedDigests(packed)...)
	md.Skills = skillpack.SkillsDeclarations(packed)
	installTestIndexEntry(t, t.TempDir(), md)
	installTestAnchorsFile(t, t.TempDir(), stdID, pub)
	skillTestWriteStandardRecord(t, stdID, stdVersion,
		serverURL+"/releases/"+stdVersion+"/release.tar.gz",
		registry.LifecycleStatePublished, registry.SkillDeclarations(md.Skills)...)
	return md
}

// skillReleaseEnv wires the standard-path test environment (temp
// XDG/HOME, the TLS-trusting hardened client, the compatibility matrix).
func skillReleaseEnv(t *testing.T, server *httptest.Server) {
	t.Helper()
	skillTestEnv(t)
	installTestEnv(t, server)
}

// skillReleaseRepo creates a repo-scope workspace: an Anvil project (anvil.yaml)
// inside a git repository (a .git entry — the probe the scope resolution
// uses), and changes the working directory into it.
func skillReleaseRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("project:\n    name: skill-e2e\n    description: ST-021-03 repo-scope E2E\n    version: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// skillReleaseScopeBase returns the directory the master copy lands under
// for a scope: the isolated HOME for global (skillTestEnv), the git root
// for repo.
func skillReleaseScopeBase(t *testing.T, scope string) string {
	t.Helper()
	switch scope {
	case "global":
		return os.Getenv("HOME")
	case "repo":
		root, err := skillFindGitRoot()
		if err != nil {
			t.Fatalf("resolve the repo scope base: %v", err)
		}
		return root
	default:
		t.Fatalf("unknown scope %q", scope)
		return ""
	}
}

// ── E2E: full adoption pipeline against the fixture release ──────────

// TestSkillInstall_StandardRelease_EndToEnd is the W3 exit-criteria test
// (§4.3): `anvil skill install <standard-skill> --agent <agent> --scope
// <repo|global>` succeeds END-TO-END against a fixture release carrying
// real skill assets — resolve the pinned standard → lifecycle+compat
// gates → VerifyAttestationAnchored before the fetch → hardened fetch →
// VerifyAssetDigest (attested named digest, fail-closed) → strict
// extraction → materialize targets → record. Every standard skill of both
// standards is exercised, across both scopes.
func TestSkillInstall_StandardRelease_EndToEnd(t *testing.T) {
	const stdVersion = "1.2.3"
	cases := []struct {
		name      string
		stdID     string
		skillName string
		scope     string // "global" | "repo"
	}{
		{"laravel-conventions-global", "anvil-standard-laravel", "laravel-conventions", "global"},
		{"laravel-delivery-repo", "anvil-standard-laravel", "laravel-delivery", "repo"},
		{"flutter-conventions-global", "anvil-standard-flutter", "flutter-conventions", "global"},
		{"flutter-delivery-repo", "anvil-standard-flutter", "flutter-delivery", "repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packed := skillReleasePacked(t, tc.stdID)
			server := skillReleaseServer(t, stdVersion, packed)
			skillReleaseEnv(t, server)
			md := skillReleaseMetadata(t, tc.stdID, stdVersion, packed, server.URL)

			if tc.scope == "repo" {
				skillTestChdir(t, skillReleaseRepo(t))
			}

			var target *skillpack.Skill
			for _, s := range packed {
				if s.Name == tc.skillName {
					target = s
					break
				}
			}
			if target == nil {
				t.Fatalf("%s does not declare skill %q", tc.stdID, tc.skillName)
			}

			_, stdout, stderr, err := executeCommand("skill", "install", tc.skillName,
				"--scope", tc.scope, "--agent", "opencode",
				"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
			if err != nil {
				t.Fatalf("install %s failed: %v (stderr: %q)", tc.skillName, err, stderr)
			}
			if !strings.Contains(stdout, "Installed skill: "+tc.skillName) {
				t.Errorf("stdout missing the success line:\n%s", stdout)
			}

			// The master copy landed at the scope base with the provenance
			// header (ADR-037 D10) — and it is the REAL authored content,
			// not test strings: the frontmatter name plus a distinctive
			// body phrase of the shipped skill.
			master := filepath.Join(skillReleaseScopeBase(t, tc.scope), ".agents", "skills", tc.skillName)
			installed, err := os.ReadFile(filepath.Join(master, "SKILL.md"))
			if err != nil {
				t.Fatalf("installed SKILL.md missing at %s: %v", master, err)
			}
			if !strings.Contains(string(installed), "name: "+tc.skillName) {
				t.Errorf("installed SKILL.md lacks the authored frontmatter name:\n%s", installed)
			}
			if !strings.Contains(string(installed), "# source: "+tc.stdID+" "+target.Version) {
				t.Errorf("installed SKILL.md lacks the provenance header (source: %s %s):\n%s", tc.stdID, target.Version, installed)
			}

			// The record pins the source standard, the skill's own
			// version, the distribution resolution (the actual asset
			// endpoint), and the targets.
			rec := skillTestReadSkillRecord(t, tc.skillName)
			if rec.Source != tc.stdID || rec.Version != target.Version {
				t.Errorf("record source/version = %s %s, want %s %s", rec.Source, rec.Version, tc.stdID, target.Version)
			}
			if rec.Resolution.Kind != registry.SkillResolutionKindDistribution {
				t.Errorf("record resolution kind = %q, want distribution", rec.Resolution.Kind)
			}
			wantURL := server.URL + "/releases/" + stdVersion + "/" + target.AssetID
			if rec.Resolution.Source != wantURL {
				t.Errorf("record resolution source = %q, want %q", rec.Resolution.Source, wantURL)
			}
			if len(rec.Targets) == 0 || rec.Targets[0].Path == "" {
				t.Errorf("record targets empty: %+v", rec.Targets)
			}
			if rec.Targets[0].Path != master {
				t.Errorf("record target path = %q, want the master copy %q", rec.Targets[0].Path, master)
			}
		})
	}
}

// TestSkillInstall_StandardRelease_AttestedMetadata verifies the security
// property the fixture release depends on (review focus: attested digests
// in the release metadata): the packer's emitted document survives the
// strict parser (skills[] well-formed, every asset bound to a named
// digest), the attestation verifies against the trust anchors
// (VerifyAttestationAnchored), and every packed asset is covered by an
// attested named digest (VerifyAssetDigest, fail-closed).
func TestSkillInstall_StandardRelease_AttestedMetadata(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
	)
	packed := skillReleasePacked(t, stdID)
	server := skillReleaseServer(t, stdVersion, packed)
	skillReleaseEnv(t, server)
	md := skillReleaseMetadata(t, stdID, stdVersion, packed, server.URL)

	// The document parses strictly: skills[] is well-formed and each
	// declared asset is bound to a named content digest (TS-021-04 rules
	// the schema cannot express).
	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := registry.Parse(raw)
	if err != nil {
		t.Fatalf("the fixture release metadata is rejected by the strict parser: %v", err)
	}
	if parsed.Metadata == nil || len(parsed.Metadata.Skills) != len(packed) {
		t.Fatalf("parsed metadata skills = %d, want %d", len(parsed.Metadata.Skills), len(packed))
	}
	for i, s := range parsed.Metadata.Skills {
		if s.Asset != packed[i].AssetID {
			t.Errorf("parsed skills[%d] asset = %q, want %q", i, s.Asset, packed[i].AssetID)
		}
	}

	// The attestation verifies against the trust anchors (anchors before
	// fetch).
	anchorsPath := skillTestAnchorsFile(t, md)
	anchors, err := registry.LoadTrustAnchors(anchorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if trust := registry.VerifyAttestationAnchored(md, anchors); !trust.Valid {
		t.Errorf("fixture release attestation fails trust verification: %v", trust.Errors)
	}

	// Every packed skill asset is covered by an attested named digest.
	for _, s := range packed {
		attested, err := registry.VerifyAssetDigest(md, s.AssetID, s.SHA256Hex)
		if err != nil {
			t.Errorf("VerifyAssetDigest(%s): %v", s.AssetID, err)
		}
		if !attested {
			t.Errorf("asset %s is not covered by an attestation-bound named digest", s.AssetID)
		}
		// A wrong digest is NOT attested (fail-closed): mismatch returns an
		// error the caller aborts on.
		if _, err := registry.VerifyAssetDigest(md, s.AssetID, strings.Repeat("0", 64)); err == nil {
			t.Errorf("asset %s verifies against a wrong digest (no mismatch error)", s.AssetID)
		}
	}
}

// TestSkillList_StandardRelease verifies record-as-registry discovery
// against the fixture release: after one standard skill is installed,
// `skill list` surfaces the installed skill and the sibling declared
// skill as available (ADR-037 D3).
func TestSkillList_StandardRelease(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
	)
	packed := skillReleasePacked(t, stdID)
	server := skillReleaseServer(t, stdVersion, packed)
	skillReleaseEnv(t, server)
	md := skillReleaseMetadata(t, stdID, stdVersion, packed, server.URL)

	if _, _, _, err := executeCommand("skill", "install", "laravel-conventions",
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md)); err != nil {
		t.Fatalf("install laravel-conventions failed: %v", err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "laravel-conventions") || !strings.Contains(stdout, "laravel-delivery") {
		t.Errorf("list lacks the standard skills:\n%s", stdout)
	}
	if !strings.Contains(stdout, stdID) {
		t.Errorf("list lacks the source standard id:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string `json:"name"`
				Source string `json:"source"`
				Status string `json:"status"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	seen := map[string]string{}
	for _, s := range envelope.Data.Skills {
		seen[s.Name] = s.Status
	}
	if seen["laravel-conventions"] != "installed" {
		t.Errorf("laravel-conventions status = %q, want installed", seen["laravel-conventions"])
	}
	if seen["laravel-delivery"] != "available" {
		t.Errorf("laravel-delivery status = %q, want available", seen["laravel-delivery"])
	}
}
