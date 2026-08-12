// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil standard install" (TS-014-03-01): the explicit
// installation flow — adoption order (resolve → strict parse → lifecycle
// gate → compatibility → location → fetch → trust → record), idempotency
// by identity plus version, deprecation warnings, retired rejection,
// project framework version handling, the content fetch policy (https
// only, redirect policy, size cap, timeout), and the exit code / --json
// conventions.
//
// Every test is self-contained: the static index, the trust anchors
// file, the project config, and the global config directory (XDG_CONFIG_HOME
// — record store) live in t.TempDir(); release content is served by a
// local https test server.
//
// Reference: TS-014-03-01, ADR-022 §3, ADR-023 §3, ADR-026, ADR-027 §3,
// ADR-030 §3
package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Test Fixtures ────────────────────────────────────────────────────

// installTestKeypair returns a fresh real Ed25519 key pair for tests.
func installTestKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return pub, priv
}

// installTestRelease builds a fully attested release document: real
// content, its real SHA-256 digest (base16), and a real Ed25519 signature
// over the canonical attestation payload (utf8(id) || 0x00 || utf8(version)
// || 0x00 || concat(decoded digest bytes in contentDigests array order) —
// the byte-exact composition of metadata.go) made with the given key.
// distributionLocation is the https URL the release content is served
// from; capability is the declared framework-version support scope.
// extraDigests are appended to trust.contentDigests AFTER the content
// digest and are covered by the same signature — the shape a
// TS-014-04-04 release carries (named digests of the release's binary
// assets).
func installTestRelease(t *testing.T, id, version, distributionLocation, lifecycleState, removalDate string, capability []string, content []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey, extraDigests ...registry.ContentDigest) registry.Metadata {
	t.Helper()
	sum := sha256.Sum256(content)
	digests := []registry.ContentDigest{{
		Algorithm: registry.DigestAlgorithmSHA256,
		Encoding:  registry.DigestEncodingBase16,
		Digest:    fmt.Sprintf("%x", sum[:]),
	}}
	digests = append(digests, extraDigests...)

	md := registry.Metadata{
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Capability: registry.Capability{
			FrameworkVersion: capability,
		},
		Distribution: registry.Distribution{
			Type:     registry.DistributionTypeGitHubReleases,
			Location: distributionLocation,
		},
		Lifecycle: registry.Lifecycle{
			State:       lifecycleState,
			RemovalDate: removalDate,
		},
		Trust: registry.Trust{
			ContentDigests: digests,
		},
	}
	payload := append([]byte(nil), md.ID...)
	payload = append(payload, 0x00)
	payload = append(payload, md.Version...)
	payload = append(payload, 0x00)
	// The canonical payload prefixes each NAMED entry's digest bytes with
	// utf8(name) || 0x00 — the asset binding is signed material
	// (security review F-2; registry-metadata.md §4.7).
	for _, d := range digests {
		decoded, err := hex.DecodeString(d.Digest)
		if err != nil {
			t.Fatalf("extra digest %q is not base16: %v", d.Digest, err)
		}
		if d.Name != "" {
			payload = append(payload, d.Name...)
			payload = append(payload, 0x00)
		}
		payload = append(payload, decoded...)
	}
	md.Trust.Attestation = registry.Attestation{
		Algorithm: registry.AttestationAlgorithmEd25519,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	return md
}

// installTestIndexEntry writes one release document into the static index
// tree at <index>/<id>/<version>.json and returns the document path.
func installTestIndexEntry(t *testing.T, indexDir string, md registry.Metadata) string {
	t.Helper()
	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal test metadata: %v", err)
	}
	path := filepath.Join(indexDir, md.ID, md.Version+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// installTestAnchorsFile writes a trust anchors allowlist file anchoring
// the publisher id to the given public key.
func installTestAnchorsFile(t *testing.T, dir, id string, pub ed25519.PublicKey) string {
	t.Helper()
	path := filepath.Join(dir, "trust-anchors.json")
	doc := map[string]interface{}{
		"publishers": map[string]string{
			id: base64.StdEncoding.EncodeToString(pub),
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal anchors: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// installTestEnv isolates the command's global state for one test:
//
//   - XDG_CONFIG_HOME → a temp dir, so the record store and the default
//     trust anchors path resolve into the test (never the real user
//     config);
//   - ANVIL_COMPATIBILITY_MATRIX → the repository's compatibility matrix
//     record (the install flow reads the runtime's supported contract
//     majors from the corpus matrix at runtime — TS-014-04-03; tests run
//     with the working directory set to the cmd package dir, so the
//     corpus path resolves relative to it);
//   - standardInstallHTTPClient → a client that trusts the local test
//     server's TLS certificate while keeping the production redirect
//     policy and the shared timeout;
//   - standardContentMaxBytes → the production value (restored).
func installTestEnv(t *testing.T, server *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	matrixPath, err := filepath.Abs(filepath.Join("..", "docs", "specification-corpus", "compatibility-matrix.json"))
	if err != nil {
		t.Fatalf("resolve compatibility matrix path: %v", err)
	}
	t.Setenv(registry.EnvCompatibilityMatrix, matrixPath)

	if server != nil {
		origClient := standardInstallHTTPClient
		client := newStandardInstallHTTPClient()
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs},
		}
		standardInstallHTTPClient = client
		t.Cleanup(func() { standardInstallHTTPClient = origClient })
	}
}

// installTestProject writes an anvil.yaml project config into dir and
// changes the working directory into it for the duration of the test.
func installTestProject(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write anvil.yaml: %v", err)
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// installTestRecordPath returns the record file path for id under the
// isolated global config directory.
func installTestRecordPath(t *testing.T, id string) string {
	t.Helper()
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("default installed standards dir: %v", err)
	}
	return filepath.Join(storeDir, id+".json")
}

// installTestReadRecord reads the recorded installed-standard record.
func installTestReadRecord(t *testing.T, id string) registry.InstalledStandardRecord {
	t.Helper()
	raw, err := os.ReadFile(installTestRecordPath(t, id))
	if err != nil {
		t.Fatalf("read record %s: %v", id, err)
	}
	var rec registry.InstalledStandardRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode record %s: %v", id, err)
	}
	return rec
}

// installTestStandardContent is deterministic release content for tests.
func installTestStandardContent(id string) []byte {
	return []byte("release content for " + id + " (TS-014-03-01 explicit installation flow tests)")
}

// ── Command Group Registration ───────────────────────────────────────

// TestStandardInstallCommand_Registered verifies that the install command
// is registered in the standard group: installation is available only
// through this explicit command surface — nothing in the CLI installs a
// standard implicitly.
//
// Reference: TS-014-03-01 (DoD: installation requires explicit user
// invocation), ADR-023 §3
func TestStandardInstallCommand_Registered(t *testing.T) {
	_, _, err := rootCmd.Find([]string{"standard", "install"})
	if err != nil {
		t.Fatalf("standard install command not found: %v", err)
	}
	_, helpOut, _, err := executeCommand("standard", "--help")
	if err != nil {
		t.Fatalf("standard --help failed: %v", err)
	}
	if !strings.Contains(helpOut, "install") {
		t.Errorf("standard group help does not list the install subcommand:\n%s", helpOut)
	}
}

// ── Success Path ─────────────────────────────────────────────────────

// TestStandardInstall_Success installs a valid published release end to
// end: the record is persisted with the pinned version, contract version,
// explicit distribution resolution, lifecycle state, and the embedded
// compatibility and trust results; the command succeeds (exit 0).
//
// Reference: TS-014-03-01 (DoD: validation runs before install completes;
// installed version is recorded with its pinned resolution)
func TestStandardInstall_Success(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	cmd, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	if cmd == nil {
		t.Fatal("executeCommand returned a nil command")
	}
	if !strings.Contains(stdout, "Installed standard: "+id+" "+version) {
		t.Errorf("stdout missing success line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "distribution") || !strings.Contains(stdout, server.URL) {
		t.Errorf("stdout missing resolution details:\n%s", stdout)
	}
	if !strings.Contains(stdout, "trust: ok") || !strings.Contains(stdout, "compatibility: ok") {
		t.Errorf("stdout missing validation summary:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.FormatVersion != registry.RecordFormatVersion {
		t.Errorf("record formatVersion = %d, want %d", rec.FormatVersion, registry.RecordFormatVersion)
	}
	if rec.ID != id || rec.Version != version || rec.ContractVersion != "1.0.0" {
		t.Errorf("record identity = %s %s (contract %s), want %s %s (contract 1.0.0)",
			rec.ID, rec.Version, rec.ContractVersion, id, version)
	}
	if rec.Resolution.Kind != registry.ResolutionKindDistribution || rec.Resolution.Source != server.URL+"/release.tar.gz" {
		t.Errorf("record resolution = %+v, want kind distribution with the resolved https URL", rec.Resolution)
	}
	if rec.Lifecycle.State != registry.LifecycleStatePublished {
		t.Errorf("record lifecycle = %q, want published", rec.Lifecycle.State)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid {
		t.Errorf("record compatibility = %+v, want a valid embedded result", rec.Compatibility)
	}
	if rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("record trust = %+v, want a valid embedded result", rec.Trust)
	}
	// The fixture runs outside any project: no framework declared, so the
	// capability validation ran shape-only and that fact is recorded.
	if rec.Compatibility.FrameworkVersionChecked {
		t.Errorf("record compatibility frameworkVersionChecked = true, want false (no project → shape-only)")
	}
	if !rec.InstalledAt.Equal(rec.UpdatedAt) {
		t.Errorf("fresh record installedAt %v != updatedAt %v", rec.InstalledAt, rec.UpdatedAt)
	}
}

// TestStandardInstall_SuccessJSON verifies the --json envelope shape
// (TS-P8-05): success envelope with the install data — identity, pinned
// version, resolution, lifecycle, timestamps, validation results.
//
// Reference: TS-014-03-01, TS-P8-05
func TestStandardInstall_SuccessJSON(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile, "--json")
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}

	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Data    struct {
			ID               string          `json:"id"`
			Version          string          `json:"version"`
			ContractVersion  string          `json:"contract_version"`
			Resolution       json.RawMessage `json:"resolution"`
			Lifecycle        json.RawMessage `json:"lifecycle"`
			InstalledAt      string          `json:"installed_at"`
			UpdatedAt        string          `json:"updated_at"`
			AlreadyInstalled bool            `json:"already_installed"`
			Compatibility    json.RawMessage `json:"compatibility"`
			Trust            json.RawMessage `json:"trust"`
			RecordPath       string          `json:"record_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not the JSON envelope: %v\n%s", err, stdout)
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		t.Errorf("envelope = %s/%s, want version 1 status success", envelope.Version, envelope.Status)
	}
	if envelope.Data.ID != id || envelope.Data.Version != version || envelope.Data.ContractVersion != "1.0.0" {
		t.Errorf("data identity = %s %s (contract %s)", envelope.Data.ID, envelope.Data.Version, envelope.Data.ContractVersion)
	}
	if envelope.Data.AlreadyInstalled {
		t.Errorf("already_installed = true on a fresh install")
	}
	if _, err := time.Parse(time.RFC3339, envelope.Data.InstalledAt); err != nil {
		t.Errorf("installed_at %q is not RFC3339: %v", envelope.Data.InstalledAt, err)
	}
	if _, err := time.Parse(time.RFC3339, envelope.Data.UpdatedAt); err != nil {
		t.Errorf("updated_at %q is not RFC3339: %v", envelope.Data.UpdatedAt, err)
	}
	for _, raw := range []struct {
		name string
		data json.RawMessage
	}{{"resolution", envelope.Data.Resolution}, {"lifecycle", envelope.Data.Lifecycle}, {"compatibility", envelope.Data.Compatibility}, {"trust", envelope.Data.Trust}} {
		if len(raw.data) == 0 || string(raw.data) == "null" {
			t.Errorf("data.%s is missing", raw.name)
		}
	}
	if !strings.Contains(envelope.Data.RecordPath, id+".json") {
		t.Errorf("record_path = %q, want the record file for %s", envelope.Data.RecordPath, id)
	}
}

// ── Adoption Order ───────────────────────────────────────────────────

// TestStandardInstall_AdoptionOrderPinned pins the documented adoption
// order (TS-014-04): compatibility runs BEFORE the content fetch, and
// trust runs before the record is written. A compatibility failure must
// never reach the network, and a trust failure must never write a record.
//
// Reference: TS-014-03-01 (PM binding decision 2), TS-014-04
func TestStandardInstall_AdoptionOrderPinned(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	t.Run("compatibility failure aborts before any fetch", func(t *testing.T) {
		installTestEnv(t, server)
		indexDir := t.TempDir()
		anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
		// The release supports only framework 5.1.0; the project declares
		// framework 11.0.0 — not covered (same-major compatibility).
		md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
		installTestIndexEntry(t, indexDir, md)
		installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\nframework:\n  laravel:\n    version: 11.0.0\n")

		before := fetchCount
		_, _, stderr, err := executeCommand("standard", "install", id, version,
			"--index", indexDir, "--trust-anchors", anchorsFile)
		if err == nil {
			t.Fatal("install succeeded, want compatibility rejection")
		}
		if !strings.Contains(stderr, "not compatible") {
			t.Errorf("stderr missing compatibility rejection: %q", stderr)
		}
		if fetchCount != before {
			t.Errorf("content fetched %d time(s) on a compatibility failure — compatibility must gate before the network", fetchCount-before)
		}
		if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
			t.Errorf("record exists after a compatibility failure, want nothing recorded")
		}
	})

	t.Run("trust failure aborts before any record", func(t *testing.T) {
		// The release is attested over content; the server serves
		// DIFFERENT bytes — integrity verification must fail and abort
		// the install.
		tamperedServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("tampered content — not what the release claims"))
		}))
		defer tamperedServer.Close()

		installTestEnv(t, tamperedServer)
		indexDir := t.TempDir()
		anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
		md := installTestRelease(t, id, version, tamperedServer.URL+"/release.tar.gz",
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
		installTestIndexEntry(t, indexDir, md)

		_, _, stderr, err := executeCommand("standard", "install", id, version,
			"--index", indexDir, "--trust-anchors", anchorsFile)
		if err == nil {
			t.Fatal("install succeeded with tampered content, want trust rejection")
		}
		if !strings.Contains(stderr, "trust verification failed") {
			t.Errorf("stderr missing trust rejection: %q", stderr)
		}
		if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
			t.Errorf("record exists after a trust failure, want nothing recorded")
		}
	})
}

// TestStandardInstall_RecordRejectsNilTrust verifies the record step
// guards against a pre-fetch-only adoption result (Trust nil): recording
// would persist a half-validated adoption, so the step fails with an
// actionable error instead of panicking — protecting future callers
// (e.g. the update flow, T-008) that might pass a pre-fetch result by
// mistake.
func TestStandardInstall_RecordRejectsNilTrust(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	md := registry.Metadata{
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Capability: registry.Capability{
			FrameworkVersion: []string{"5.1.0"},
		},
		Lifecycle: registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	before := registry.ValidateAdoptionBeforeFetch(md, []int{1}, "5.1.0")
	if !before.Valid {
		t.Fatalf("pre-fetch result = %+v, want valid; errors: %v", before, before.Errors)
	}
	if before.Trust != nil {
		t.Fatal("pre-fetch result unexpectedly carries a trust result")
	}

	err := recordStandardInstall(&cobra.Command{}, md, "https://example.com/release.tar.gz", before, nil)
	if err == nil {
		t.Fatal("recordStandardInstall accepted a pre-fetch-only adoption result, want rejection")
	}
	if !strings.Contains(err.Error(), "no trust result") {
		t.Errorf("err = %v, want the actionable nil-trust rejection", err)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, md.ID)); !os.IsNotExist(statErr) {
		t.Errorf("record exists after the nil-trust rejection, want nothing recorded")
	}
}

// ── Compatibility Matrix (supported contract majors) ────────────────

// TestStandardInstall_CompatibilityMatrixUnreadable verifies that an
// unreadable compatibility matrix aborts the install with an actionable
// error BEFORE any content is fetched — the runtime's supported
// contract majors are read from the corpus matrix at runtime (T-010
// reviewer finding G4) and are never silently defaulted (PM binding
// decision 3). Nothing is recorded.
func TestStandardInstall_CompatibilityMatrixUnreadable(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	// Point the matrix resolution at a file that does not exist.
	t.Setenv(registry.EnvCompatibilityMatrix, filepath.Join(t.TempDir(), "no-matrix.json"))

	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	before := fetchCount
	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "compatibility matrix") {
		t.Errorf("stderr missing the matrix failure message: %q", stderr)
	}
	if !strings.Contains(stderr, registry.EnvCompatibilityMatrix) {
		t.Errorf("stderr missing the matrix override guidance: %q", stderr)
	}
	if fetchCount != before {
		t.Errorf("content fetched %d time(s) with an unreadable matrix, want 0 — the matrix gates before the network", fetchCount-before)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a matrix failure, want nothing recorded")
	}
}

// ── Idempotency ──────────────────────────────────────────────────────

// TestStandardInstall_IdempotentReinstall verifies that re-installing the
// same identity and version is idempotent (ADR-023 §3): the full
// validation still runs, the record's validation results are refreshed via
// Update (installedAt preserved, updatedAt re-stamped), the command
// reports "already installed (re-validated)", and no duplicate record is
// created.
//
// Reference: TS-014-03-01 (PM binding decision 6), TS-014-03-03
func TestStandardInstall_IdempotentReinstall(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	if _, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("first install failed: %v (stderr: %q)", err, stderr)
	}
	first := installTestReadRecord(t, id)

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("re-install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "already installed") || !strings.Contains(stdout, "(re-validated)") {
		t.Errorf("re-install stdout missing the already-installed report:\n%s", stdout)
	}

	second := installTestReadRecord(t, id)
	if !second.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installedAt changed across re-install: %v → %v — the original install time must be preserved", first.InstalledAt, second.InstalledAt)
	}
	if second.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updatedAt went backwards across re-install: %v → %v", first.UpdatedAt, second.UpdatedAt)
	}
	if second.UpdatedAt.Equal(second.InstalledAt) {
		t.Errorf("updatedAt = installedAt after a re-install, want the re-validation event re-stamped")
	}
	if second.Compatibility == nil || !second.Compatibility.Valid || second.Trust == nil || !second.Trust.Valid {
		t.Errorf("re-installed record lost its embedded validation results: compat=%+v trust=%+v", second.Compatibility, second.Trust)
	}

	// Exactly one record file: no duplicate install.
	entries, err := os.ReadDir(filepath.Dir(installTestRecordPath(t, id)))
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	var recordFiles int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			recordFiles++
		}
	}
	if recordFiles != 1 {
		t.Errorf("store contains %d record files after a re-install, want exactly 1", recordFiles)
	}
}

// TestStandardInstall_VersionConflict verifies that installing a
// different version over an installed standard is rejected with an
// actionable error pointing at the update flow (TS-014-03-02): a version
// change is an update, an explicit adoption event — this flow never
// updates.
//
// Reference: TS-014-03-01 (PM binding decisions 6, 9), TS-014-03-03
func TestStandardInstall_VersionConflict(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	installTestIndexEntry(t, indexDir, installTestRelease(t, id, "1.2.3", server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv))
	installTestIndexEntry(t, indexDir, installTestRelease(t, id, "1.2.4", server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv))

	if _, _, stderr, err := executeCommand("standard", "install", id, "1.2.3",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("first install failed: %v (stderr: %q)", err, stderr)
	}

	_, _, stderr, err := executeCommand("standard", "install", id, "1.2.4",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	// Version conflict is a configuration conflict → exit 2
	// (TS-019-03-02, D-06).
	requireExitCode(t, err, output.ExitCodeConfig)
	if !strings.Contains(stderr, "already installed at version") {
		t.Errorf("stderr missing version-conflict message: %q", stderr)
	}
	if !strings.Contains(stderr, "update flow") && !strings.Contains(stderr, "update") {
		t.Errorf("stderr missing the update-flow guidance: %q", stderr)
	}

	// The recorded version is unchanged: this flow never updates.
	rec := installTestReadRecord(t, id)
	if rec.Version != "1.2.3" {
		t.Errorf("record version = %s after a rejected conflict, want 1.2.3", rec.Version)
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────

// TestStandardInstall_RetiredRejected verifies that a retired release is
// not installable: LifecycleAdoptable false produces an actionable error
// distinguishing retired from not-found, nothing is fetched, and nothing
// is recorded.
//
// Reference: TS-014-03-01 (PM binding decision 1), TS-014-01-03,
// ADR-027 §3
func TestStandardInstall_RetiredRejected(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStateRetired, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "not offered for fresh adoption") {
		t.Errorf("stderr missing the retired-not-adoptable message: %q", stderr)
	}
	if fetchCount != 0 {
		t.Errorf("content fetched %d times for a retired release, want 0", fetchCount)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists for a retired release, want nothing recorded")
	}
}

// TestStandardInstall_DeprecatedWarning verifies that a deprecated
// release installs WITH a warning: the warning states the deprecation,
// the removal date, and the no-updates note; the installed record keeps
// the deprecated lifecycle state.
//
// Reference: TS-014-03-01 (DoD: deprecated standards install with a
// warning; PM binding decision 8), TS-014-01-03, ADR-023 §3, ADR-027 §3
func TestStandardInstall_DeprecatedWarning(t *testing.T) {
	const (
		id      = "anvil-standard-flutter"
		version = "2.0.0"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStateDeprecated, "2027-01-01T00:00:00Z", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("deprecated install failed, want success with warning: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Warnings:") {
		t.Errorf("stdout missing the Warnings section:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deprecated") || !strings.Contains(stdout, "2027-01-01T00:00:00Z") {
		t.Errorf("stdout missing the deprecation warning with removal date:\n%s", stdout)
	}
	if !strings.Contains(stdout, "no updates") {
		t.Errorf("stdout missing the no-updates note:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Lifecycle.State != registry.LifecycleStateDeprecated || rec.Lifecycle.RemovalDate != "2027-01-01T00:00:00Z" {
		t.Errorf("record lifecycle = %+v, want deprecated with the removal date", rec.Lifecycle)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid || rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("deprecated install record missing embedded validation results")
	}
}

// ── Not Found / Invalid ──────────────────────────────────────────────

// TestStandardInstall_NotFounds verifies the not-found exit code 3
// contract: a standard missing from the index and a version missing from
// the index both fail with exit code 3 (TS-P8-07).
//
// Reference: TS-014-03-01 (PM binding decision 1)
func TestStandardInstall_NotFounds(t *testing.T) {
	content := installTestStandardContent("anvil-standard-laravel")
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), "anvil-standard-laravel", pub)
	md := installTestRelease(t, "anvil-standard-laravel", "1.2.3", server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	t.Run("standard not in the index", func(t *testing.T) {
		_, _, stderr, err := executeCommand("standard", "install", "anvil-standard-missing", "1.0.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !strings.Contains(stderr, "not found") {
			t.Errorf("stderr missing not-found message: %q", stderr)
		}
	})

	t.Run("version not in the index", func(t *testing.T) {
		_, _, stderr, err := executeCommand("standard", "install", "anvil-standard-laravel", "9.9.9",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !strings.Contains(stderr, "not found") {
			t.Errorf("stderr missing not-found message: %q", stderr)
		}
	})
}

// TestStandardInstall_MalformedEntry verifies that an entry that
// structurally decodes but fails strict registry validation aborts the
// install with the actionable validation problem (exit 1) — the
// structural decode alone is never trusted.
//
// Reference: TS-014-03-01 (PM binding decision 2), TS-014-01-02
func TestStandardInstall_MalformedEntry(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	// Lifecycle state "unknown-state" structurally decodes but fails the
	// strict parse (lifecycle enum).
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		"unknown-state", "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "is invalid") {
		t.Errorf("stderr missing invalid-release message: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists for a malformed entry, want nothing recorded")
	}
}

// ── Trust / Anchors ──────────────────────────────────────────────────

// TestStandardInstall_NoAnchors verifies the default-fail anchor behavior
// (ADR-022 §3; PM decision D-07): an install without a configured anchor
// fails with an actionable message naming the publisher, the anchor path,
// the --trust-anchors flag, and the ANVIL_TRUST_ANCHORS environment
// variable — there is no first-use acceptance and no privileged path.
//
// Reference: TS-014-03-01 (PM binding decisions 4, 10)
func TestStandardInstall_NoAnchors(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	t.Run("trust anchors file missing", func(t *testing.T) {
		before := fetchCount
		_, _, stderr, err := executeCommand("standard", "install", id, version,
			"--index", indexDir, "--trust-anchors", filepath.Join(t.TempDir(), "no-anchors.json"))
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "no trust anchors file found") {
			t.Errorf("stderr missing the missing-anchors message: %q", stderr)
		}
		if !strings.Contains(stderr, "--trust-anchors") || !strings.Contains(stderr, registry.EnvTrustAnchors) {
			t.Errorf("stderr missing the anchors override guidance: %q", stderr)
		}
		// Anchors load BEFORE the fetch (local operation): a missing
		// anchors file must fail fast without any download.
		if fetchCount != before {
			t.Errorf("content fetched %d time(s) with a missing anchors file, want 0 — anchors must gate before the fetch", fetchCount-before)
		}
		if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
			t.Errorf("record exists after a missing-anchors failure, want nothing recorded")
		}
	})

	t.Run("empty allowlist fails with the actionable no-anchor message", func(t *testing.T) {
		emptyAnchors := filepath.Join(t.TempDir(), "trust-anchors.json")
		if err := os.WriteFile(emptyAnchors, []byte(`{"publishers": {}}`), 0o644); err != nil {
			t.Fatalf("write empty anchors: %v", err)
		}
		_, _, stderr, err := executeCommand("standard", "install", id, version,
			"--index", indexDir, "--trust-anchors", emptyAnchors)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "trust verification failed") {
			t.Errorf("stderr missing the trust rejection: %q", stderr)
		}
		if !strings.Contains(stderr, id) || !strings.Contains(stderr, "--trust-anchors") || !strings.Contains(stderr, registry.EnvTrustAnchors) {
			t.Errorf("stderr missing publisher/override guidance: %q", stderr)
		}
		if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
			t.Errorf("record exists without anchors, want nothing recorded")
		}
	})

	t.Run("publisher without an anchor is rejected", func(t *testing.T) {
		anchorsFile := installTestAnchorsFile(t, t.TempDir(), "anvil-standard-other", pub)
		_, _, stderr, err := executeCommand("standard", "install", id, version,
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "unknown publisher") || !strings.Contains(stderr, id) {
			t.Errorf("stderr missing the unknown-publisher rejection: %q", stderr)
		}
	})
}

// TestStandardInstall_AnchorsEnvVar verifies the ANVIL_TRUST_ANCHORS
// environment variable resolves the anchors file when no flag is passed.
//
// Reference: TS-014-03-01, TS-014-04-02
func TestStandardInstall_AnchorsEnvVar(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	t.Setenv(registry.EnvTrustAnchors, anchorsFile)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version, "--index", indexDir)
	if err != nil {
		t.Fatalf("install with ANVIL_TRUST_ANCHORS failed: %v (stderr: %q)", err, stderr)
	}
	rec := installTestReadRecord(t, id)
	if rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("record trust = %+v, want valid", rec.Trust)
	}
}

// ── Content Fetch Policy ─────────────────────────────────────────────

// TestStandardInstall_Fetch404 verifies that a missing release asset
// aborts the install with an actionable error (HTTP 404, publisher fix vs
// adopter choose another version), and nothing is recorded.
//
// Reference: TS-014-03-01 (PM binding decision 5), TS-014-02-03
func TestStandardInstall_Fetch404(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/missing.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "could not fetch") {
		t.Errorf("stderr missing the fetch failure message: %q", stderr)
	}
	if !strings.Contains(stderr, "404") {
		t.Errorf("stderr missing the HTTP status: %q", stderr)
	}
	if !strings.Contains(stderr, "publisher") {
		t.Errorf("stderr missing the two-audience guidance: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a fetch failure, want nothing recorded")
	}
}

// TestStandardInstall_RedirectToNonHTTPS verifies the redirect policy:
// a redirect to a non-https target is refused — release content is
// fetched over TLS only (ADR-030 §3).
//
// Reference: TS-014-03-01 (PM binding decision 5)
func TestStandardInstall_RedirectToNonHTTPS(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/release.tar.gz", http.StatusFound)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "TLS only") && !strings.Contains(stderr, "https") {
		t.Errorf("stderr missing the non-https redirect rejection: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a refused redirect, want nothing recorded")
	}
}

// TestStandardInstall_RedirectToNonHTTPSUserinfo verifies the redirect
// policy on the INSTALL surface for a non-https target carrying
// userinfo (QA F-2): the refusal renders the target WITHOUT its
// credentials — neither the username nor the password nor the userinfo
// '@' appears in the error — while keeping the redacted target form
// visible, and nothing is recorded. The fetch client is shared with the
// update flow, so this pins the same scrubbed refusal both surfaces
// surface.
//
// Reference: TS-014-03-01 (PM binding decision 5), TS-014-03-02 (fix
// round 3), ADR-030 §3
func TestStandardInstall_RedirectToNonHTTPSUserinfo(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	redirectCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		// Non-https + userinfo: the redirect policy refuses the target
		// outright — the refusal message must not echo the credentials.
		http.Redirect(w, r, "http://alice:secret@127.0.0.1:9/final.tar.gz", http.StatusFound)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "TLS only") && !strings.Contains(stderr, "https") {
		t.Errorf("stderr missing the non-https redirect rejection: %q", stderr)
	}
	// The refusal surfaces the REDACTED target form.
	if !strings.Contains(stderr, "http://127.0.0.1:9/final.tar.gz") {
		t.Errorf("stderr missing the redacted target form: %q", stderr)
	}
	for _, fragment := range []string{"alice", "secret", "@"} {
		if strings.Contains(stderr, fragment) {
			t.Errorf("stderr leaks the credential fragment %q: %q — credentials must never appear in errors", fragment, stderr)
		}
	}
	if redirectCount != 1 {
		t.Errorf("redirect requested %d time(s), want exactly 1", redirectCount)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a refused redirect, want nothing recorded")
	}
}

// TestStandardInstall_SizeCapExceeded verifies that content exceeding the
// size cap is rejected DURING the download (the cap is enforced via a
// limit reader, never buffered unbounded), and nothing is recorded.
//
// Reference: TS-014-03-01 (PM binding decision 5)
func TestStandardInstall_SizeCapExceeded(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
	}))
	defer server.Close()

	installTestEnv(t, server)
	origCap := standardContentMaxBytes
	standardContentMaxBytes = 1024
	t.Cleanup(func() { standardContentMaxBytes = origCap })

	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "size cap") {
		t.Errorf("stderr missing the size-cap rejection: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a cap rejection, want nothing recorded")
	}
}

// TestStandardInstall_FetchTimeout verifies that a stalled content server
// surfaces an explicit timeout error (shared timed client, TD-008 §4).
//
// Reference: TS-014-03-01 (PM binding decision 5), TD-008 §4
func TestStandardInstall_FetchTimeout(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	// Shrink the shared client timeout so the test does not wait on the
	// production default deadline; the install client is rebuilt from the
	// shared timeout while keeping the transport that trusts the test
	// server's TLS certificate.
	origHTTP := httpClient
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	t.Cleanup(func() { httpClient = origHTTP })
	origInstall := standardInstallHTTPClient
	client := newStandardInstallHTTPClient()
	client.Transport = standardInstallHTTPClient.Transport
	standardInstallHTTPClient = client
	t.Cleanup(func() { standardInstallHTTPClient = origInstall })

	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "timed out") {
		t.Errorf("stderr missing the timeout message: %q", stderr)
	}
}

// TestStandardInstall_UserinfoRejected verifies that a distribution
// location carrying userinfo (https://user:pass@host/...) is rejected:
// the strict parse (the authoritative validation point) rejects the
// document, the install aborts before any fetch, and nothing is
// recorded — credentials are never sent, echoed, or persisted.
//
// Reference: TS-014-03-01 (security finding 1), TS-014-01-02
func TestStandardInstall_UserinfoRejected(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, "https://alice:secret@example.com/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "userinfo") {
		t.Errorf("stderr missing the userinfo rejection: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a userinfo rejection, want nothing recorded")
	}
}

// TestStandardInstall_FetchBoundaryUserinfo verifies the defense-in-depth
// userinfo check at the fetch boundary: a location that bypassed parse
// and ResolveLocation (a future index source) is still refused before
// any request is issued — credentials must never be sent as Basic auth.
// The rejection message renders the location WITHOUT its credentials:
// credentials are never sent, echoed, or persisted (reviewer F1).
//
// Reference: TS-014-03-01 (security finding 1), TS-014-03-02 (fix round 1)
func TestStandardInstall_FetchBoundaryUserinfo(t *testing.T) {
	_, _, err := fetchStandardContent("https://alice:secret@example.com/release.tar.gz")
	if err == nil {
		t.Fatal("fetchStandardContent accepted a userinfo URL, want rejection")
	}
	if !strings.Contains(err.Error(), "userinfo") {
		t.Errorf("error = %v, want the userinfo rejection", err)
	}
	for _, credential := range []string{"alice", "secret"} {
		if strings.Contains(err.Error(), credential) {
			t.Errorf("error echoes the credential %q: %v — credentials must never appear in errors", credential, err)
		}
	}
	if strings.Contains(err.Error(), "@") {
		t.Errorf("error keeps userinfo syntax (%v) — the location must be rendered without credentials", err)
	}
}

// TestStandardInstall_RedirectChainBounded verifies the redirect chain is
// bounded: 10 redirects are followed, the 11th hop is refused, and the
// install aborts with nothing recorded.
//
// Reference: TS-014-03-01 (reviewer finding 6a), ADR-030 §3
func TestStandardInstall_RedirectChainBounded(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release.tar.gz" {
			_, _ = w.Write(content)
			return
		}
		// /r0 → /r1 → ... → /r9 → /r10: the redirect to /r10 is the 10th
		// redirect decision, which the bounded policy refuses.
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "https://"+r.Host+"/r"+strconv.Itoa(n+1), http.StatusFound)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/r0",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "stopped after 10 redirects") {
		t.Errorf("stderr missing the redirect-chain rejection: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a refused redirect chain, want nothing recorded")
	}
}

// TestStandardInstall_HTTPSRedirectAllowed verifies that an allowed https
// redirect is followed, the fetch succeeds, and the record's resolution
// source is the ACTUAL endpoint used — the final response URL after the
// redirect, not the declared location (ADR-022 §3: resolution is
// explicit and recorded).
//
// Reference: TS-014-03-01 (reviewer finding 6b), ADR-022 §3
func TestStandardInstall_HTTPSRedirectAllowed(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://"+r.Host+"/final.tar.gz", http.StatusFound)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/redirect",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install with an allowed https redirect failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, server.URL+"/final.tar.gz") {
		t.Errorf("stdout missing the final-URL resolution:\n%s", stdout)
	}
	rec := installTestReadRecord(t, id)
	wantSource := server.URL + "/final.tar.gz"
	if rec.Resolution.Source != wantSource {
		t.Errorf("record resolution.source = %q, want the final response URL %q", rec.Resolution.Source, wantSource)
	}
}

// TestStandardInstall_Fetch5xx verifies that a server error (e.g. 503)
// aborts the install with an actionable error and nothing is recorded.
//
// Reference: TS-014-03-01 (reviewer finding 6c), TS-014-02-03
func TestStandardInstall_Fetch5xx(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "503") {
		t.Errorf("stderr missing the HTTP status: %q", stderr)
	}
	if !strings.Contains(stderr, "publisher") {
		t.Errorf("stderr missing the two-audience guidance: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after a 5xx failure, want nothing recorded")
	}
}

// TestStandardInstall_AnchorsDefaultPath verifies the documented default
// trust anchors path (<user config dir>/anvil/trust-anchors.json) is used
// when neither the flag nor the environment variable is set.
//
// Reference: TS-014-03-01 (reviewer finding 6d), TS-014-04-02
func TestStandardInstall_AnchorsDefaultPath(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	// Place the allowlist at the resolved default path — no flag, no env.
	defaultPath, err := registry.DefaultTrustAnchorsPath()
	if err != nil {
		t.Fatalf("default trust anchors path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(defaultPath), err)
	}
	if err := os.WriteFile(defaultPath, mustMarshal(t, map[string]interface{}{
		"publishers": map[string]string{id: base64.StdEncoding.EncodeToString(pub)},
	}), 0o644); err != nil {
		t.Fatalf("write default anchors: %v", err)
	}

	indexDir := t.TempDir()
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("standard", "install", id, version, "--index", indexDir)
	if err != nil {
		t.Fatalf("install with default-path anchors failed: %v (stderr: %q)", err, stderr)
	}
	rec := installTestReadRecord(t, id)
	if rec.Trust == nil || !rec.Trust.Valid || rec.Trust.AnchorPath != defaultPath {
		t.Errorf("record trust = %+v, want valid with anchor path %s", rec.Trust, defaultPath)
	}
}

// mustMarshal marshals v to JSON for test fixtures, failing the test on
// error.
func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

// ── Project Framework Version Handling ───────────────────────────────

// TestStandardInstall_FrameworkDeclaredUndeterminable verifies that a
// project declaring a framework without a determinable version REJECTS
// the install with an actionable error (never assumed — Transition Plan
// A2; PM binding decision 3).
//
// Reference: TS-014-03-01 (PM binding decision 3), Transition Plan A2
func TestStandardInstall_FrameworkDeclaredUndeterminable(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)
	installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\n")

	_, _, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "framework version cannot be determined") && !strings.Contains(stderr, "cannot be determined") {
		t.Errorf("stderr missing the undeterminable-version rejection: %q", stderr)
	}
	if !strings.Contains(stderr, "framework.laravel.version") {
		t.Errorf("stderr missing the declaration guidance: %q", stderr)
	}
	if _, err := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(err) {
		t.Errorf("record exists after the rejection, want nothing recorded")
	}
}

// TestStandardInstall_FrameworkFreeProject verifies that a framework-free
// project (no framework declared — ADR-026) installs with shape-only
// capability validation: the compatibility result records
// FrameworkVersionChecked=false explicitly — the not-checked fact is
// recorded, never hidden — and the install proceeds.
//
// Reference: TS-014-03-01 (PM binding decision 3), ADR-026
func TestStandardInstall_FrameworkFreeProject(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)
	installTestProject(t, t.TempDir(), "project:\n  name: my-app\n")

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("framework-free install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "shape-only") || !strings.Contains(stdout, "not checked") {
		t.Errorf("stdout must surface the shape-only validation explicitly:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Compatibility == nil || !rec.Compatibility.Valid {
		t.Fatalf("record compatibility = %+v, want a valid shape-only result", rec.Compatibility)
	}
	if rec.Compatibility.FrameworkVersionChecked {
		t.Errorf("frameworkVersionChecked = true in a framework-free project, want false (recorded, not assumed)")
	}
	if rec.Compatibility.ProjectFrameworkVersion != "" {
		t.Errorf("projectFrameworkVersion = %q, want empty", rec.Compatibility.ProjectFrameworkVersion)
	}
}

// TestStandardInstall_FrameworkVersionChecked verifies that a project
// declaring a framework AND its version validates against the declared
// capability scope: FrameworkVersionChecked=true with the project version
// recorded.
//
// Reference: TS-014-03-01 (PM binding decision 3)
func TestStandardInstall_FrameworkVersionChecked(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0", "5.2.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)
	installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\nframework:\n  laravel:\n    version: 5.2.1\n")

	_, stdout, stderr, err := executeCommand("standard", "install", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "framework version 5.2.1 checked") {
		t.Errorf("stdout missing the checked-framework summary:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Compatibility == nil || !rec.Compatibility.Valid || !rec.Compatibility.FrameworkVersionChecked {
		t.Errorf("record compatibility = %+v, want valid with frameworkVersionChecked", rec.Compatibility)
	}
	if rec.Compatibility.ProjectFrameworkVersion != "5.2.1" {
		t.Errorf("record projectFrameworkVersion = %q, want 5.2.1", rec.Compatibility.ProjectFrameworkVersion)
	}
}

// ── JSON Error Envelope ──────────────────────────────────────────────

// TestStandardInstall_JSONErrorEnvelope verifies that a failing install
// with --json produces the TS-P8-05 error envelope on stdout (the
// machine-readable error surface; the envelope carries the failure while
// the human-readable path carries the exit code).
//
// Reference: TS-014-03-01, TS-P8-05
func TestStandardInstall_JSONErrorEnvelope(t *testing.T) {
	content := installTestStandardContent("anvil-standard-laravel")
	pub, _ := installTestKeypair(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), "anvil-standard-laravel", pub)

	_, stdout, _, _ := executeCommand("standard", "install", "anvil-standard-missing", "1.0.0",
		"--index", indexDir, "--trust-anchors", anchorsFile, "--json")

	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not the JSON error envelope: %v\n%s", err, stdout)
	}
	if envelope.Version != "1" || envelope.Status != "error" {
		t.Errorf("envelope = %s/%s, want version 1 status error", envelope.Version, envelope.Status)
	}
	if !strings.Contains(envelope.Error, "not found") {
		t.Errorf("error envelope missing the not-found message: %q", envelope.Error)
	}
}
