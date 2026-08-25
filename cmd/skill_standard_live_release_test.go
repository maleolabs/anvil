package cmd

// ── TS-021-06 (T-104): Real-release E2E — `anvil skill install` against ─
// ── a REAL published standard release (not the core fixture) ──────────
//
// The W2 exit criteria (ticket doc §4.2): `anvil skill install
// <standard-skill>` succeeds against a REAL published standard release —
// attested named digest, strict extraction, record + targets — replacing
// the fixture-only verification of ANVIL-V2.1-S1 (ST-021-03).
//
// This test exercises the production adoption pipeline (ADR-037 D4) with
// the REAL supply chain:
//
//   - the registry index document comes from the standard repository's
//     main checkout (registry/index/<id>/<version>.json — the static
//     index, ADR-030), the same document adopters resolve;
//   - the skill asset is fetched over HTTPS from the REAL GitHub releases
//     channel (distribution.location + skills[].asset), verified against
//     the ATTESTED named content digest (TS-014-04-04), and extracted by
//     the strict bundle extractor;
//   - the publisher trust anchor is the release's OWN declared public key
//     (documented TOFU posture for release-time keys — docs/release.md of
//     the standard repos; publisher origin is the adopter's out-of-band
//     pinning, PM decision D-07).
//
// Gating: the test SKIPS when the standard repository checkout is not
// available (env var unset — the repos live outside the Core checkout,
// the established convention of adapter_laravel_resolution_test.go /
// flutter_resolution_test.go) or when the release channel is not yet
// reachable (release CI still pending). W2 enables the final verification
// once the republished releases (skills[] + attested digests) are live.
// The test never mocks the channel: an unreachable channel skips, a
// reachable channel must pass the FULL pipeline or the test fails.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillpack"
)

// ── Live release resolution ───────────────────────────────────────────

// liveStandardRepoDir resolves the checkout of a standard repository the
// test reads the real registry index from. It honors the same environment
// variables the existing standard-resolution tests use
// (ANVIL_STANDARD_<ID>_DIR / E2E_STANDARD_<ID>_DIR); the test skips when
// neither is set or the directory is not the standard module, because the
// standard repositories live outside the Core checkout.
func liveStandardRepoDir(t *testing.T, stdID string) string {
	t.Helper()
	upper := strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(stdID, "anvil-standard-"), "-", "_"))
	env := "ANVIL_STANDARD_" + upper + "_DIR"
	if v := os.Getenv(env); v != "" {
		return v
	}
	if v := os.Getenv("E2E_STANDARD_" + upper + "_DIR"); v != "" {
		return v
	}
	t.Skipf("%s not set — the %s repository is outside the Core checkout (TS-021-06 real-release E2E)", env, stdID)
	return ""
}

// liveLatestSkillsRelease reads the registry index of a standard repo
// checkout and returns the strict-parsed metadata of the NEWEST release
// that declares skills[] with attested named digests — the release this
// test installs from. It skips when the checkout has no skills-declaring
// release yet (republish pending).
func liveLatestSkillsRelease(t *testing.T, repoDir, stdID string) registry.Metadata {
	t.Helper()
	indexDir := filepath.Join(repoDir, "registry", "index", stdID)
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		t.Skipf("no registry index for %s at %s: %v (republish pending)", stdID, indexDir, err)
	}

	var versions []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(name, ".json"))
	}
	if len(versions) == 0 {
		t.Skipf("registry index for %s at %s declares no release documents", stdID, indexDir)
	}
	sort.Slice(versions, func(i, j int) bool {
		return semverGreater(versions[i], versions[j])
	})

	for _, v := range versions {
		raw, err := os.ReadFile(filepath.Join(indexDir, v+".json"))
		if err != nil {
			t.Fatalf("read index document %s %s: %v", stdID, v, err)
		}
		parsed, err := registry.Parse(raw)
		if err != nil {
			// A broken index document must not silently fall through; it
			// is a release defect the pipeline should have rejected.
			t.Fatalf("index document %s %s fails the strict registry parse: %v", stdID, v, err)
		}
		md := parsed.Metadata
		if md == nil || len(md.Skills) == 0 {
			continue
		}
		return *md
	}
	t.Skipf("%s checkout declares no release with skills[] yet (republish pending)", stdID)
	return registry.Metadata{}
}

// semverGreater reports whether a is a greater plain-semver than b.
func semverGreater(a, b string) bool {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ia, ib := 0, 0
		if i < len(pa) {
			ia = atoiSafe(pa[i])
		}
		if i < len(pb) {
			ib = atoiSafe(pb[i])
		}
		if ia != ib {
			return ia > ib
		}
	}
	return false
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// liveAssetReachable probes the real release channel for one skill asset
// (https-only, production client, short timeout). The test skips when the
// release is not live yet (CI pending) — the probe never substitutes for
// the pipeline's own fetch+verify.
func liveAssetReachable(t *testing.T, md registry.Metadata, sk registry.Skill) {
	t.Helper()
	base, err := standardReleaseDownloadBase(md.Distribution.Location)
	if err != nil {
		t.Fatalf("derive the release download base of %s %s: %v", md.ID, md.Version, err)
	}
	assetURL, err := standardReleaseAssetURL(base, sk.Asset)
	if err != nil {
		t.Fatalf("derive the asset URL of %s: %v", sk.Asset, err)
	}
	client := newStandardInstallHTTPClient()
	client.Timeout = 15 * time.Second
	req, err := http.NewRequest(http.MethodHead, assetURL, nil)
	if err != nil {
		t.Fatalf("build the liveness probe for %s: %v", assetURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("the %s %s release asset %s is not reachable yet (release CI pending): %v", md.ID, md.Version, sk.Asset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("the %s %s release asset %s is not live yet (HTTP %d)", md.ID, md.Version, sk.Asset, resp.StatusCode)
	}
}

// ── E2E: full adoption pipeline against the real release ─────────────

// TestSkillInstall_LiveStandardRelease_EndToEnd is the W2 exit-criteria
// test (ticket doc §4.2): `anvil skill install <standard-skill> --scope
// global --agent <agent>` succeeds END-TO-END against a REAL published
// standard release — resolve the pinned standard from the real index →
// lifecycle+compat gates → trust anchors before fetch → hardened https
// fetch → VerifyAssetDigest (attested named digest, fail-closed) → strict
// extraction → materialize targets → record. One skill per standard is
// exercised (the same convention as the ST-021-03 fixture E2E).
func TestSkillInstall_LiveStandardRelease_EndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		stdID      string
		skillName  string
		bodyPhrase string
	}{
		{"laravel-conventions", "anvil-standard-laravel", "laravel-conventions", "# Laravel Conventions"},
		{"flutter-conventions", "anvil-standard-flutter", "flutter-conventions", "# Flutter Conventions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := liveStandardRepoDir(t, tc.stdID)
			md := liveLatestSkillsRelease(t, repoDir, tc.stdID)

			// The target skill must be declared by the live release.
			var sk registry.Skill
			for _, s := range md.Skills {
				if s.Name == tc.skillName {
					sk = s
					break
				}
			}
			if sk.Name == "" {
				t.Fatalf("%s %s does not declare skill %q", md.ID, md.Version, tc.skillName)
			}

			// Liveness probe: skip while the release CI is pending. The
			// actual fetch below still goes through the production
			// hardened client with digest verification — never this probe.
			liveAssetReachable(t, md, sk)

			// Isolate the environment: HOME (global scope base) + XDG
			// (record stores) + the compatibility matrix. The production
			// HTTPS client is NOT replaced — the fetch verifies real TLS.
			skillTestEnv(t)
			installTestEnv(t, nil)

			// The installed-standard record is written from the REAL
			// index document (record IS the registry, ADR-037 D3): the
			// distribution resolution + the parser-validated skills[]
			// declarations. Trust anchors pin the release's own declared
			// publisher key (documented TOFU posture for release-time
			// keys — docs/release.md; publisher origin is out-of-band
			// pinning, PM decision D-07).
			skillTestWriteStandardRecord(t, md.ID, md.Version, md.Distribution.Location,
				md.Lifecycle.State, registry.SkillDeclarations(md.Skills)...)

			anchorsDir := t.TempDir()
			anchorsPath := filepath.Join(anchorsDir, "trust-anchors.json")
			anchorsDoc, err := json.Marshal(map[string]any{
				"publishers": map[string]string{md.ID: md.Trust.Attestation.PublicKey},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(anchorsPath, anchorsDoc, 0o644); err != nil {
				t.Fatal(err)
			}

			// The registry index: the REAL checkout (registry/index/)
			// serves the pinned release document the resolver parses.
			indexDir := filepath.Join(repoDir, "registry", "index")

			_, stdout, stderr, err := executeCommand("skill", "install", tc.skillName,
				"--scope", "global", "--agent", "opencode",
				"--index", indexDir, "--trust-anchors", anchorsPath)
			if err != nil {
				t.Fatalf("install %s from the real %s %s release failed: %v (stderr: %q)", tc.skillName, md.ID, md.Version, err, stderr)
			}
			if !strings.Contains(stdout, "Installed skill: "+tc.skillName) {
				t.Errorf("stdout missing the success line:\n%s", stdout)
			}

			// The master copy landed at the global scope base with the
			// provenance header (ADR-037 D10) — and it is the REAL
			// published content, not test strings: the frontmatter name,
			// the distinctive H1 body phrase of the shipped skill, and
			// the injected provenance header.
			master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", tc.skillName)
			installed, err := os.ReadFile(filepath.Join(master, "SKILL.md"))
			if err != nil {
				t.Fatalf("installed SKILL.md missing at %s: %v", master, err)
			}
			if !strings.Contains(string(installed), "name: "+tc.skillName) {
				t.Errorf("installed SKILL.md lacks the authored frontmatter name:\n%s", installed)
			}
			if !strings.Contains(string(installed), tc.bodyPhrase) {
				t.Errorf("installed SKILL.md lacks the distinctive body phrase %q (installed content is not the shipped skill):\n%s", tc.bodyPhrase, installed)
			}
			if !strings.Contains(string(installed), "# source: "+md.ID+" "+sk.Version) {
				t.Errorf("installed SKILL.md lacks the provenance header (source: %s %s):\n%s", md.ID, sk.Version, installed)
			}

			// The record pins the source standard, the skill version, the
			// distribution resolution (the REAL release channel endpoint),
			// and the targets.
			rec := skillTestReadSkillRecord(t, tc.skillName)
			if rec.Source != md.ID || rec.Version != sk.Version {
				t.Errorf("record source/version = %s %s, want %s %s", rec.Source, rec.Version, md.ID, sk.Version)
			}
			if rec.Resolution.Kind != registry.SkillResolutionKindDistribution {
				t.Errorf("record resolution kind = %q, want distribution", rec.Resolution.Kind)
			}
			if !strings.HasPrefix(rec.Resolution.Source, "https://") {
				t.Errorf("record resolution source %q is not https (ADR-030 §3)", rec.Resolution.Source)
			}
			// The recorded source is the ACTUAL endpoint after any allowed
			// redirects (ADR-022 §3) — GitHub release downloads redirect to
			// a signed CDN URL whose query carries the asset file name, so
			// the asset identity must appear in the URL (path suffix or
			// filename= query), never as a bare unauthenticated path.
			if !strings.Contains(rec.Resolution.Source, sk.Asset) {
				t.Errorf("record resolution source %q does not reference the skill asset %q", rec.Resolution.Source, sk.Asset)
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

// TestSkillInstall_LiveStandardRelease_AttestedMetadata verifies the
// security property of the live release (review focus: attestation): the
// real index document survives the strict parser with every skills[].asset
// bound to a named attested digest, the attestation verifies against the
// release's own declared public key, and every declared skill asset is
// covered by a named digest (VerifyAssetDigest, fail-closed on mismatch).
func TestSkillInstall_LiveStandardRelease_AttestedMetadata(t *testing.T) {
	cases := []struct {
		stdID string
	}{
		{"anvil-standard-laravel"},
		{"anvil-standard-flutter"},
	}
	for _, tc := range cases {
		t.Run(tc.stdID, func(t *testing.T) {
			repoDir := liveStandardRepoDir(t, tc.stdID)
			md := liveLatestSkillsRelease(t, repoDir, tc.stdID)

			// Strict parse already happened in liveLatestSkillsRelease;
			// assert the binding the schema cannot express: every
			// declared asset is covered by a named content digest.
			for _, sk := range md.Skills {
				declared, ok := registry.AssetDigest(md, sk.Asset)
				if !ok {
					t.Errorf("%s %s: asset %s is not covered by an attestation-bound named digest", md.ID, md.Version, sk.Asset)
					continue
				}
				if declared.Name != sk.Asset {
					t.Errorf("%s %s: digest entry name %q != asset %q", md.ID, md.Version, declared.Name, sk.Asset)
				}
				// Fail-closed: a wrong downloaded digest must be rejected.
				if _, err := registry.VerifyAssetDigest(md, sk.Asset, strings.Repeat("0", 64)); err == nil {
					t.Errorf("%s %s: asset %s verifies against a wrong digest (no mismatch error)", md.ID, md.Version, sk.Asset)
				}
			}

			// The attestation verifies against the release's own declared
			// public key (the anchor the adopter pins out of band).
			anchorsPath := filepath.Join(t.TempDir(), "trust-anchors.json")
			anchorsDoc, err := json.Marshal(map[string]any{
				"publishers": map[string]string{md.ID: md.Trust.Attestation.PublicKey},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(anchorsPath, anchorsDoc, 0o644); err != nil {
				t.Fatal(err)
			}
			anchors, err := registry.LoadTrustAnchors(anchorsPath)
			if err != nil {
				t.Fatal(err)
			}
			if trust := registry.VerifyAttestationAnchored(md, anchors); !trust.Valid {
				t.Errorf("%s %s attestation fails trust verification with the declared public key: %v", md.ID, md.Version, trust.Errors)
			}
		})
	}
}

// TestSkillInstall_LiveStandardRelease_FixtureParity (defensive) locks
// the supply-chain invariant that the REAL release's packed skill content
// still matches the committed core fixture seed: the published release
// must have been packed from the same authored content the S1 fixture E2E
// verifies (TS-021-06 — content seeded from fixtures/standard-skills/).
// The test compares the live release's declared asset digests against the
// digests the core packer produces from the fixture content: equal content
// ⇒ equal deterministic bundles ⇒ equal digests. Any divergence means the
// standard repos drifted from the core seed and the fixture E2E no longer
// represents what is actually shipped — a supply-chain break the test
// must surface.
func TestSkillInstall_LiveStandardRelease_FixtureParity(t *testing.T) {
	cases := []struct {
		stdID string
	}{
		{"anvil-standard-laravel"},
		{"anvil-standard-flutter"},
	}
	for _, tc := range cases {
		t.Run(tc.stdID, func(t *testing.T) {
			repoDir := liveStandardRepoDir(t, tc.stdID)
			md := liveLatestSkillsRelease(t, repoDir, tc.stdID)

			fixtureDir := filepath.Join("..", "fixtures", "standard-skills", tc.stdID, "skills")
			packed, err := skillpack.PackStandard(fixtureDir, tc.stdID)
			if err != nil {
				t.Fatalf("pack the committed %s fixture content: %v", tc.stdID, err)
			}
			declared := map[string]string{}
			for _, s := range md.Skills {
				for _, d := range md.Trust.ContentDigests {
					if d.Name == s.Asset {
						declared[s.Asset] = d.Digest
						break
					}
				}
			}
			// Forward direction: every fixture-packed asset must be
			// declared by the live release with a matching digest — a
			// release that drops or rewrites a seeded skill is caught.
			for _, s := range packed {
				want, ok := declared[s.AssetID]
				if !ok {
					t.Errorf("%s %s does not declare the fixture-seeded asset %s", md.ID, md.Version, s.AssetID)
					continue
				}
				if want != s.SHA256Hex {
					t.Errorf("live %s %s digest of %s = %s, fixture-packed = %s — the standard repo drifted from the core seed", md.ID, md.Version, s.AssetID, want, s.SHA256Hex)
				}
			}
			// Reverse direction: every declared skill asset must have its
			// fixture-packed counterpart — a release that ADDS a skill
			// outside the seed (or renames one) is caught, not just one
			// that drops a seeded skill. The sets must be exactly equal:
			// the release cannot escape the core seed undetected.
			if len(declared) != len(packed) {
				t.Errorf("live %s %s declares %d skill asset digest(s), fixture packs %d — the release escaped the core seed", md.ID, md.Version, len(declared), len(packed))
			}
			for asset := range declared {
				found := false
				for _, s := range packed {
					if s.AssetID == asset {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("live %s %s declares skill asset %s which has no fixture-packed counterpart — the standard repo shipped a skill outside the core seed", md.ID, md.Version, asset)
				}
			}
		})
	}
}
