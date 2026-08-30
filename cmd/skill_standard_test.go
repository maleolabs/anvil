package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
)

// ── Fixtures ─────────────────────────────────────────────────────────

// skillTestBundle packs a deterministic, extractor-valid skill bundle for
// a standard's release (the same shape ST-021-03's packaging produces).
func skillTestBundle(t *testing.T, skillName, skillVersion, source string) []byte {
	t.Helper()
	manifest := skillbundle.Manifest{
		Name:            skillName,
		Version:         skillVersion,
		Source:          source,
		ContractVersion: "1.0.0",
		Description:     "test skill " + skillName,
		Files:           []string{skillName + "/SKILL.md"},
	}
	contents := map[string][]byte{
		skillName + "/SKILL.md": []byte(fmt.Sprintf(
			"---\nname: %s\ndescription: test skill %s\n---\n# %s\n", skillName, skillName, skillName)),
	}
	bundle, err := skillbundle.CreateBundle(manifest, contents)
	if err != nil {
		t.Fatalf("create test bundle: %v", err)
	}
	return bundle
}

// skillTestStandardFixture wires one fixture standard release carrying a
// skill into the test environment: an attested index document (skills[]
// bound to the named asset digest), the trust anchors file, and the
// installed-standard record. server must serve the asset at
// /releases/<version>/<assetID>. It returns the parsed metadata and the
// skill declaration.
func skillTestStandardFixture(t *testing.T, id, version, lifecycleState, skillName, skillVersion, assetID string, bundle []byte, serverURL string) (registry.Metadata, registry.Skill) {
	t.Helper()

	// The named asset digest covers the bundle bytes (attestation-bound).
	sum := sha256.Sum256(bundle)
	namedDigest := registry.ContentDigest{
		Algorithm: registry.DigestAlgorithmSHA256,
		Encoding:  registry.DigestEncodingBase16,
		Digest:    fmt.Sprintf("%x", sum[:]),
		Name:      assetID,
	}

	pub, priv := installTestKeypair(t)
	releaseContent := []byte("release content for " + id)
	md := installTestRelease(t, id, version,
		serverURL+"/releases/"+version+"/release.tar.gz",
		lifecycleState, "", []string{"5.1.0"}, releaseContent, pub, priv, namedDigest)
	md.Skills = []registry.Skill{{
		Name:        skillName,
		Version:     skillVersion,
		Asset:       assetID,
		Description: "test skill",
	}}

	// Index entry + anchors + installed-standard record carrying the
	// declared skill (ST-021-04 — the record IS the skill registry: the
	// resolver matches record declarations, and list surfaces them as
	// available).
	installTestIndexEntry(t, t.TempDir(), md)
	installTestAnchorsFile(t, t.TempDir(), id, pub)
	skillTestWriteStandardRecord(t, id, version, serverURL+"/releases/"+version+"/release.tar.gz", lifecycleState,
		registry.SkillDeclarations(md.Skills)...)
	return md, md.Skills[0]
}

// skillTestWriteStandardRecord records an installed-standard record for
// the fixture (the skill install resolves the declaration from the
// PINNED version of an INSTALLED standard's record — ADR-037 D3,
// ST-021-04). skills, when given, are persisted as the record's skill
// declarations.
func skillTestWriteStandardRecord(t *testing.T, id, version, distURL, lifecycleState string, skills ...registry.SkillDeclaration) {
	t.Helper()
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("default installed standards dir: %v", err)
	}
	store := registry.NewInstalledStandardStore(storeDir)
	ts := time.Now()
	if _, _, err := store.Record(id, registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Resolution: registry.Resolution{
			Kind:   registry.ResolutionKindDistribution,
			Source: distURL,
		},
		Lifecycle:   registry.Lifecycle{State: lifecycleState},
		InstalledAt: ts,
		UpdatedAt:   ts,
		Skills:      skills,
	}); err != nil {
		t.Fatalf("record installed standard %s: %v", id, err)
	}
}

// skillTestStandardServer serves the fixture standard's release content
// and the skill asset, and returns the trusted-env TLS server.
func skillTestStandardServer(t *testing.T, assetID string, asset []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/1.2.3/" + assetID:
			_, _ = w.Write(asset)
		case "/releases/1.2.3/release.tar.gz":
			_, _ = w.Write([]byte("release content"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// ── Standard Install ─────────────────────────────────────────────────

// skillTestCountingServer is a fixture TLS server that counts how many
// times the skill asset was requested, so fail-fast gates can assert
// that NO fetch happened.
type skillTestCountingServer struct {
	server    *httptest.Server
	assetHits atomic.Int32
}

func skillTestCountingServerNew(t *testing.T, assetID string, asset []byte) *skillTestCountingServer {
	t.Helper()
	cs := &skillTestCountingServer{}
	cs.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/1.2.3/" + assetID:
			cs.assetHits.Add(1)
			_, _ = w.Write(asset)
		case "/releases/1.2.3/release.tar.gz":
			_, _ = w.Write([]byte("release content"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cs.server.Close)
	return cs
}

// skillTestAnchorsFileFor writes a trust anchors file anchoring id to an
// explicit base64 key, so tests can anchor a key that does NOT match the
// fixture's declared key (anchor-mismatch gate).
func skillTestAnchorsFileFor(t *testing.T, id, keyB64 string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-anchors.json")
	doc := map[string]interface{}{
		"publishers": map[string]string{id: keyB64},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal anchors: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write anchors: %v", err)
	}
	return path
}

// TestSkillInstall_Standard_AnchorMismatchAbortsBeforeFetch verifies the
// attestation+anchor gate (ADR-037 D4: trust anchors before fetch; fix
// HIGH-1): a metadata document whose declared key is not anchored — the
// tampered-index case — aborts the install with exit 1 and the asset is
// NEVER fetched.
func TestSkillInstall_Standard_AnchorMismatchAbortsBeforeFetch(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	cs := skillTestCountingServerNew(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, cs.server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, cs.server.URL)

	// Anchor a DIFFERENT key than the fixture's declared key.
	otherPub, _ := installTestKeypair(t)
	anchorsFile := skillTestAnchorsFileFor(t, md.ID, base64.StdEncoding.EncodeToString(otherPub))

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("anchor mismatch: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (trust failure)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "trust verification failed") && !strings.Contains(stderr, "trust anchor") {
		t.Errorf("anchor-mismatch rejection not actionable:\n%s", stderr)
	}
	if hits := cs.assetHits.Load(); hits != 0 {
		t.Errorf("skill asset was fetched %d time(s) despite the failed trust gate — trust anchors must be verified before any fetch", hits)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".agents", "skills", skillName)); !os.IsNotExist(err) {
		t.Errorf("aborted install left content behind")
	}
}

// TestSkillInstall_Standard_MissingAnchorsFailFast verifies the missing-
// anchors precondition: with no --trust-anchors, no env override, and no
// default file, the install aborts before any fetch (no first-use
// acceptance, ADR-022 §3).
func TestSkillInstall_Standard_MissingAnchorsFailFast(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	cs := skillTestCountingServerNew(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, cs.server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, cs.server.URL)

	// No --trust-anchors: the default file under the temp XDG config dir
	// does not exist.
	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md))
	if err == nil {
		t.Fatal("missing anchors: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "trust") {
		t.Errorf("missing-anchors rejection not actionable:\n%s", stderr)
	}
	if hits := cs.assetHits.Load(); hits != 0 {
		t.Errorf("skill asset was fetched %d time(s) with no trust anchors configured", hits)
	}
}

// TestSkillInstall_Standard_CorruptAnchorsFailFast verifies a corrupt
// anchors file aborts before any fetch.
func TestSkillInstall_Standard_CorruptAnchorsFailFast(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	cs := skillTestCountingServerNew(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, cs.server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, cs.server.URL)

	corrupt := filepath.Join(t.TempDir(), "trust-anchors.json")
	if err := os.WriteFile(corrupt, []byte("this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", corrupt)
	if err == nil {
		t.Fatal("corrupt anchors: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "trust") {
		t.Errorf("corrupt-anchors rejection not actionable:\n%s", stderr)
	}
	if hits := cs.assetHits.Load(); hits != 0 {
		t.Errorf("skill asset was fetched %d time(s) with a corrupt anchors file", hits)
	}
}

// TestSkillUpdate_Standard_AnchorMismatchAbortsBeforeFetch verifies the
// trust gate applies to update too: a re-resolved metadata whose key is
// no longer anchored aborts before any fetch.
func TestSkillUpdate_Standard_AnchorMismatchAbortsBeforeFetch(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	cs := skillTestCountingServerNew(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, cs.server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, cs.server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	if _, _, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatal(err)
	}
	cs.assetHits.Store(0)

	// The anchor changes between install and update: the re-adoption must
	// abort before re-fetching.
	otherPub, _ := installTestKeypair(t)
	wrongAnchors := skillTestAnchorsFileFor(t, md.ID, base64.StdEncoding.EncodeToString(otherPub))

	_, _, _, err := executeCommand("skill", "update", skillName,
		"--index", indexDir, "--trust-anchors", wrongAnchors)
	if err == nil {
		t.Fatal("update with a non-anchored key: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (trust failure)", code, output.ExitCodeGeneral)
	}
	if hits := cs.assetHits.Load(); hits != 0 {
		t.Errorf("update fetched the skill asset %d time(s) despite the failed trust gate", hits)
	}
}

// TestSkillInstall_Standard_Success runs the full standard-skill
// adoption pipeline end-to-end against a local fixture: resolve the
// pinned standard → gates → anchors → fetch → digest → extract →
// materialize → record. The record carries the standard as source, the
// skill's own version, and the distribution resolution.
func TestSkillInstall_Standard_Success(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)

	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)

	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)

	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	_, stdout, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Installed skill: "+skillName) {
		t.Errorf("stdout missing success line:\n%s", stdout)
	}

	// Content landed at the master target with the provenance header.
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", skillName)
	installedMD, err := os.ReadFile(filepath.Join(master, "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(installedMD), "# source: "+stdID+" "+skillVer) {
		t.Errorf("installed SKILL.md lacks the provenance header (source: %s %s):\n%s", stdID, skillVer, installedMD)
	}

	rec := skillTestReadSkillRecord(t, skillName)
	if rec.Source != stdID {
		t.Errorf("record source = %q, want %q", rec.Source, stdID)
	}
	if rec.Version != skillVer {
		t.Errorf("record version = %q, want the skill version %q", rec.Version, skillVer)
	}
	if rec.Resolution.Kind != registry.SkillResolutionKindDistribution {
		t.Errorf("record resolution kind = %q, want %q", rec.Resolution.Kind, registry.SkillResolutionKindDistribution)
	}
	if rec.Resolution.Source != server.URL+"/releases/1.2.3/"+assetID {
		t.Errorf("record resolution source = %q, want the actual asset endpoint", rec.Resolution.Source)
	}
	if len(rec.Targets) == 0 || rec.Targets[0].Path != master {
		t.Errorf("record targets = %+v, want the master target", rec.Targets)
	}
}

// skillTestIndexDir writes the fixture metadata into a fresh index dir
// and returns it. (The fixture helper writes into its own temp dir; the
// command needs an explicit --index, so the metadata is materialized
// again into a stable location.)
func skillTestIndexDir(t *testing.T, md registry.Metadata) string {
	t.Helper()
	dir := t.TempDir()
	installTestIndexEntry(t, dir, md)
	return dir
}

// skillTestAnchorsFile writes the anchors file for the fixture metadata's
// publisher and returns its path.
func skillTestAnchorsFile(t *testing.T, md registry.Metadata) string {
	t.Helper()
	key := md.Trust.Attestation.PublicKey
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-anchors.json")
	doc := map[string]interface{}{
		"publishers": map[string]string{md.ID: key},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal anchors: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write anchors: %v", err)
	}
	return path
}

// TestSkillInstall_Standard_JSON verifies the JSON envelope of a
// standard-skill install (no StepReporter pollution on stdout).
func TestSkillInstall_Standard_JSON(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)

	_, stdout, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md), "--json")
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Name    string `json:"name"`
			Source  string `json:"source"`
			Version string `json:"version"`
			Targets []struct {
				Path string `json:"path"`
			} `json:"targets"`
			RecordPath string `json:"record_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not a JSON envelope (progress polluted it?): %v\nstdout:\n%s", err, stdout)
	}
	if envelope.Status != "success" || envelope.Data.Name != skillName || envelope.Data.Source != stdID || envelope.Data.Version != skillVer {
		t.Errorf("envelope data = %+v, want name %s / source %s / version %s", envelope.Data, skillName, stdID, skillVer)
	}
	if len(envelope.Data.Targets) == 0 || envelope.Data.Targets[0].Path == "" {
		t.Errorf("envelope targets = %+v, want recorded target paths", envelope.Data.Targets)
	}
	if strings.Contains(stdout, "Step:") {
		t.Errorf("stdout carries StepReporter progress (envelope polluted):\n%s", stdout)
	}
}

// TestSkillInstall_Standard_NoInstalledStandard verifies the missing-
// record gate: a skill is installable only when its source standard is
// installed (the record is the registry, ADR-037 D3) — exit 3.
func TestSkillInstall_Standard_NoInstalledStandard(t *testing.T) {
	skillTestEnv(t)
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
	)
	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	server := skillTestStandardServer(t, "anvil-skill-overview-1-0-0", bundle)
	installTestEnv(t, server)

	// Index entry + anchors exist, but NO installed-standard record.
	pub, priv := installTestKeypair(t)
	md := installTestRelease(t, stdID, stdVer, server.URL+"/releases/1.2.3/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, []byte("content"), pub, priv)
	md.Skills = []registry.Skill{{Name: skillName, Version: "1.0.0", Asset: "anvil-skill-overview-1-0-0"}}
	installTestIndexEntry(t, t.TempDir(), md)
	installTestAnchorsFile(t, t.TempDir(), stdID, pub)

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err == nil {
		t.Fatal("install without an installed source standard: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeRuntime {
		t.Errorf("exit code = %d, want %d (not found)", code, output.ExitCodeRuntime)
	}
	if !strings.Contains(stderr, "no installed standard declares skills") && !strings.Contains(stderr, "not provided by any installed standard") {
		t.Errorf("error not actionable:\n%s", stderr)
	}
}

// TestSkillInstall_Standard_RetiredRejected verifies the lifecycle gate:
// a skill whose source standard's pinned release is retired is not
// installable (exit 1, distinct from not-found).
func TestSkillInstall_Standard_RetiredRejected(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStateRetired,
		skillName, "1.0.0", assetID, bundle, server.URL)

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err == nil {
		t.Fatal("install from a retired source: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (gate failure)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "not offered for adoption") {
		t.Errorf("retired rejection not actionable:\n%s", stderr)
	}
}

// TestSkillInstall_Standard_DeprecatedInstallsWithWarning verifies that a
// deprecated source still installs (ADR-023 §3) but surfaces the
// deprecation warning.
func TestSkillInstall_Standard_DeprecatedInstallsWithWarning(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStateDeprecated,
		skillName, "1.0.0", assetID, bundle, server.URL)

	_, stdout, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err != nil {
		t.Fatalf("deprecated source install failed: %v", err)
	}
	if !strings.Contains(stdout, "Installed skill: "+skillName) {
		t.Errorf("stdout missing success line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deprecated") {
		t.Errorf("stdout lacks the deprecation warning:\n%s", stdout)
	}
}

// TestSkillInstall_Standard_ChecksumMismatch verifies VerifyAssetDigest
// fail-closed: a tampered asset aborts the install (exit 1) and nothing
// is recorded or materialized.
func TestSkillInstall_Standard_ChecksumMismatch(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	// The declared digest covers the REAL bundle; the server serves a
	// tampered asset.
	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	tampered := append([]byte{}, bundle...)
	tampered = append(tampered, []byte("tampered")...)

	server := skillTestStandardServer(t, assetID, tampered)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, "1.0.0", assetID, bundle, server.URL)

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err == nil {
		t.Fatal("tampered asset: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (digest mismatch)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "digest mismatch") && !strings.Contains(stderr, "attestation-bound digest") {
		t.Errorf("digest failure not actionable:\n%s", stderr)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", skillName)
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("tampered asset left content behind at %s", master)
	}
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(skillName); !errors.Is(err, registry.ErrSkillRecordNotFound) {
		t.Errorf("tampered asset recorded an install (err = %v)", err)
	}
}

// TestSkillInstall_Standard_SizeCap verifies the download size cap
// BEFORE extraction (security INFO-2 from T-002): an oversized asset is
// rejected without any extraction work.
func TestSkillInstall_Standard_SizeCap(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	// Shrink the download cap so a legitimate small bundle exceeds it.
	orig := skillAssetMaxBytes
	skillAssetMaxBytes = 16
	t.Cleanup(func() { skillAssetMaxBytes = orig })

	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, "1.0.0", assetID, bundle, server.URL)

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err == nil {
		t.Fatal("oversized asset: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "download cap") {
		t.Errorf("size-cap rejection not explicit:\n%s", stderr)
	}
}

// TestSkillInstall_Standard_VersionConflict verifies install of a skill
// recorded at a different version is rejected as a PREFLIGHT (exit 2,
// update hint): the asset is never fetched and nothing is written (P2).
func TestSkillInstall_Standard_VersionConflict(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, "1.0.0", stdID)
	cs := skillTestCountingServerNew(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, cs.server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, "1.0.0", assetID, bundle, cs.server.URL)

	// Pre-record the skill at a different version.
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	if _, _, err := store.Record(skillName, registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            skillName,
		Version:       "9.9.9",
		Source:        stdID,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindDistribution, Source: cs.server.URL},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: filepath.Join(t.TempDir(), skillName),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md))
	if err == nil {
		t.Fatal("version conflict not rejected")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeConfig {
		t.Errorf("exit code = %d, want %d (version conflict)", code, output.ExitCodeConfig)
	}
	if !strings.Contains(stderr, "skill update") {
		t.Errorf("version-conflict error not actionable (no update hint):\n%s", stderr)
	}
	if hits := cs.assetHits.Load(); hits != 0 {
		t.Errorf("version-conflict install fetched the skill asset %d time(s) — the rejection must be a preflight before any fetch", hits)
	}
}

// TestSkillInstall_Standard_AmbiguousNameRejected verifies the ambiguity
// gate: the same skill name declared by two installed standards is
// rejected with an actionable product message (F-3 — no internal jargon)
// and exit 1 (an environment/data problem, not "not found").
func TestSkillInstall_Standard_AmbiguousNameRejected(t *testing.T) {
	const (
		stdA      = "anvil-standard-laravel"
		stdB      = "anvil-standard-flutter"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetA    = "anvil-skill-overview-1-0-0-a"
		assetB    = "anvil-skill-overview-1-0-0-b"
	)
	bundleA := skillTestBundle(t, skillName, skillVer, stdA)
	bundleB := skillTestBundle(t, skillName, skillVer, stdB)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/" + stdVer + "/" + assetA:
			_, _ = w.Write(bundleA)
		case "/releases/" + stdVer + "/" + assetB:
			_, _ = w.Write(bundleB)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	skillTestEnv(t)
	installTestEnv(t, server)

	// Both standards declare the same skill name, each with its own
	// attested asset.
	pubA, privA := installTestKeypair(t)
	sumA := sha256.Sum256(bundleA)
	mdA := installTestRelease(t, stdA, stdVer, server.URL+"/releases/"+stdVer+"/release-a.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, []byte("content-a"), pubA, privA,
		registry.ContentDigest{Algorithm: registry.DigestAlgorithmSHA256, Encoding: registry.DigestEncodingBase16, Digest: fmt.Sprintf("%x", sumA[:]), Name: assetA})
	mdA.Skills = []registry.Skill{{Name: skillName, Version: skillVer, Asset: assetA}}

	pubB, privB := installTestKeypair(t)
	sumB := sha256.Sum256(bundleB)
	mdB := installTestRelease(t, stdB, stdVer, server.URL+"/releases/"+stdVer+"/release-b.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, []byte("content-b"), pubB, privB,
		registry.ContentDigest{Algorithm: registry.DigestAlgorithmSHA256, Encoding: registry.DigestEncodingBase16, Digest: fmt.Sprintf("%x", sumB[:]), Name: assetB})
	mdB.Skills = []registry.Skill{{Name: skillName, Version: skillVer, Asset: assetB}}

	indexDir := t.TempDir()
	installTestIndexEntry(t, indexDir, mdA)
	installTestIndexEntry(t, indexDir, mdB)
	skillTestWriteStandardRecord(t, stdA, stdVer, server.URL+"/releases/"+stdVer+"/release-a.tar.gz", registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: assetA})
	skillTestWriteStandardRecord(t, stdB, stdVer, server.URL+"/releases/"+stdVer+"/release-b.tar.gz", registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: assetB})
	anchorsFile := skillTestAnchorsFile(t, mdA)

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("ambiguous skill name: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (ambiguity)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "multiple installed standards") {
		t.Errorf("ambiguity rejection not actionable:\n%s", stderr)
	}
	if strings.Contains(stderr, "T-006") || strings.Contains(stderr, "seam") {
		t.Errorf("user-facing error leaks internal jargon (F-3):\n%s", stderr)
	}
}

// TestSkillCommands_JSONErrorEnvelopes verifies the --json error path for
// every command: failures write a TS-P8-05 ERROR envelope to stdout (not
// human text) and still return a non-zero exit code.
func TestSkillCommands_JSONErrorEnvelopes(t *testing.T) {
	skillTestEnv(t)

	cases := []struct {
		name string
		args []string
	}{
		{"install-invalid-name", []string{"skill", "install", "Bad_Name!", "--scope", "global", "--json"}},
		{"update-not-installed", []string{"skill", "update", "anvil-overview", "--json"}},
		{"uninstall-invalid-name", []string{"skill", "uninstall", "Bad_Name!", "--json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stdout, _, err := executeCommand(tc.args...)
			if err == nil {
				t.Fatal("expected a command error")
			}
			if code := skillTestExitCode(t, err); code == output.ExitCodeSuccess {
				t.Errorf("error exit code = 0, want non-zero")
			}
			var envelope struct {
				Version string `json:"version"`
				Status  string `json:"status"`
				Error   string `json:"error"`
			}
			if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
				t.Fatalf("stdout is not a JSON envelope: %v\nstdout:\n%s", jerr, stdout)
			}
			if envelope.Version != "1" || envelope.Status != "error" || envelope.Error == "" {
				t.Errorf("envelope = %+v, want version 1 / status error / non-empty error", envelope)
			}
		})
	}

	// list --json error path: an unreadable store directory.
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	// Replace the store directory with a FILE so the listing cannot read
	// it — the --json error envelope must still come out on stdout.
	if err := os.MkdirAll(filepath.Dir(store.Dir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Dir(), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err == nil {
		t.Fatal("list with an unreadable store: expected error")
	}
	var envelope struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("list --json error is not a JSON envelope: %v\nstdout:\n%s", jerr, stdout)
	}
	if envelope.Status != "error" || envelope.Error == "" {
		t.Errorf("list error envelope = %+v, want status error", envelope)
	}
}

// TestSkillResolutionHintsOnStderr verifies the advisory resolution
// notes (MIN-5) at the command level: an unreadable installed-standard
// record is surfaced as a stderr hint while the install/update still
// succeeds through another standard. (The W2 F-4 index-skip hint is
// obsolete: with record-based discovery the index is consulted only for
// the matched standard — an unrelated standard with an unresolvable
// pinned version is irrelevant; a MATCHED one fails the install with an
// actionable error, exercised by
// TestSkillInstall_Standard_MetadataUnresolvable.)
func TestSkillResolutionHintsOnStderr(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	// A second standard with NO skill declarations and a pinned version
	// MISSING from the index: with record-based discovery it is simply
	// irrelevant to the resolution (no declarations, never index-resolved).
	skillTestWriteStandardRecord(t, "anvil-standard-flutter", "9.9.9",
		server.URL+"/releases/9.9.9/release.tar.gz", registry.LifecycleStatePublished)

	// A corrupt installed-standard record file.
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stdDir, "anvil-standard-corrupt.json"), []byte("not a record"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install succeeds via laravel; the corrupt-record hint lands on
	// stderr.
	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stderr, "could not be read") {
		t.Errorf("stderr lacks the corrupt-record hint (MIN-5):\n%s", stderr)
	}

	// Update (same resolution path) surfaces the hint again.
	_, _, stderr, err = executeCommand("skill", "update", skillName,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("update failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stderr, "could not be read") {
		t.Errorf("update stderr lacks the resolution hints:\n%s", stderr)
	}
}

// TestSkillInstall_Standard_MetadataUnresolvable verifies the resolver's
// post-seam failure mode: a skill DECLARED by an installed standard
// record whose PINNED release metadata is missing from the registry index
// is not installable — discovery is record-based, but the install
// pipeline needs the release metadata (asset URL, attested digests,
// gates), so the resolution fails with an actionable error and exit 1
// (an environment problem, not a missing skill).
func TestSkillInstall_Standard_MetadataUnresolvable(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "9.9.9" // pinned version NOT in the index
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)

	// The installed-standard record declares the skill, but the index
	// holds NO entry for the pinned version 9.9.9 (a stale index).
	skillTestWriteStandardRecord(t, stdID, stdVer,
		server.URL+"/releases/9.9.9/release.tar.gz", registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: assetID})

	// An empty index directory: the metadata cannot be resolved.
	indexDir := t.TempDir()
	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", skillTestAnchorsFileFor(t, stdID, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="))
	if err == nil {
		t.Fatal("install with unresolvable release metadata: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (environment problem)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "could not be resolved in the registry index") {
		t.Errorf("metadata-unresolvable rejection not actionable:\n%s", stderr)
	}
}

// ── Standard Update ──────────────────────────────────────────────────

// TestSkillUpdate_Standard_Success re-adopts an installed standard skill
// and refreshes every target: the stale file is pruned, the record keeps
// its installedAt, and the source/version stay pinned.
func TestSkillUpdate_Standard_Success(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	if _, _, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatal(err)
	}
	before := skillTestReadSkillRecord(t, skillName)
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", skillName)

	// Drift: a stale file from an old version.
	stale := filepath.Join(master, "stale.txt")
	if err := os.WriteFile(stale, []byte("old version file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update without flags: scope + agents resolve from the record.
	_, stdout, _, err := executeCommand("skill", "update", skillName,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(stdout, "Updated skill: "+skillName) {
		t.Errorf("stdout missing update line:\n%s", stdout)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file %s was not pruned by the update", stale)
	}

	after := skillTestReadSkillRecord(t, skillName)
	if !before.InstalledAt.Equal(after.InstalledAt) {
		t.Errorf("update changed installedAt: %v → %v (must be preserved)", before.InstalledAt, after.InstalledAt)
	}
	if after.Source != stdID || after.Version != skillVer {
		t.Errorf("update changed the pinned source/version: %+v", after)
	}
}

// TestSkillUpdate_Standard_DeprecatedNoUpdate verifies the no-updates
// rule: a deprecated source standard's skill cannot be updated (exit 1)
// — deprecation/retirement propagates to skills (ADR-023 §3).
func TestSkillUpdate_Standard_DeprecatedNoUpdate(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStateDeprecated,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	// Install while published, then the source becomes deprecated before
	// the update (the fixture records the installed standard as
	// deprecated, mirroring the index document).
	if _, _, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "update", skillName,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("update of a deprecated source: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (no-updates rule)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "no updates") && !strings.Contains(stderr, "deprecated") {
		t.Errorf("no-updates rejection not actionable:\n%s", stderr)
	}
}

// ── Standard List ────────────────────────────────────────────────────

// TestSkillList_StandardInstalled lists an installed standard skill with
// its source standard and targets.
func TestSkillList_StandardInstalled(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)

	if _, _, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", skillTestIndexDir(t, md), "--trust-anchors", skillTestAnchorsFile(t, md)); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Standard Skills") || !strings.Contains(stdout, skillName) || !strings.Contains(stdout, stdID) {
		t.Errorf("list lacks the standard-skill entry:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name    string `json:"name"`
				Source  string `json:"source"`
				Status  string `json:"status"`
				Targets []struct {
					Path string `json:"path"`
				} `json:"targets"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v", err)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		if s.Source != stdID || s.Status != "installed" {
			t.Errorf("standard entry = %+v, want source %s / status installed", s, stdID)
		}
		if len(s.Targets) == 0 || s.Targets[0].Path == "" {
			t.Errorf("standard entry targets = %+v, want the recorded target path", s.Targets)
		}
	}
	if !found {
		t.Error("list --json lacks the standard-skill entry")
	}
}

// ── ST-021-04: Registration & Discovery ──────────────────────────────

// TestStandardInstall_RecordsSkillDeclarations verifies the ST-021-04
// registration at the explicit install event: 'anvil standard install'
// persists the release's parser-validated skills[] as the record's
// Skills []SkillDeclaration (ADR-037 D3 — the record IS the skill
// registry).
func TestStandardInstall_RecordsSkillDeclarations(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)

	pub, priv := installTestKeypair(t)
	// The content the fixture server serves at /releases/1.2.3/release.tar.gz.
	releaseContent := []byte("release content")
	sum := sha256.Sum256(bundle)
	namedDigest := registry.ContentDigest{
		Algorithm: registry.DigestAlgorithmSHA256,
		Encoding:  registry.DigestEncodingBase16,
		Digest:    fmt.Sprintf("%x", sum[:]),
		Name:      assetID,
	}
	md := installTestRelease(t, stdID, stdVer,
		server.URL+"/releases/"+stdVer+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, releaseContent, pub, priv, namedDigest)
	md.Skills = []registry.Skill{{
		Name:        skillName,
		Version:     skillVer,
		Asset:       assetID,
		Description: "Anvil overview skill",
	}}
	indexDir := t.TempDir()
	installTestIndexEntry(t, indexDir, md)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), stdID, pub)

	if _, _, stderr, err := executeCommand("standard", "install", stdID, stdVer,
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("standard install failed: %v (stderr: %q)", err, stderr)
	}

	rec := installTestReadRecord(t, stdID)
	if rec.FormatVersion != registry.RecordFormatVersion {
		t.Errorf("record formatVersion = %d, want %d", rec.FormatVersion, registry.RecordFormatVersion)
	}
	want := registry.SkillDeclarations(md.Skills)
	if len(rec.Skills) != len(want) {
		t.Fatalf("record Skills = %+v, want %+v", rec.Skills, want)
	}
	for i := range want {
		if rec.Skills[i] != want[i] {
			t.Errorf("record Skills[%d] = %+v, want %+v", i, rec.Skills[i], want[i])
		}
	}
}

// TestStandardUpdate_RefreshesSkillDeclarations verifies the refresh at
// the explicit update event: 'anvil standard update' REPLACES the
// record's declarations with the target release's (ST-021-04 / ADR-037
// D3 — the record is replaced, so the declarations are the target's).
func TestStandardUpdate_RefreshesSkillDeclarations(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		v1        = "1.2.3"
		v2        = "1.3.0"
		skillA    = "overview"
		skillAVer = "1.0.0"
		skillB    = "lifecycle"
		skillBVer = "1.0.0"
		assetA    = "anvil-skill-overview-1-0-0"
		assetB    = "anvil-skill-lifecycle-1-0-0"
	)
	bundleA := skillTestBundle(t, skillA, skillAVer, stdID)
	bundleB := skillTestBundle(t, skillB, skillBVer, stdID)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/" + v1 + "/" + assetA:
			_, _ = w.Write(bundleA)
		case "/releases/" + v2 + "/" + assetB:
			_, _ = w.Write(bundleB)
		case "/releases/" + v1 + "/release.tar.gz":
			_, _ = w.Write([]byte("content v1"))
		case "/releases/" + v2 + "/release.tar.gz":
			_, _ = w.Write([]byte("content v2"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	skillTestEnv(t)
	installTestEnv(t, server)

	pub, priv := installTestKeypair(t)
	sumA := sha256.Sum256(bundleA)
	mdA := installTestRelease(t, stdID, v1, server.URL+"/releases/"+v1+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, []byte("content v1"), pub, priv,
		registry.ContentDigest{Algorithm: registry.DigestAlgorithmSHA256, Encoding: registry.DigestEncodingBase16, Digest: fmt.Sprintf("%x", sumA[:]), Name: assetA})
	mdA.Skills = []registry.Skill{{Name: skillA, Version: skillAVer, Asset: assetA, Description: "overview skill"}}

	sumB := sha256.Sum256(bundleB)
	mdB := installTestRelease(t, stdID, v2, server.URL+"/releases/"+v2+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, []byte("content v2"), pub, priv,
		registry.ContentDigest{Algorithm: registry.DigestAlgorithmSHA256, Encoding: registry.DigestEncodingBase16, Digest: fmt.Sprintf("%x", sumB[:]), Name: assetB})
	mdB.Skills = []registry.Skill{{Name: skillB, Version: skillBVer, Asset: assetB, Description: "lifecycle skill"}}

	indexDir := t.TempDir()
	installTestIndexEntry(t, indexDir, mdA)
	installTestIndexEntry(t, indexDir, mdB)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), stdID, pub)

	if _, _, stderr, err := executeCommand("standard", "install", stdID, v1,
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("fixture install failed: %v (stderr: %q)", err, stderr)
	}
	installed := installTestReadRecord(t, stdID)
	if len(installed.Skills) != 1 || installed.Skills[0].Name != skillA {
		t.Fatalf("v1 record Skills = %+v, want [%s]", installed.Skills, skillA)
	}

	if _, _, stderr, err := executeCommand("standard", "update", stdID, v2,
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("standard update failed: %v (stderr: %q)", err, stderr)
	}

	updated := installTestReadRecord(t, stdID)
	if len(updated.Skills) != 1 || updated.Skills[0].Name != skillB {
		t.Fatalf("v2 record Skills = %+v, want the target's [%s] (refresh on update)", updated.Skills, skillB)
	}
	if updated.Skills[0].Version != skillBVer || updated.Skills[0].Asset != assetB {
		t.Errorf("v2 record Skills[0] = %+v, want version %s / asset %s", updated.Skills[0], skillBVer, assetB)
	}
	if !updated.InstalledAt.Equal(installed.InstalledAt) {
		t.Errorf("update changed installedAt: %v → %v (must be preserved)", installed.InstalledAt, updated.InstalledAt)
	}
}

// TestSkillList_StandardAvailable lists a declared-but-not-installed
// standard skill as "available" (ST-021-04 — the record IS the skill
// registry; discovery iterates installed-standard records, no index, no
// separate store).
func TestSkillList_StandardAvailable(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
	)
	skillTestEnv(t)

	// An installed-standard record declaring the skill; nothing installed.
	skillTestWriteStandardRecord(t, stdID, stdVer, "https://example.com/releases/1.2.3/release.tar.gz",
		registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: "anvil-skill-overview-1-0-0", Description: "Anvil overview skill"})

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Standard Skills") || !strings.Contains(stdout, skillName) || !strings.Contains(stdout, stdID) {
		t.Errorf("list lacks the available standard-skill row:\n%s", stdout)
	}
	if !strings.Contains(stdout, "available") {
		t.Errorf("declared skill not shown as available:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name    string `json:"name"`
				Source  string `json:"source"`
				Version string `json:"version"`
				Status  string `json:"status"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		if s.Source != stdID || s.Status != "available" || s.Version != skillVer {
			t.Errorf("available entry = %+v, want source %s / status available / version %s", s, stdID, skillVer)
		}
	}
	if !found {
		t.Error("list --json lacks the available standard-skill entry")
	}
}

// TestSkillList_StandardRetiredUnavailable verifies the D4-gates
// propagation: a skill declared by a RETIRED standard is shown as
// "unavailable" with an actionable message (retired releases are not
// offered for fresh adoption, ADR-027 §3) — the no-updates rule
// propagates to the standard's skills.
func TestSkillList_StandardRetiredUnavailable(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
	)
	skillTestEnv(t)
	skillTestWriteStandardRecord(t, stdID, stdVer, "https://example.com/releases/1.2.3/release.tar.gz",
		registry.LifecycleStateRetired,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: "anvil-skill-overview-1-0-0"})

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "unavailable") {
		t.Errorf("retired-source skill not shown as unavailable:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not offered for installation") {
		t.Errorf("unavailable entry lacks the actionable message:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string   `json:"name"`
				Status string   `json:"status"`
				Hints  []string `json:"hints"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v", err)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		if s.Status != "unavailable" || len(s.Hints) == 0 {
			t.Errorf("unavailable entry = %+v, want status unavailable with hints", s)
		}
	}
	if !found {
		t.Error("list --json lacks the unavailable entry")
	}
}

// TestSkillList_StandardUninstallRemovesDeclarations verifies the
// uninstall propagation: after the source standard record is removed,
// its declared-but-not-installed skills disappear from the listing
// (discovery iterates records), while an INSTALLED skill of that
// standard stays listed, flagged stale by TS-021-03 (missing source
// standard) with the uninstall hint.
func TestSkillList_StandardUninstallRemovesDeclarations(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	// Install the skill, then "uninstall" the standard (record delete —
	// the standard store's Delete; there is no standard-uninstall command
	// surface in this sprint).
	if _, _, _, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatal(err)
	}
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.NewInstalledStandardStore(stdDir).Delete(stdID); err != nil {
		t.Fatalf("delete installed-standard record: %v", err)
	}

	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string   `json:"name"`
				Status string   `json:"status"`
				Hints  []string `json:"hints"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	var entry *struct {
		Name   string   `json:"name"`
		Status string   `json:"status"`
		Hints  []string `json:"hints"`
	}
	for i := range envelope.Data.Skills {
		if envelope.Data.Skills[i].Name == skillName {
			entry = &envelope.Data.Skills[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("list after standard uninstall lost the installed skill %s entirely (want stale): %s", skillName, stdout)
	}
	if entry.Status != "stale" {
		t.Errorf("installed skill after standard uninstall status = %q, want stale (TS-021-03 missing source)", entry.Status)
	}
	if len(entry.Hints) == 0 || !strings.Contains(strings.Join(entry.Hints, " "), "uninstall") {
		t.Errorf("stale entry lacks the actionable uninstall hint: %+v", entry.Hints)
	}
}

// TestSkillList_LegacyRecordCompatible verifies the ST-021-04 migration
// at the command level: an installed-standard record written in the
// legacy format version (1) stays readable by 'anvil skill list' — its
// Skills default empty (no available rows, no data loss), the listing
// succeeds, and an installed skill sourced from it is still reported.
func TestSkillList_LegacyRecordCompatible(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
	)
	skillTestEnv(t)

	// Write a legacy format-1 record file by hand (the pre-T-006 shape).
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := registry.InstalledStandardRecord{
		FormatVersion:   registry.LegacyRecordFormatVersion,
		ID:              stdID,
		Version:         stdVer,
		ContractVersion: "1.0.0",
		Resolution: registry.Resolution{
			Kind:   registry.ResolutionKindDistribution,
			Source: "https://example.com/releases/1.2.3/release.tar.gz",
		},
		Lifecycle:   registry.Lifecycle{State: registry.LifecycleStatePublished},
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stdDir, stdID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// An installed skill sourced from the legacy standard (published →
	// current, not stale).
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	target := filepath.Join(t.TempDir(), skillName)
	if _, _, err := store.Record(skillName, registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            skillName,
		Version:       "1.0.0",
		Source:        stdID,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindDistribution, Source: "https://example.com/releases/1.2.3/anvil-skill-overview-1-0-0"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: target,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatalf("list with a legacy-format standard record failed: %v", err)
	}
	if !strings.Contains(stdout, skillName) || !strings.Contains(stdout, "installed") {
		t.Errorf("list with a legacy record lost the installed skill row:\n%s", stdout)
	}

	// --json: no corrupt_standard_records, the installed row is present.
	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "corrupt_standard_records") {
		t.Errorf("legacy record reported as corrupt in --json:\n%s", stdout)
	}
}

// ── Fix Round (team review) ─────────────────────────────────────────

// TestSkillUpdate_LegacyRecordRefreshHint verifies the reviewer #2 fix:
// a skill whose source standard IS installed but whose record predates
// the ST-021-04 declarations (legacy format 1, no Skills) fails the
// update with the "not provided" exit (3) and an ACTIONABLE hint —
// refresh the standard record via an explicit install/update — instead
// of the misleading "install a standard that ships skills first" (the
// standard is already installed).
func TestSkillUpdate_LegacyRecordRefreshHint(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
	)
	skillTestEnv(t)

	// A legacy format-1 standard record: installed, no declarations.
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := registry.InstalledStandardRecord{
		FormatVersion:   registry.LegacyRecordFormatVersion,
		ID:              stdID,
		Version:         stdVer,
		ContractVersion: "1.0.0",
		Resolution: registry.Resolution{
			Kind:   registry.ResolutionKindDistribution,
			Source: "https://example.com/releases/1.2.3/release.tar.gz",
		},
		Lifecycle:   registry.Lifecycle{State: registry.LifecycleStatePublished},
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stdDir, stdID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// An installed skill record sourced from the legacy standard.
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	if _, _, err := store.Record(skillName, registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            skillName,
		Version:       "1.0.0",
		Source:        stdID,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindDistribution, Source: "https://example.com/releases/1.2.3/anvil-skill-overview-1-0-0"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: filepath.Join(t.TempDir(), skillName),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "update", skillName)
	if err == nil {
		t.Fatal("update of a skill whose source record carries no declarations: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeRuntime {
		t.Errorf("exit code = %d, want %d (not found)", code, output.ExitCodeRuntime)
	}
	if !strings.Contains(stderr, "declare no skills") {
		t.Errorf("error lacks the no-declarations diagnosis:\n%s", stderr)
	}
	if !strings.Contains(stderr, "anvil standard update") {
		t.Errorf("error lacks the refresh-the-record hint (standard update):\n%s", stderr)
	}
	if strings.Contains(stderr, "install a standard that ships skills") {
		t.Errorf("error misleadingly tells the user to install a standard when the standard is already installed:\n%s", stderr)
	}
}

// TestSkillInstall_Standard_DeclarationDivergence verifies the reviewer
// #3 fix: a record declaration that diverges from the release metadata
// on version or asset (a hand-edited record, a tampered index, or a
// re-published release) is rejected with an actionable error (exit 1)
// instead of silently installing from unvalidated state.
func TestSkillInstall_Standard_DeclarationDivergence(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	// Hand-edit the record's declaration to a divergent version/asset.
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := registry.NewInstalledStandardStore(stdDir).Get(stdID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Skills = []registry.SkillDeclaration{{
		Name:    skillName,
		Version: "9.9.9",                      // divergent
		Asset:   "anvil-skill-overview-9-9-9", // divergent
	}}
	if _, err := registry.NewInstalledStandardStore(stdDir).Update(stdID, rec); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("divergent declaration: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (divergence)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "disagree") {
		t.Errorf("divergence rejection not actionable (no 'disagree' diagnosis):\n%s", stderr)
	}
	if !strings.Contains(stderr, "9.9.9") || !strings.Contains(stderr, skillVer) {
		t.Errorf("divergence error does not surface both the record and the metadata declaration:\n%s", stderr)
	}
}

// TestSkillList_StandardLifecycleCrossCheckIndex verifies the reviewer
// #1 / security LOW-1 fix: the declared rows' status follows the
// registry index metadata's lifecycle when it resolves — a standard
// whose RECORD is published but whose index metadata is retired is
// shown as unavailable (the frozen record lifecycle does not mask the
// current truth).
func TestSkillList_StandardLifecycleCrossCheckIndex(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	skillTestEnv(t)

	// Index metadata: RETIRED (with the digest-bound skill asset so the
	// strict parse passes).
	pub, priv := installTestKeypair(t)
	releaseContent := []byte("release content")
	sum := sha256.Sum256(bundle)
	md := installTestRelease(t, stdID, stdVer, "https://example.com/releases/"+stdVer+"/release.tar.gz",
		registry.LifecycleStateRetired, "", []string{"5.1.0"}, releaseContent, pub, priv,
		registry.ContentDigest{Algorithm: registry.DigestAlgorithmSHA256, Encoding: registry.DigestEncodingBase16, Digest: fmt.Sprintf("%x", sum[:]), Name: assetID})
	md.Skills = []registry.Skill{{Name: skillName, Version: skillVer, Asset: assetID}}
	indexDir := t.TempDir()
	installTestIndexEntry(t, indexDir, md)
	t.Setenv("ANVIL_REGISTRY_INDEX", indexDir)

	// Record: PUBLISHED (frozen at adoption), with the declaration.
	skillTestWriteStandardRecord(t, stdID, stdVer, "https://example.com/releases/"+stdVer+"/release.tar.gz",
		registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: assetID})

	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string   `json:"name"`
				Status string   `json:"status"`
				Hints  []string `json:"hints"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		// The index metadata (retired) wins over the record (published).
		if s.Status != "unavailable" {
			t.Errorf("entry status = %q, want unavailable (index metadata retired, record published)", s.Status)
		}
		// Index resolved → no verify hint.
		for _, h := range s.Hints {
			if strings.Contains(h, "registry index could not be resolved") {
				t.Errorf("resolved index unexpectedly carries the verify hint: %q", h)
			}
		}
	}
	if !found {
		t.Error("list --json lacks the declared standard-skill entry")
	}
}

// TestSkillList_StandardLifecycleFallbackHint verifies the reviewer #1 /
// security LOW-1 fallback: when the registry index is unavailable, the
// declared row falls back to the record's frozen lifecycle and carries a
// verify hint — the listing never fails over a missing index.
func TestSkillList_StandardLifecycleFallbackHint(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
	)
	skillTestEnv(t)
	// No ANVIL_REGISTRY_INDEX and no default index dir → unresolvable.
	skillTestWriteStandardRecord(t, stdID, stdVer, "https://example.com/releases/1.2.3/release.tar.gz",
		registry.LifecycleStatePublished,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: "anvil-skill-overview-1-0-0"})

	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string   `json:"name"`
				Status string   `json:"status"`
				Hints  []string `json:"hints"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		// Fallback: the record lifecycle (published → available).
		if s.Status != "available" {
			t.Errorf("entry status = %q, want available (record fallback)", s.Status)
		}
		var hasVerify bool
		for _, h := range s.Hints {
			if strings.Contains(h, "registry index could not be resolved") || strings.Contains(h, "verify with the registry index") {
				hasVerify = true
			}
		}
		if !hasVerify {
			t.Errorf("fallback entry lacks the verify-with-the-index hint: %+v", s.Hints)
		}
	}
	if !found {
		t.Error("list --json lacks the declared standard-skill entry")
	}
}

// TestSkillList_StandardDeprecatedAvailable covers the deprecated branch
// of skillEntryFromDeclaration at the command level (reviewer #4 /
// product F-4): a deprecated source standard's declared skill stays
// "available" and the hint names the deprecation — human and JSON.
func TestSkillList_StandardDeprecatedAvailable(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
	)
	skillTestEnv(t)
	skillTestWriteStandardRecord(t, stdID, stdVer, "https://example.com/releases/1.2.3/release.tar.gz",
		registry.LifecycleStateDeprecated,
		registry.SkillDeclaration{Name: skillName, Version: skillVer, Asset: "anvil-skill-overview-1-0-0"})

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, skillName) || !strings.Contains(stdout, "available") {
		t.Errorf("deprecated-source skill not listed as available:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deprecated") {
		t.Errorf("deprecated-source entry lacks the deprecation hint:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name   string   `json:"name"`
				Status string   `json:"status"`
				Hints  []string `json:"hints"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v", err)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		if s.Status != "available" {
			t.Errorf("entry status = %q, want available", s.Status)
		}
		var hasDeprecation bool
		for _, h := range s.Hints {
			if strings.Contains(h, "deprecated") {
				hasDeprecation = true
			}
		}
		if !hasDeprecation {
			t.Errorf("entry hints lack the deprecation notice: %+v", s.Hints)
		}
	}
	if !found {
		t.Error("list --json lacks the deprecated-source entry")
	}
}

// TestSkillInstall_Standard_DuplicateDeclarationRejected verifies the
// reviewer #5 nit fix: a hand-edited record declaring the same skill
// name more than once is rejected as an inconsistent record ("more than
// once") — never rendered as a repeated standard id or as a fake
// multi-standard ambiguity.
func TestSkillInstall_Standard_DuplicateDeclarationRejected(t *testing.T) {
	const (
		stdID     = "anvil-standard-laravel"
		stdVer    = "1.2.3"
		skillName = "overview"
		skillVer  = "1.0.0"
		assetID   = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVer, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)
	indexDir := skillTestIndexDir(t, md)
	anchorsFile := skillTestAnchorsFile(t, md)

	// Hand-edit the record to declare the same skill name twice.
	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := registry.NewInstalledStandardStore(stdDir).Get(stdID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Skills = []registry.SkillDeclaration{
		{Name: skillName, Version: skillVer, Asset: assetID},
		{Name: skillName, Version: skillVer, Asset: assetID},
	}
	if _, err := registry.NewInstalledStandardStore(stdDir).Update(stdID, rec); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", skillName,
		"--scope", "global", "--agent", "opencode",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("duplicate declaration: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "more than once") {
		t.Errorf("duplicate-declaration rejection not diagnosed as an inconsistent record:\n%s", stderr)
	}
	if strings.Contains(stderr, stdID+", "+stdID) {
		t.Errorf("standard id repeated in the error (dedupe failure):\n%s", stderr)
	}
	if strings.Contains(stderr, "multiple installed standards") {
		t.Errorf("duplicate declarations in ONE standard misreported as multi-standard ambiguity:\n%s", stderr)
	}
}
