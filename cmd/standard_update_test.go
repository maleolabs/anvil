// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil standard update" (TS-014-03-02): the explicit update
// flow — installed-record precondition, the update lifecycle gate
// (installed deprecated → no updates), re-validation exactly as at
// installation (resolve → strict parse → lifecycle gate → compatibility
// → location → fetch → trust → record update), record semantics
// (installedAt preserved, updatedAt refreshed, new resolution + embedded
// results), idempotency (already-at-version), retired/deprecated target
// handling, the adoption-order pins (compat failure ⇒ zero fetch; trust
// failure ⇒ record unchanged), fetch failures, framework gates, and the
// exit code / --json conventions.
//
// Every test is self-contained: the static index, the trust anchors
// file, the project config, and the global config directory (XDG_CONFIG_HOME
// — record store) live in t.TempDir(); release content is served by a
// local https test server. Tests reuse the install-flow fixtures
// (standard_install_test.go) so both flows are exercised against the
// same adoption machinery.
//
// Reference: TS-014-03-02, TS-014-03-01, TS-014-03-03, TS-014-04,
// ADR-022 §3, ADR-023 §3, ADR-027 §3, ADR-030 §3
package cmd

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Test Fixtures ────────────────────────────────────────────────────

// updateTestKeypair carries the publisher key pair a fixture release is
// attested with — the SAME pair every release of a standard uses within
// one test, so the anchors file always matches.
type updateTestKeypair struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// updateTestContent returns deterministic release content for the given
// version (distinct per version, so an update demonstrably adopts the
// target's content).
func updateTestContent(id, version string) []byte {
	return []byte("release content for " + id + " " + version + " (TS-014-03-02 explicit update flow tests)")
}

// updateTestRelease writes one release document into the index AND
// returns the metadata: the release is attested over content with keys.
func updateTestRelease(t *testing.T, indexDir, id, version, location, lifecycleState, removalDate string, keys updateTestKeypair, content []byte) registry.Metadata {
	t.Helper()
	md := installTestRelease(t, id, version, location, lifecycleState, removalDate,
		[]string{"5.1.0"}, content, keys.pub, keys.priv)
	installTestIndexEntry(t, indexDir, md)
	return md
}

// updateTestInstallBase installs version 1.2.3 (published) through the
// install command and returns the index dir, the anchors file, the
// keypair, and the content, against the given content server. The
// server must serve the 1.2.3 content at /release.tar.gz and the target
// content at /release-v2.tar.gz.
func updateTestInstallBase(t *testing.T, server *httptest.Server) (indexDir, anchorsFile, id string, keys updateTestKeypair, content []byte) {
	t.Helper()
	id = "anvil-standard-laravel"
	content = updateTestContent(id, "1.2.3")
	pub, priv := installTestKeypair(t)
	keys = updateTestKeypair{pub: pub, priv: priv}

	installTestEnv(t, server)
	indexDir = t.TempDir()
	anchorsFile = installTestAnchorsFile(t, t.TempDir(), id, pub)
	updateTestRelease(t, indexDir, id, "1.2.3", server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", keys, content)

	if _, _, stderr, err := executeCommand("standard", "install", id, "1.2.3",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("fixture install failed: %v (stderr: %q)", err, stderr)
	}
	return indexDir, anchorsFile, id, keys, content
}

// ── Command Group Registration ───────────────────────────────────────

// TestStandardUpdateCommand_Registered verifies that the update command
// is registered in the standard group: updates are available only
// through this explicit command surface — nothing in the CLI updates a
// standard implicitly (no auto-update path, DoD TS-014-03-02).
//
// Reference: TS-014-03-02 (DoD: update requires explicit user
// invocation), ADR-023 §3
func TestStandardUpdateCommand_Registered(t *testing.T) {
	_, _, err := rootCmd.Find([]string{"standard", "update"})
	if err != nil {
		t.Fatalf("standard update command not found: %v", err)
	}
	_, helpOut, _, err := executeCommand("standard", "--help")
	if err != nil {
		t.Fatalf("standard --help failed: %v", err)
	}
	if !strings.Contains(helpOut, "update") {
		t.Errorf("standard group help does not list the update subcommand:\n%s", helpOut)
	}
}

// ── Success Path ─────────────────────────────────────────────────────

// TestStandardUpdate_Success updates an installed published release to a
// new published version end to end: the full adoption validation runs
// against the TARGET version (compatibility + trust), the record is
// updated atomically with the new pinned version, the NEW explicit
// resolution (the actual endpoint used), the target's lifecycle state,
// and freshly embedded validation results; installedAt (the original
// install time) is preserved and updatedAt is refreshed.
//
// Reference: TS-014-03-02 (DoD: updated version and resolution are
// recorded; compatibility and trust are re-validated exactly as at
// installation; PM binding decision 6)
func TestStandardUpdate_Success(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.3.0"
	)
	v2Content := updateTestContent(id, version)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	// The target release is attested with the SAME publisher key the
	// standard is anchored with — the trust anchor allowlist matches.
	updateTestRelease(t, indexDir, id, version, server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	first := installTestReadRecord(t, id)
	preSource := first.Resolution.Source

	cmd, stdout, stderr, err := executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("update failed: %v (stderr: %q)", err, stderr)
	}
	if cmd == nil {
		t.Fatal("executeCommand returned a nil command")
	}
	if !strings.Contains(stdout, "Updated standard: "+id+" "+version) {
		t.Errorf("stdout missing the success line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "distribution") || !strings.Contains(stdout, server.URL+"/release-v2.tar.gz") {
		t.Errorf("stdout missing the new resolution details:\n%s", stdout)
	}
	if !strings.Contains(stdout, "trust: ok") || !strings.Contains(stdout, "compatibility: ok") {
		t.Errorf("stdout missing validation summary:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Installed At (original install):") || !strings.Contains(stdout, "Updated At (this update):") {
		t.Errorf("stdout missing the record-semantics sections:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.FormatVersion != registry.RecordFormatVersion {
		t.Errorf("record formatVersion = %d, want %d", rec.FormatVersion, registry.RecordFormatVersion)
	}
	if rec.ID != id || rec.Version != version {
		t.Errorf("record identity = %s %s, want %s %s", rec.ID, rec.Version, id, version)
	}
	if rec.ContractVersion != "1.0.0" {
		t.Errorf("record contractVersion = %q, want the target's declared 1.0.0", rec.ContractVersion)
	}
	if rec.Resolution.Kind != registry.ResolutionKindDistribution || rec.Resolution.Source != server.URL+"/release-v2.tar.gz" {
		t.Errorf("record resolution = %+v, want kind distribution with the NEW endpoint used", rec.Resolution)
	}
	if rec.Resolution.Source == preSource {
		t.Errorf("record resolution.source unchanged (%s) — the update must record the new resolution", preSource)
	}
	if rec.Lifecycle.State != registry.LifecycleStatePublished {
		t.Errorf("record lifecycle = %q, want published", rec.Lifecycle.State)
	}
	if !rec.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installedAt changed across update: %v → %v — the original install time must be preserved", first.InstalledAt, rec.InstalledAt)
	}
	if rec.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updatedAt went backwards across update: %v → %v", first.UpdatedAt, rec.UpdatedAt)
	}
	if !rec.UpdatedAt.After(first.InstalledAt) {
		t.Errorf("updatedAt %v must be after the preserved installedAt %v", rec.UpdatedAt, rec.InstalledAt)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid || rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("updated record missing embedded validation results: compat=%+v trust=%+v", rec.Compatibility, rec.Trust)
	}
	if rec.Trust.AnchorPath != anchorsFile {
		t.Errorf("record trust anchor path = %q, want %q (the anchor used)", rec.Trust.AnchorPath, anchorsFile)
	}
	// The fixture runs outside any project: framework-free shape-only
	// capability validation, recorded explicitly (ADR-026).
	if rec.Compatibility.FrameworkVersionChecked {
		t.Errorf("record compatibility frameworkVersionChecked = true, want false (no project → shape-only)")
	}
}

// TestStandardUpdate_SuccessJSON verifies the --json envelope shape
// (TS-P8-05): success envelope with the update data — identity, pinned
// target version, resolution, lifecycle, the preserved install time and
// refreshed update time, validation results, and the already-at-version
// marker.
//
// Reference: TS-014-03-02, TS-P8-05
func TestStandardUpdate_SuccessJSON(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.3.0"
	)
	v2Content := updateTestContent(id, version)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	updateTestRelease(t, indexDir, id, version, server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	_, stdout, stderr, err := executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile, "--json")
	if err != nil {
		t.Fatalf("update failed: %v (stderr: %q)", err, stderr)
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
			AlreadyAtVersion bool            `json:"already_at_version"`
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
	if envelope.Data.AlreadyAtVersion {
		t.Errorf("already_at_version = true on a version-change update")
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

// ── Idempotency ──────────────────────────────────────────────────────

// TestStandardUpdate_AlreadyAtVersion verifies that updating to the
// version already installed is idempotent (PM binding decision 7): the
// full validation still runs (the content is fetched and verified), the
// record's validation results are refreshed via Update (installedAt
// preserved, updatedAt re-stamped), and the command reports "already at
// version X (re-validated)" — machine-readable via already_at_version.
//
// Reference: TS-014-03-02 (PM binding decision 7), TS-014-03-03
func TestStandardUpdate_AlreadyAtVersion(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := updateTestContent(id, version)
	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
	first := installTestReadRecord(t, id)
	// The base install already fetched once; count the fetches the
	// update itself performs.
	fetchesBeforeUpdate := fetchCount

	_, stdout, stderr, err := executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("same-version update failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "already at version") || !strings.Contains(stdout, "(re-validated)") {
		t.Errorf("stdout missing the already-at-version report:\n%s", stdout)
	}
	if fetchCount != fetchesBeforeUpdate+1 {
		t.Errorf("content fetched %d time(s) by the already-at-version update, want exactly 1 — the full validation must still run", fetchCount-fetchesBeforeUpdate)
	}

	second := installTestReadRecord(t, id)
	if !second.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installedAt changed across the already-at-version update: %v → %v", first.InstalledAt, second.InstalledAt)
	}
	if second.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updatedAt went backwards: %v → %v", first.UpdatedAt, second.UpdatedAt)
	}
	if second.Compatibility == nil || !second.Compatibility.Valid || second.Trust == nil || !second.Trust.Valid {
		t.Errorf("record lost its embedded validation results after re-validation: compat=%+v trust=%+v", second.Compatibility, second.Trust)
	}

	// Machine-readable marker.
	_, stdout, stderr, err = executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile, "--json")
	if err != nil {
		t.Fatalf("same-version update (--json) failed: %v (stderr: %q)", err, stderr)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			AlreadyAtVersion bool `json:"already_at_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not the JSON envelope: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" || !envelope.Data.AlreadyAtVersion {
		t.Errorf("envelope = %s already_at_version=%v, want success + true", envelope.Status, envelope.Data.AlreadyAtVersion)
	}
}

// ── Preconditions ────────────────────────────────────────────────────

// TestStandardUpdate_NotInstalled verifies the install-first
// precondition (PM binding decision 2): updating a standard that has no
// record fails with an actionable error naming the install flow, exit
// code 1, nothing is fetched, and no record is created.
//
// Reference: TS-014-03-02 (PM binding decision 2)
func TestStandardUpdate_NotInstalled(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := updateTestContent(id, version)
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	updateTestRelease(t, indexDir, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", keys, content)

	_, _, stderr, err := executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	// Not installed is a missing prerequisite → exit 4 (TS-019-03-02,
	// D-02).
	requireExitCode(t, err, output.ExitCodePrecondition)
	if !strings.Contains(stderr, "not installed") {
		t.Errorf("stderr missing the not-installed message: %q", stderr)
	}
	if !strings.Contains(stderr, "install") {
		t.Errorf("stderr missing the install-first guidance: %q", stderr)
	}
	if fetchCount != 0 {
		t.Errorf("content fetched %d times for a not-installed standard, want 0", fetchCount)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record created for a not-installed standard, want nothing recorded")
	}
}

// TestStandardUpdate_CorruptRecord verifies that a corrupt
// installed-standard record aborts the update with recovery guidance:
// the installed lifecycle state cannot be evaluated from an unreadable
// record, so the update refuses (it must not bypass the update
// lifecycle gate) and points at the install flow, which recovers by
// re-adoption (TS-014-03-03).
//
// Reference: TS-014-03-02, TS-014-03-03
func TestStandardUpdate_CorruptRecord(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := updateTestContent(id, version)
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	updateTestRelease(t, indexDir, id, version, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", keys, content)

	// A corrupt record file where the store expects the record.
	recordPath := installTestRecordPath(t, id)
	if err := os.MkdirAll(filepath.Dir(recordPath), 0o755); err != nil {
		t.Fatalf("mkdir record dir: %v", err)
	}
	if err := os.WriteFile(recordPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	_, _, stderr, err := executeCommand("standard", "update", id, version,
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "cannot be read") {
		t.Errorf("stderr missing the corrupt-record message: %q", stderr)
	}
	if !strings.Contains(stderr, "install") {
		t.Errorf("stderr missing the re-install recovery guidance: %q", stderr)
	}
	if fetchCount != 0 {
		t.Errorf("content fetched %d times with a corrupt record, want 0", fetchCount)
	}
}

// ── Lifecycle Gates ──────────────────────────────────────────────────

// TestStandardUpdate_InstalledDeprecated verifies the installed-side
// lifecycle gate (PM binding decision 4; ADR-023 §3): a deprecated
// INSTALLED standard receives no updates — the update is rejected with
// an actionable error explaining the no-updates rule, exit code 1, and
// the record stays untouched. Nothing is resolved or fetched.
//
// Reference: TS-014-03-02 (PM binding decision 4; DoD: deprecated
// standards receive no updates), TS-014-01-03, ADR-023 §3, ADR-027 §3
func TestStandardUpdate_InstalledDeprecated(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := updateTestContent(id, "1.2.3")
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write(content)
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	// 1.2.3 installs as deprecated (deprecated releases install with a
	// warning); 1.3.0 is a published target the update would otherwise
	// adopt.
	updateTestRelease(t, indexDir, id, "1.2.3", server.URL+"/release.tar.gz",
		registry.LifecycleStateDeprecated, "2027-01-01T00:00:00Z", keys, content)
	updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, updateTestContent(id, "1.3.0"))

	if _, _, stderr, err := executeCommand("standard", "install", id, "1.2.3",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("fixture install of the deprecated release failed: %v (stderr: %q)", err, stderr)
	}
	before := installTestReadRecord(t, id)
	fetchesBeforeUpdate := fetchCount

	_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "receives no updates") {
		t.Errorf("stderr missing the no-updates rejection: %q", stderr)
	}
	if !strings.Contains(stderr, "no updates") || !strings.Contains(stderr, "ADR-023") {
		t.Errorf("stderr missing the no-updates rule citation: %q", stderr)
	}
	if fetchCount != fetchesBeforeUpdate {
		t.Errorf("content fetched %d time(s) by the update on a deprecated installed standard, want 0 — the gate must run before any fetch", fetchCount-fetchesBeforeUpdate)
	}

	after := installTestReadRecord(t, id)
	if after.Version != "1.2.3" || !after.InstalledAt.Equal(before.InstalledAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("record changed across the rejected update: before=%+v after=%+v", before, after)
	}
	if after.Lifecycle.State != registry.LifecycleStateDeprecated {
		t.Errorf("record lifecycle = %q, want the unchanged deprecated state", after.Lifecycle.State)
	}
}

// TestStandardUpdate_TargetDeprecated verifies the target-side
// deprecated decision (PM binding decision 4): when the INSTALLED
// standard is published and the TARGET version is deprecated, the
// update proceeds WITH the deprecation warning — the update is itself
// the explicit adoption event — and the updated record keeps the
// deprecated lifecycle state, so the no-updates rule becomes
// self-enforcing: a subsequent update attempt is rejected.
//
// Reference: TS-014-03-02 (PM binding decision 4), TS-014-01-03,
// ADR-023 §3, ADR-027 §3
func TestStandardUpdate_TargetDeprecated(t *testing.T) {
	const id = "anvil-standard-laravel"
	v2Content := updateTestContent(id, "1.3.0")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStateDeprecated, "2027-01-01T00:00:00Z", keys, v2Content)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "1.3.0",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("update to a deprecated target failed, want success with warning: %v (stderr: %q)", err, stderr)
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
	if rec.Version != "1.3.0" {
		t.Errorf("record version = %s, want the deprecated target 1.3.0", rec.Version)
	}
	if rec.Lifecycle.State != registry.LifecycleStateDeprecated || rec.Lifecycle.RemovalDate != "2027-01-01T00:00:00Z" {
		t.Errorf("record lifecycle = %+v, want deprecated with the removal date", rec.Lifecycle)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid || rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("deprecated-target update record missing embedded validation results")
	}

	// The updated record is deprecated: the no-updates rule is now
	// self-enforcing — a further update attempt is rejected even though
	// a published 1.4.0 exists in the index.
	updateTestRelease(t, indexDir, id, "1.4.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, updateTestContent(id, "1.4.0"))
	_, _, stderr, err = executeCommand("standard", "update", id, "1.4.0",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "no updates") {
		t.Errorf("second update stderr missing the self-enforcing no-updates rejection: %q", stderr)
	}
	after := installTestReadRecord(t, id)
	if after.Version != "1.3.0" {
		t.Errorf("record version = %s after the rejected second update, want 1.3.0", after.Version)
	}
}

// TestStandardUpdate_TargetRetired verifies that a retired target
// version is not offered for adoption: the update fails with an
// actionable error distinguishing retired from not-found (via the
// orchestration phase A, TS-014-04-03), exit code 1, nothing is
// fetched, and the installed record is unchanged.
//
// Reference: TS-014-03-02 (PM binding decisions 4, 9), TS-014-01-03,
// ADR-027 §3
func TestStandardUpdate_TargetRetired(t *testing.T) {
	const id = "anvil-standard-laravel"
	v2Content := updateTestContent(id, "1.3.0")

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			fetchCount++
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStateRetired, "", keys, v2Content)

	before := installTestReadRecord(t, id)
	_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "not offered for adoption") {
		t.Errorf("stderr missing the retired-not-adoptable message: %q", stderr)
	}
	if fetchCount != 0 {
		t.Errorf("content fetched %d times for a retired target, want 0", fetchCount)
	}
	after := installTestReadRecord(t, id)
	if after.Version != "1.2.3" || !after.InstalledAt.Equal(before.InstalledAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("record changed across the retired-target rejection: before=%+v after=%+v", before, after)
	}
}

// ── Not Found / Exit Codes ───────────────────────────────────────────

// TestStandardUpdate_NotFounds verifies the not-found exit code 3
// contract (TS-P8-07): a standard missing from the index and a target
// version missing from the index both fail with exit code 3, and the
// installed record is unchanged.
//
// Reference: TS-014-03-02 (PM binding decisions 1, 9)
func TestStandardUpdate_NotFounds(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := updateTestContent(id, "1.2.3")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
	before := installTestReadRecord(t, id)

	t.Run("standard not in the index", func(t *testing.T) {
		// The standard is installed (the record exists), but the index
		// pointed at by --index does not contain it: resolution fails
		// with not-found (exit 3). The second index is a valid index
		// holding a different standard.
		index2 := t.TempDir()
		pub2, priv2 := installTestKeypair(t)
		md := installTestRelease(t, "anvil-standard-flutter", "2.0.0", server.URL+"/release.tar.gz",
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub2, priv2)
		installTestIndexEntry(t, index2, md)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.0.0",
			"--index", index2, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !strings.Contains(stderr, "not found") {
			t.Errorf("stderr missing not-found message: %q", stderr)
		}
	})

	t.Run("version not in the index", func(t *testing.T) {
		_, _, stderr, err := executeCommand("standard", "update", id, "9.9.9",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeRuntime)
		if !strings.Contains(stderr, "not found") {
			t.Errorf("stderr missing not-found message: %q", stderr)
		}
	})

	after := installTestReadRecord(t, id)
	if after.Version != before.Version || !after.InstalledAt.Equal(before.InstalledAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("record changed across not-found updates: before=%+v after=%+v", before, after)
	}
}

// ── Adoption Order (pinned) ──────────────────────────────────────────

// TestStandardUpdate_AdoptionOrderPinned pins the documented adoption
// order for updates (TS-014-04; PM binding decision 5): compatibility
// runs BEFORE the content fetch — a compatibility failure must never
// reach the network (zero fetches) — and trust runs before the record
// is written — a trust failure must never change the record.
//
// Reference: TS-014-03-02 (PM binding decision 5), TS-014-04
func TestStandardUpdate_AdoptionOrderPinned(t *testing.T) {
	const id = "anvil-standard-laravel"

	t.Run("compatibility failure aborts before any fetch", func(t *testing.T) {
		v2Content := updateTestContent(id, "1.3.0")

		fetchCount := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/release-v2.tar.gz":
				fetchCount++
				_, _ = w.Write(v2Content)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
		// The target supports only framework 5.1.0; the project declares
		// framework 11.0.0 — not covered (same-major compatibility).
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)
		installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\nframework:\n  laravel:\n    version: 11.0.0\n")

		before := installTestReadRecord(t, id)
		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "not compatible") {
			t.Errorf("stderr missing compatibility rejection: %q", stderr)
		}
		if fetchCount != 0 {
			t.Errorf("content fetched %d time(s) on a compatibility failure — compatibility must gate before the network", fetchCount)
		}
		after := installTestReadRecord(t, id)
		if after.Version != "1.2.3" || !after.InstalledAt.Equal(before.InstalledAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("record changed across the compatibility failure: before=%+v after=%+v", before, after)
		}
	})

	t.Run("trust failure aborts before any record change", func(t *testing.T) {
		v2Content := updateTestContent(id, "1.3.0")
		// The target is attested over v2Content; the server serves
		// DIFFERENT bytes for the target — integrity verification must
		// fail and abort the update. The base install asset stays
		// intact so the fixture install succeeds.
		tamperedServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/release-v2.tar.gz":
				_, _ = w.Write([]byte("tampered content — not what the release claims"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer tamperedServer.Close()

		indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, tamperedServer)
		updateTestRelease(t, indexDir, id, "1.3.0", tamperedServer.URL+"/release-v2.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)

		before := installTestReadRecord(t, id)
		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "trust verification failed") {
			t.Errorf("stderr missing trust rejection: %q", stderr)
		}
		after := installTestReadRecord(t, id)
		if after.Version != "1.2.3" || !after.InstalledAt.Equal(before.InstalledAt) || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("record changed across the trust failure: before=%+v after=%+v", before, after)
		}
		if after.Resolution.Source != tamperedServer.URL+"/release.tar.gz" {
			t.Errorf("record resolution = %q after the trust failure, want the pre-update source", after.Resolution.Source)
		}
	})
}

// ── Content Fetch Policy ─────────────────────────────────────────────

// TestStandardUpdate_FetchFailures verifies that fetch failures abort
// the update with the SAME fetch boundary as the install (reused
// fetchStandardContent): HTTP 404, a timeout, and a redirect to a
// non-https target all fail with actionable errors (exit 1) and the
// installed record — version, timestamps, and resolution — stays
// untouched.
//
// Reference: TS-014-03-02 (PM binding decisions 5, 8), ADR-030 §3
func TestStandardUpdate_FetchFailures(t *testing.T) {
	const id = "anvil-standard-laravel"
	v2Content := updateTestContent(id, "1.3.0")

	t.Run("404", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/release.tar.gz" {
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/missing.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "could not fetch") || !strings.Contains(stderr, "404") {
			t.Errorf("stderr missing the fetch failure with HTTP status: %q", stderr)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Resolution.Source != before.Resolution.Source {
			t.Errorf("record changed across the 404 failure: before=%+v after=%+v", before, after)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/release.tar.gz" {
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
				return
			}
			// Complete the header phase, flush a partial body, then
			// stall: the fetch must fail on the idle-timeout body bound
			// (TD-008) — the failure mode a total per-request deadline
			// would mis-handle, since it would also cut off
			// slow-but-progressing downloads.
			w.Header().Set("Content-Length", strconv.Itoa(len(v2Content)+1024))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(v2Content[:1])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(2 * time.Second)
			_, _ = w.Write(v2Content[1:])
		}))
		defer server.Close()

		indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
		// Shrink the download idle timeout so the test does not wait on
		// the production default stall window (updateTestInstallBase
		// already rebuilt standardInstallHTTPClient with a transport
		// trusting the test server's TLS certificate).
		t.Setenv(EnvDownloadIdleTimeout, "100ms")

		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "timed out") {
			t.Errorf("stderr missing the timeout message: %q", stderr)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("record changed across the timeout failure: before=%+v after=%+v", before, after)
		}
	})

	t.Run("redirect to non-https refused", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/release.tar.gz" {
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
				return
			}
			http.Redirect(w, r, "http://127.0.0.1:1/release.tar.gz", http.StatusFound)
		}))
		defer server.Close()

		indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/redirect",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "TLS only") && !strings.Contains(stderr, "https") {
			t.Errorf("stderr missing the non-https redirect rejection: %q", stderr)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("record changed across the refused redirect: before=%+v after=%+v", before, after)
		}
	})
}

// ── Project Framework Version Handling ───────────────────────────────

// TestStandardUpdate_FrameworkDeclaredUndeterminable verifies that a
// project declaring a framework without a determinable version REJECTS
// the update with an actionable error (never assumed — Transition Plan
// A2; PM binding decision 3), before any fetch, with the record
// unchanged.
//
// Reference: TS-014-03-02 (PM binding decision 3), Transition Plan A2
func TestStandardUpdate_FrameworkDeclaredUndeterminable(t *testing.T) {
	const id = "anvil-standard-laravel"
	v2Content := updateTestContent(id, "1.3.0")

	fetchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			fetchCount++
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)
	installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\n")

	before := installTestReadRecord(t, id)
	_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "framework version cannot be determined") && !strings.Contains(stderr, "cannot be determined") {
		t.Errorf("stderr missing the undeterminable-version rejection: %q", stderr)
	}
	if !strings.Contains(stderr, "framework.laravel.version") {
		t.Errorf("stderr missing the declaration guidance: %q", stderr)
	}
	if fetchCount != 0 {
		t.Errorf("content fetched %d times after the rejection, want 0", fetchCount)
	}
	after := installTestReadRecord(t, id)
	if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("record changed across the rejection: before=%+v after=%+v", before, after)
	}
}

// ── JSON Error Envelope ──────────────────────────────────────────────

// TestStandardUpdate_JSONErrorEnvelope verifies that a failing update
// with --json produces the TS-P8-05 error envelope on stdout (the
// machine-readable error surface; the human-readable path carries the
// exit code).
//
// Reference: TS-014-03-02, TS-P8-05
func TestStandardUpdate_JSONErrorEnvelope(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := updateTestContent(id, "1.2.3")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)

	_, stdout, _, _ := executeCommand("standard", "update", id, "9.9.9",
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

// ── Fetch Boundary (update surface) ──────────────────────────────────

// TestStandardUpdate_FetchBoundary pins the fetch boundary at the UPDATE
// surface (reviewer F4): the update reuses the install's fetch policy
// verbatim (fetchStandardContent) — a distribution location carrying
// userinfo is rejected (before any fetch when declared; after an
// allowed redirect when smuggled in), the size cap is enforced during
// the download, and in every rejection the credentials are NEVER echoed
// in the error output (reviewer F1 — the "credentials never sent,
// echoed, or persisted" contract). The installed record stays unchanged
// on every boundary rejection.
//
// Reference: TS-014-03-02 (fix round 1), TS-014-03-01 (security finding
// 1), ADR-030 §3
func TestStandardUpdate_FetchBoundary(t *testing.T) {
	const id = "anvil-standard-laravel"

	t.Run("userinfo in the declared location is rejected before any fetch", func(t *testing.T) {
		v2Content := updateTestContent(id, "1.3.0")
		pub, priv := installTestKeypair(t)
		keys := updateTestKeypair{pub: pub, priv: priv}

		fetchCount := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fetchCount++
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		}))
		defer server.Close()

		indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
		// A userinfo location fails the strict parse (the authoritative
		// validation point, same as install) — the update never reaches
		// the network.
		updateTestRelease(t, indexDir, id, "1.3.0", "https://alice:secret@example.com/release.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)
		fetchesBeforeUpdate := fetchCount

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "userinfo") {
			t.Errorf("stderr missing the userinfo rejection: %q", stderr)
		}
		for _, credential := range []string{"alice", "secret"} {
			if strings.Contains(stderr, credential) {
				t.Errorf("stderr echoes the credential %q: %q — credentials must never appear in errors", credential, stderr)
			}
		}
		if fetchCount != fetchesBeforeUpdate {
			t.Errorf("content fetched %d time(s) by the update with a userinfo location, want 0", fetchCount-fetchesBeforeUpdate)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("record changed across the userinfo rejection: before=%+v after=%+v", before, after)
		}
	})

	t.Run("redirect to a userinfo target is refused with redacted output", func(t *testing.T) {
		v2Content := updateTestContent(id, "1.3.0")
		pub, priv := installTestKeypair(t)
		keys := updateTestKeypair{pub: pub, priv: priv}

		redirectCount := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/redirect":
				redirectCount++
				// Smuggle credentials into the redirect target: the
				// post-redirect userinfo check must refuse the final URL
				// and must not echo the credentials.
				http.Redirect(w, r, "https://alice:secret@"+r.Host+"/final.tar.gz", http.StatusFound)
			case "/final.tar.gz":
				_, _ = w.Write(v2Content)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/redirect",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "userinfo") {
			t.Errorf("stderr missing the post-redirect userinfo rejection: %q", stderr)
		}
		for _, credential := range []string{"alice", "secret"} {
			if strings.Contains(stderr, credential) {
				t.Errorf("stderr echoes the credential %q: %q — credentials must never appear in errors", credential, stderr)
			}
		}
		if redirectCount != 1 {
			t.Errorf("redirect requested %d time(s), want exactly 1", redirectCount)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Resolution.Source != before.Resolution.Source {
			t.Errorf("record changed across the userinfo redirect rejection: before=%+v after=%+v", before, after)
		}
	})

	t.Run("size cap enforced during the download", func(t *testing.T) {
		// The cap is a package-level variable: shrink it BEFORE the base
		// install (the base content is small and must still pass) so the
		// update target — an oversized 4 KiB asset — is the one rejected.
		origCap := standardContentMaxBytes
		standardContentMaxBytes = 1024
		t.Cleanup(func() { standardContentMaxBytes = origCap })

		v2Content := make([]byte, 4096)
		pub, priv := installTestKeypair(t)
		keys := updateTestKeypair{pub: pub, priv: priv}

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/release-v2.tar.gz":
				_, _ = w.Write(v2Content)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "size cap") {
			t.Errorf("stderr missing the size-cap rejection: %q", stderr)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Resolution.Source != before.Resolution.Source {
			t.Errorf("record changed across the size-cap rejection: before=%+v after=%+v", before, after)
		}
	})

	t.Run("network failure on a redirect to a userinfo target leaks no credentials", func(t *testing.T) {
		// QA F-1: when a redirect to a userinfo-bearing target fails at
		// the network layer, Go's url.Error masks the password but leaks
		// the username. The fetch error must be scrubbed so NEITHER the
		// username nor the password appears — the redacted URL form is
		// surfaced instead, and the record stays unchanged.
		v2Content := updateTestContent(id, "1.3.0")
		pub, priv := installTestKeypair(t)
		keys := updateTestKeypair{pub: pub, priv: priv}

		redirectCount := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/redirect":
				redirectCount++
				// Port 9 (discard) on 127.0.0.1 refuses the connection:
				// the redirected request fails at the network layer, and
				// the returned url.Error renders the userinfo-bearing
				// target URL.
				http.Redirect(w, r, "https://alice:secret@127.0.0.1:9/final.tar.gz", http.StatusFound)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/redirect",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "could not be reached") {
			t.Errorf("stderr missing the network-failure message: %q", stderr)
		}
		// The scrubbed url.Error rendering surfaces the redacted URL form.
		if !strings.Contains(stderr, "https://127.0.0.1:9/final.tar.gz") {
			t.Errorf("stderr missing the redacted URL form: %q", stderr)
		}
		for _, fragment := range []string{"alice", "secret", "user", "pass", "@"} {
			if strings.Contains(stderr, fragment) {
				t.Errorf("stderr leaks the credential fragment %q: %q — credentials must never appear in errors", fragment, stderr)
			}
		}
		if redirectCount != 1 {
			t.Errorf("redirect requested %d time(s), want exactly 1", redirectCount)
		}
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Resolution.Source != before.Resolution.Source {
			t.Errorf("record changed across the network-failure rejection: before=%+v after=%+v", before, after)
		}
	})

	t.Run("redirect to a non-https userinfo target is refused with redacted output", func(t *testing.T) {
		// QA F-2: the CheckRedirect refusal (non-https target) used to
		// render the redirect target via req.URL.String(), echoing FULL
		// credentials for http://user:pass@... targets in the refusal
		// message. The refusal must render the target without
		// credentials; the record stays unchanged.
		v2Content := updateTestContent(id, "1.3.0")
		pub, priv := installTestKeypair(t)
		keys := updateTestKeypair{pub: pub, priv: priv}

		redirectCount := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/release.tar.gz":
				_, _ = w.Write(updateTestContent(id, "1.2.3"))
			case "/redirect":
				redirectCount++
				// Non-https + userinfo: the redirect policy refuses the
				// target outright — the refusal message must not echo
				// the credentials.
				http.Redirect(w, r, "http://alice:secret@127.0.0.1:9/final.tar.gz", http.StatusFound)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
		updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/redirect",
			registry.LifecycleStatePublished, "", keys, v2Content)
		before := installTestReadRecord(t, id)

		_, _, stderr, err := executeCommand("standard", "update", id, "1.3.0",
			"--index", indexDir, "--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "could not be reached") {
			t.Errorf("stderr missing the network-failure message: %q", stderr)
		}
		if !strings.Contains(stderr, "TLS only") && !strings.Contains(stderr, "https") {
			t.Errorf("stderr missing the non-https refusal: %q", stderr)
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
		after := installTestReadRecord(t, id)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.Resolution.Source != before.Resolution.Source {
			t.Errorf("record changed across the non-https userinfo refusal: before=%+v after=%+v", before, after)
		}
	})
}

// ── Downgrade ────────────────────────────────────────────────────────

// TestStandardUpdate_Downgrade pins the downgrade semantics (reviewer
// F5; PM binding decision 1): the target version is explicit and an
// OLDER version is a valid explicit adoption event — the downgrade is
// fully re-validated (compatibility + trust run against the older
// target), records the older pinned version with its new resolution,
// preserves installedAt, refreshes updatedAt, and never triggers an
// automatic rollback.
//
// Reference: TS-014-03-02 (fix round 1), ADR-022 §3
func TestStandardUpdate_Downgrade(t *testing.T) {
	const id = "anvil-standard-laravel"
	content124 := updateTestContent(id, "1.2.4")
	content123 := updateTestContent(id, "1.2.3")
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(content124)
		case "/release-123.tar.gz":
			_, _ = w.Write(content123)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	updateTestRelease(t, indexDir, id, "1.2.4", server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", keys, content124)
	updateTestRelease(t, indexDir, id, "1.2.3", server.URL+"/release-123.tar.gz",
		registry.LifecycleStatePublished, "", keys, content123)

	if _, _, stderr, err := executeCommand("standard", "install", id, "1.2.4",
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("fixture install of 1.2.4 failed: %v (stderr: %q)", err, stderr)
	}
	first := installTestReadRecord(t, id)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "1.2.3",
		"--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("downgrade update failed, want success: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Updated standard: "+id+" 1.2.3") {
		t.Errorf("stdout missing the downgrade success line:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Version != "1.2.3" {
		t.Errorf("record version = %s after the downgrade, want the older pinned version 1.2.3", rec.Version)
	}
	if rec.Resolution.Source != server.URL+"/release-123.tar.gz" {
		t.Errorf("record resolution = %q, want the older target's endpoint", rec.Resolution.Source)
	}
	if !rec.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installedAt changed across the downgrade: %v → %v — the original install time must be preserved", first.InstalledAt, rec.InstalledAt)
	}
	if !rec.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("updatedAt not refreshed across the downgrade: %v → %v", first.UpdatedAt, rec.UpdatedAt)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid || rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("downgrade record missing embedded validation results: compat=%+v trust=%+v", rec.Compatibility, rec.Trust)
	}
}

// ── Latest-Published Resolution (ST-021-05) ──────────────────────────

// TestStandardUpdate_LatestResolution verifies that "anvil standard
// update <id>" (version omitted) resolves the latest published release
// from the index and updates to it: the target is pinned exactly like an
// explicit version, re-validated (compatibility + trust), recorded with
// the new resolution, and the human report annotates the automated
// choice.
//
// Reference: ST-021-05, TS-014-03-02, ADR-022 §3
func TestStandardUpdate_LatestResolution(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.4.0"
	)
	v2Content := updateTestContent(id, "1.4.0")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	// Two newer published releases; the latest (1.4.0) must win.
	updateTestRelease(t, indexDir, id, "1.3.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)
	updateTestRelease(t, indexDir, id, version, server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	first := installTestReadRecord(t, id)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("version-less update failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Updated standard: "+id+" "+version+" (latest published release)") {
		t.Errorf("stdout should report the resolved latest release, got:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Version != version {
		t.Errorf("record version = %s, want the latest %s", rec.Version, version)
	}
	if rec.Resolution.Source != server.URL+"/release-v2.tar.gz" {
		t.Errorf("record resolution = %q, want the latest target's endpoint", rec.Resolution.Source)
	}
	if !rec.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installedAt changed across the version-less update: %v → %v", first.InstalledAt, rec.InstalledAt)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid || rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("version-less update record missing embedded validation results: compat=%+v trust=%+v", rec.Compatibility, rec.Trust)
	}
}

// TestStandardUpdate_LatestResolution_AlreadyAtVersion verifies the
// idempotent path of a version-less update: when the installed version
// IS the latest published release, the update still runs the full
// validation and reports "already at version (re-validated)".
//
// Reference: ST-021-05, TS-014-03-02 (PM binding decision 7)
func TestStandardUpdate_LatestResolution_AlreadyAtVersion(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := updateTestContent(id, "1.2.3")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("version-less update at the already-latest version failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "already at version") || !strings.Contains(stdout, "(re-validated)") {
		t.Errorf("stdout missing the already-at-version report:\n%s", stdout)
	}
}

// TestStandardUpdate_LatestResolution_RetiredExcluded verifies that a
// retired release is never the version-less update target: with a
// retired 1.4.0 and a published 1.3.0, the update resolves 1.3.0.
//
// Reference: ST-021-05, ADR-027 §3
func TestStandardUpdate_LatestResolution_RetiredExcluded(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.3.0"
	)
	v2Content := updateTestContent(id, version)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(updateTestContent(id, "1.2.3"))
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	indexDir, anchorsFile, _, keys, _ := updateTestInstallBase(t, server)
	updateTestRelease(t, indexDir, id, "1.4.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStateRetired, "", keys, v2Content)
	updateTestRelease(t, indexDir, id, version, server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("version-less update failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Updated standard: "+id+" "+version+" (latest published release)") {
		t.Errorf("stdout should skip the retired 1.4.0 and resolve 1.3.0, got:\n%s", stdout)
	}
	rec := installTestReadRecord(t, id)
	if rec.Version != version {
		t.Errorf("record version = %s, want 1.3.0 (retired 1.4.0 excluded)", rec.Version)
	}
}

// ── Missing-Index Degradation (ST-021-05) ────────────────────────────

// TestStandardUpdate_LatestResolution_DowngradeWarning verifies the
// latest-resolution downgrade guard (reviewer M1, ST-021-05): when the
// installed version is NEWER than every resolvable release — the newer
// installed releases were retired in the index, so the newest resolvable
// one is older — a version-less update resolves the older target and
// proceeds (a downgrade is a documented adoption event) BUT the report
// carries a prominent warning naming both versions and the explicit-pin
// alternative. The choice is never silent.
//
// Reference: ST-021-05, TS-014-03-02, ADR-027 §3
func TestStandardUpdate_LatestResolution_DowngradeWarning(t *testing.T) {
	const (
		id         = "anvil-standard-laravel"
		installed  = "1.4.0"
		resolvable = "1.3.0"
	)
	content140 := updateTestContent(id, installed)
	v2Content := updateTestContent(id, resolvable)
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release.tar.gz":
			_, _ = w.Write(content140)
		case "/release-v2.tar.gz":
			_, _ = w.Write(v2Content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	installTestEnv(t, server)
	indexDir := t.TempDir()
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	// Initial index: 1.4.0 and 1.5.0 published.
	updateTestRelease(t, indexDir, id, installed, server.URL+"/release.tar.gz",
		registry.LifecycleStatePublished, "", keys, content140)
	updateTestRelease(t, indexDir, id, "1.5.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	if _, _, stderr, err := executeCommand("standard", "install", id, installed,
		"--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("fixture install of %s failed: %v (stderr: %q)", installed, err, stderr)
	}

	// The index moves on: 1.5.0 and 1.4.0 are retired; only 1.3.0
	// remains published — the newest resolvable release is now OLDER
	// than the installed 1.4.0.
	updateTestRelease(t, indexDir, id, "1.5.0", server.URL+"/release-v2.tar.gz",
		registry.LifecycleStateRetired, "", keys, v2Content)
	updateTestRelease(t, indexDir, id, installed, server.URL+"/release.tar.gz",
		registry.LifecycleStateRetired, "", keys, content140)
	updateTestRelease(t, indexDir, id, resolvable, server.URL+"/release-v2.tar.gz",
		registry.LifecycleStatePublished, "", keys, v2Content)

	_, stdout, stderr, err := executeCommand("standard", "update", id, "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("version-less update failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Updated standard: "+id+" "+resolvable+" (latest published release)") {
		t.Errorf("stdout should resolve the newest resolvable release %s, got:\n%s", resolvable, stdout)
	}
	if !strings.Contains(stdout, "Warning:") ||
		!strings.Contains(stdout, "older than the installed version "+installed) ||
		!strings.Contains(stdout, "downgrade") {
		t.Errorf("stdout must carry the prominent downgrade warning, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "pin it explicitly") || !strings.Contains(stdout, "anvil standard update "+id) {
		t.Errorf("stdout should name the explicit-pin alternative, got:\n%s", stdout)
	}

	rec := installTestReadRecord(t, id)
	if rec.Version != resolvable {
		t.Errorf("record version = %s, want the resolved older target %s", rec.Version, resolvable)
	}
}

// TestStandardUpdate_MissingIndex_ConcreteHint verifies that a missing
// index hard-fails the update — resolution requires the index — with the
// concrete first-run hint naming the --index flag, the
// ANVIL_REGISTRY_INDEX environment variable, and the absence of a
// bundled index (ADR-030).
//
// Reference: ST-021-05, ADR-030
func TestStandardUpdate_MissingIndex_ConcreteHint(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := updateTestContent(id, "1.2.3")
	pub, priv := installTestKeypair(t)
	keys := updateTestKeypair{pub: pub, priv: priv}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	indexDir, anchorsFile, _, _, _ := updateTestInstallBase(t, server)
	_ = indexDir
	_ = keys // the base fixture already installed 1.2.3

	missing := t.TempDir() + "/does-not-exist"
	_, _, stderr, err := executeCommand("standard", "update", id, "1.2.3", "--index", missing, "--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeRuntime)
	if !strings.Contains(stderr, "registry index not found") {
		t.Errorf("stderr should report the missing index: %q", stderr)
	}
	for _, want := range []string{"--index", envStandardIndex, "no bundled index"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr should carry the concrete first-run hint (%q), got: %q", want, stderr)
		}
	}
}

// TestStandardUpdate_Args_OptionalVersion verifies the argument validator
// accepts a version-less update and rejects an argument-less one with the
// range explained.
//
// Reference: ST-021-05
func TestStandardUpdate_Args_OptionalVersion(t *testing.T) {
	_, _, stderr, err := executeCommand("standard", "update")
	if err == nil {
		t.Fatal("update with no arguments must fail the argument validator")
	}
	if !strings.Contains(stderr, "requires between 1 and 2 argument(s)") {
		t.Errorf("stderr should explain the argument range, got: %q", stderr)
	}
}
