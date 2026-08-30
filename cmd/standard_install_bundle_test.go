// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil standard install-bundle" (TS-014-05-02): the
// offline/bundled installation flow — adoption sequence (OpenBundle:
// structure → checksum → strict parse of the bundled metadata → lifecycle
// gate → compatibility → Bundle.Verify → record), the no-network
// guarantee, failure attribution (bundle corrupt vs metadata invalid vs
// verification mismatch), idempotency by identity plus version, version
// conflict, deprecation warnings, retired rejection, fail-closed anchors,
// and the exit code / --json conventions (identical to the online
// install).
//
// Every test is self-contained: the bundle archive, the trust anchors
// file, and the global config directory (XDG_CONFIG_HOME — record store)
// live in t.TempDir(); the bundled metadata's distribution.location
// always points at a dead port — the offline flow never fetches from it.
// Bundles are produced with registry.CreateBundle (the same packer the
// registry tests use), and the metadata is built with the attested
// release fixture shared with the online install tests.
//
// Reference: TS-014-05-02, TS-014-05-01, ADR-022 §3, ADR-023 §3,
// ADR-026, ADR-027 §3
package cmd

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Test Fixtures ────────────────────────────────────────────────────

// offlineInstallDeadLocation is the distribution location declared in
// bundled test metadata: a well-formed https URL at a dead port. The
// offline flow never fetches from it (the bundled content IS the release
// content), so a test completing successfully proves no network access —
// any fetch attempt would fail with a connection error.
const offlineInstallDeadLocation = "https://127.0.0.1:1/release.tar.gz"

// installBundleTestBundle packs the release content and its metadata
// document into a valid bundle archive (registry.CreateBundle — the
// production packer) and returns the archive bytes.
func installBundleTestBundle(t *testing.T, md registry.Metadata, content []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal bundle metadata: %v", err)
	}
	data, err := registry.CreateBundle(content, raw)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	return data
}

// installBundleTestFile writes the bundle archive to disk and returns
// its path.
func installBundleTestFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "release.bundle.tar.gz")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write bundle %s: %v", path, err)
	}
	return path
}

// installBundleTestRelease builds a fully attested bundled release: the
// metadata (built by the shared install fixture over content) with a
// distribution location that is never fetched, packed into a bundle.
// Returns the bundle path, the metadata, and the content.
func installBundleTestRelease(t *testing.T, id, version, lifecycleState, removalDate string, capability []string, content []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey) (string, registry.Metadata) {
	t.Helper()
	md := installTestRelease(t, id, version, offlineInstallDeadLocation,
		lifecycleState, removalDate, capability, content, pub, priv)
	return installBundleTestFile(t, installBundleTestBundle(t, md, content)), md
}

// bundleWithModifiedByte rebuilds a bundle archive whose uncompressed
// tar stream differs from the original at the given byte offset, while
// keeping the ORIGINAL bundle.sha256 value. The stream still decodes
// cleanly (so the tar layout and the gzip stream are valid), but the
// recomputed bundle checksum no longer matches the declared value — the
// deterministic "bundle corrupt / modified after creation" (integrity)
// fixture. The bundle is recompressed as a single fresh gzip member, so
// the pinned single-member format still holds.
func bundleWithModifiedByte(t *testing.T, data []byte, offset int, b byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decompress bundle: %v", err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read uncompressed stream: %v", err)
	}
	if offset >= len(raw) {
		t.Fatalf("modification offset %d out of range (stream %d bytes)", offset, len(raw))
	}
	raw[offset] = b

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatalf("recompress bundle: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close recompressed bundle: %v", err)
	}
	return buf.Bytes()
}

// failingRoundTripper fails every HTTP request: the no-network test
// proof. If any code path of the offline flow constructed or used an
// HTTP client, the install would fail instead of completing.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network access attempted during offline install")
}

// poisonStandardInstallHTTPClient replaces the install fetch client with
// one that fails every request, so any network access attempt during an
// offline install fails the install.
func poisonStandardInstallHTTPClient(t *testing.T) {
	t.Helper()
	orig := standardInstallHTTPClient
	standardInstallHTTPClient = &http.Client{
		Timeout:   100 * time.Millisecond,
		Transport: failingRoundTripper{},
	}
	t.Cleanup(func() { standardInstallHTTPClient = orig })
}

// ── Command Group Registration ───────────────────────────────────────

// TestStandardInstallBundleCommand_Registered verifies that the
// install-bundle command is registered in the standard group and listed
// by the group help: offline installation is available only through this
// explicit command surface.
//
// Reference: TS-014-05-02 (PM binding decision 1), ADR-023 §3
func TestStandardInstallBundleCommand_Registered(t *testing.T) {
	_, _, err := rootCmd.Find([]string{"standard", "install-bundle"})
	if err != nil {
		t.Fatalf("standard install-bundle command not found: %v", err)
	}
	_, helpOut, _, err := executeCommand("standard", "--help")
	if err != nil {
		t.Fatalf("standard --help failed: %v", err)
	}
	if !strings.Contains(helpOut, "install-bundle") {
		t.Errorf("standard group help does not list the install-bundle subcommand:\n%s", helpOut)
	}
	if !strings.Contains(helpOut, "offline") {
		t.Errorf("standard group help does not describe install-bundle as offline:\n%s", helpOut)
	}
}

// ── Success Path ─────────────────────────────────────────────────────

// TestStandardInstallBundle_Success installs a valid published bundle end
// to end, with no network reachable at any point: the record is persisted
// with the pinned version, contract version, the explicit bundle
// resolution (kind "bundle", source = the bundle path), lifecycle state,
// and the embedded compatibility and trust results; the command succeeds
// (exit 0).
//
// Reference: TS-014-05-02 (DoD: a complete bundle installs without
// network access; installed version and resolution are recorded)
func TestStandardInstallBundle_Success(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	// Every network path is poisoned: any fetch attempt fails the
	// install. The bundled metadata declares a distribution location at
	// a dead port as well — the offline flow must complete without it.
	poisonStandardInstallHTTPClient(t)

	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	cmd, stdout, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("install-bundle failed: %v (stderr: %q)", err, stderr)
	}
	if cmd == nil {
		t.Fatal("executeCommand returned a nil command")
	}
	if !strings.Contains(stdout, "Installed standard: "+id+" "+version) {
		t.Errorf("stdout missing success line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "bundle") || !strings.Contains(stdout, bundlePath) {
		t.Errorf("stdout missing the bundle resolution details:\n%s", stdout)
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
	if rec.Resolution.Kind != registry.ResolutionKindBundle || rec.Resolution.Source != bundlePath {
		t.Errorf("record resolution = %+v, want kind bundle with the bundle path as source", rec.Resolution)
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
	// The fixture runs outside any project: no framework declared, so
	// the capability validation ran shape-only and that fact is
	// recorded — the same as the online install.
	if rec.Compatibility.FrameworkVersionChecked {
		t.Errorf("record compatibility frameworkVersionChecked = true, want false (no project → shape-only)")
	}
	if !rec.InstalledAt.Equal(rec.UpdatedAt) {
		t.Errorf("fresh record installedAt %v != updatedAt %v", rec.InstalledAt, rec.UpdatedAt)
	}
}

// TestStandardInstallBundle_SuccessJSON verifies the --json envelope
// shape (TS-P8-05): success envelope with the install data — identity,
// pinned version, the bundle resolution (kind + source), lifecycle,
// timestamps, and the embedded validation results.
//
// Reference: TS-014-05-02 (PM binding decision 1), TS-P8-05
func TestStandardInstallBundle_SuccessJSON(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, stdout, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile, "--json")
	if err != nil {
		t.Fatalf("install-bundle failed: %v (stderr: %q)", err, stderr)
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
	var resolution struct {
		Kind   string `json:"kind"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(envelope.Data.Resolution, &resolution); err != nil {
		t.Fatalf("data.resolution is not decodable: %v", err)
	}
	if resolution.Kind != registry.ResolutionKindBundle || resolution.Source != bundlePath {
		t.Errorf("data.resolution = %+v, want kind bundle with the bundle path as source", resolution)
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
	}{{"lifecycle", envelope.Data.Lifecycle}, {"compatibility", envelope.Data.Compatibility}, {"trust", envelope.Data.Trust}} {
		if len(raw.data) == 0 || string(raw.data) == "null" {
			t.Errorf("data.%s is missing", raw.name)
		}
	}
	if !strings.Contains(envelope.Data.RecordPath, id+".json") {
		t.Errorf("record_path = %q, want the record file for %s", envelope.Data.RecordPath, id)
	}
}

// TestStandardInstallBundle_NoNetwork pins the no-network guarantee (PM
// binding decision 2): the offline flow constructs no HTTP client and
// never fetches — the bundle file, the anchors file, the compatibility
// matrix, the project config, and the record store are its only inputs.
// The test poisons every network path (the shared install HTTP client is
// replaced with one that fails every request, and the bundled metadata
// declares a distribution location at a dead port) and asserts the
// install still completes and records the local anchor path — the
// recorded TrustResult.AnchorPath proves the anchors came from the local
// operator file, never from the network or the bundle.
//
// Reference: TS-014-05-02 (DoD: a complete bundle installs without
// network access; PM binding decision 2)
func TestStandardInstallBundle_NoNetwork(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	poisonStandardInstallHTTPClient(t)

	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("offline install performed network access or failed: %v (stderr: %q)", err, stderr)
	}

	rec := installTestReadRecord(t, id)
	if rec.Resolution.Kind != registry.ResolutionKindBundle {
		t.Errorf("record resolution kind = %q, want bundle", rec.Resolution.Kind)
	}
	if rec.Trust == nil || !rec.Trust.Valid {
		t.Fatalf("record trust = %+v, want a valid embedded result", rec.Trust)
	}
	if rec.Trust.AnchorPath != anchorsFile {
		t.Errorf("record trust anchorPath = %q, want the local operator anchors file %q", rec.Trust.AnchorPath, anchorsFile)
	}
}

// ── Verification Material / Attribution ──────────────────────────────

// TestStandardInstallBundle_MissingVerificationMaterial verifies that a
// bundle whose metadata document carries no verification material fails
// the install at open (the strict parse requires trust.contentDigests,
// trust.attestation.signature, and trust.attestation.publicKey — ADR-022
// §3) with the metadata failure class attributed distinctly ("bundled
// metadata document invalid"), an actionable resolution, and nothing
// recorded.
//
// Reference: TS-014-05-02 (DoD: missing or invalid verification material
// fails the install with an actionable error; PM binding decision 4)
func TestStandardInstallBundle_MissingVerificationMaterial(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, _ := installTestKeypair(t)

	installTestEnv(t, nil)
	// The metadata document deliberately omits the trust section: no
	// digests, no attestation — no verification material.
	md := registry.Metadata{
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Capability: registry.Capability{
			FrameworkVersion: []string{"5.1.0"},
		},
		Distribution: registry.Distribution{
			Type:     registry.DistributionTypeGitHubReleases,
			Location: offlineInstallDeadLocation,
		},
		Lifecycle: registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	bundlePath := installBundleTestFile(t, installBundleTestBundle(t, md, content))
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "bundled metadata document") || !strings.Contains(stderr, "invalid") {
		t.Errorf("stderr missing the metadata-invalid attribution: %q", stderr)
	}
	if !strings.Contains(stderr, "verification material") && !strings.Contains(stderr, "trust") {
		t.Errorf("stderr missing the verification-material guidance: %q", stderr)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record exists after a missing-verification-material rejection, want nothing recorded")
	}
}

// TestStandardInstallBundle_TamperedAttribution verifies the failure
// attribution contract: a byte-corrupt bundle is rejected at OPEN with
// the corrupt-family message (bundle checksum / archive integrity), a
// structurally invalid archive is rejected as "not a valid bundle
// archive", while a bundle whose content does not match its declared
// digests (internally consistent, checksum valid) reaches VERIFY and
// fails with the distinct "content digest mismatch" — never confused
// with a corrupt bundle.
//
// Reference: TS-014-05-02 (PM binding decisions 3, 4; TS-014-05-01
// security notes)
func TestStandardInstallBundle_TamperedAttribution(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	t.Run("byte-corrupt bundle rejected at open as corrupt", func(t *testing.T) {
		// A valid bundle whose uncompressed stream is modified while the
		// declared bundle checksum stays original: the archive decodes,
		// but the recomputed bundle checksum mismatches — the integrity
		// failure class ("bundle corrupt or modified after creation").
		md := installTestRelease(t, id, version, offlineInstallDeadLocation,
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
		bundlePath := installBundleTestFile(t, bundleWithModifiedByte(t,
			installBundleTestBundle(t, md, content), 512, 'X'))

		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "corrupt") {
			t.Errorf("stderr missing the corrupt-bundle attribution: %q", stderr)
		}
		if !strings.Contains(stderr, "fresh copy") {
			t.Errorf("stderr missing the fresh-copy resolution: %q", stderr)
		}
		if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
			t.Errorf("record exists after a corrupt-bundle rejection, want nothing recorded")
		}
	})

	t.Run("non-archive input rejected as not a valid bundle", func(t *testing.T) {
		bundlePath := installBundleTestFile(t, []byte("this is not a bundle archive at all"))
		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "not a valid bundle archive") {
			t.Errorf("stderr missing the not-a-bundle attribution: %q", stderr)
		}
	})

	t.Run("content digest mismatch rejected at verify, not as corrupt", func(t *testing.T) {
		// The metadata is attested over contentA; the bundle carries
		// contentB. The bundle checksum and the metadata parse are both
		// valid — only the content digest verification fails, with the
		// distinct "content digest mismatch" reason and the
		// offline-appropriate guidance (the shared engine's "re-fetch
		// from the distribution location" advice is misleading offline —
		// the bundled content IS the content).
		contentA := []byte("content the release claims")
		contentB := []byte("different content actually carried in the bundle")
		md := installTestRelease(t, id, version, offlineInstallDeadLocation,
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, contentA, pub, priv)
		bundlePath := installBundleTestFile(t, installBundleTestBundle(t, md, contentB))

		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "trust verification failed") {
			t.Errorf("stderr missing the verify-failure surface: %q", stderr)
		}
		if !strings.Contains(stderr, "content digest mismatch") {
			t.Errorf("stderr missing the digest-mismatch attribution: %q", stderr)
		}
		if !strings.Contains(stderr, "obtain a fresh copy of the bundle from the publisher") {
			t.Errorf("stderr missing the offline digest-mismatch guidance: %q", stderr)
		}
		if strings.Contains(stderr, "corrupt") {
			t.Errorf("stderr conflates a digest mismatch with a corrupt bundle: %q", stderr)
		}
		if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
			t.Errorf("record exists after a digest-mismatch rejection, want nothing recorded")
		}
	})

	t.Run("attestation mismatch rejected at verify", func(t *testing.T) {
		// The metadata declares the anchored public key (pub) but is
		// signed by a different key: the anchor matches, the attestation
		// does not — the attestation failure is attributed separately.
		_, otherPriv := installTestKeypair(t)
		md := installTestRelease(t, id, version, offlineInstallDeadLocation,
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, otherPriv.Public().(ed25519.PublicKey), otherPriv)
		md.Trust.Attestation.PublicKey = base64.StdEncoding.EncodeToString(pub)
		bundlePath := installBundleTestFile(t, installBundleTestBundle(t, md, content))

		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "attestation") || !strings.Contains(stderr, "signature") {
			t.Errorf("stderr missing the attestation-mismatch attribution: %q", stderr)
		}
	})
}

// TestStandardInstallBundle_AnchorMismatch verifies the fail-closed
// anchor behavior (PM decision D-07): a bundle verified against an
// allowlist anchoring a DIFFERENT key fails at verify with the distinct
// anchor-mismatch attribution — the release was not signed by the
// trusted publisher.
//
// Reference: TS-014-05-02, TS-014-04-02, ADR-022 §3
func TestStandardInstallBundle_AnchorMismatch(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)
	otherPub, _ := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	// The allowlist anchors a different key than the release declares.
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, otherPub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "public key mismatch") {
		t.Errorf("stderr missing the anchor-mismatch attribution: %q", stderr)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record exists after an anchor mismatch, want nothing recorded")
	}
}

// ── Lifecycle ────────────────────────────────────────────────────────

// TestStandardInstallBundle_RetiredRejected verifies that a retired
// bundled release is not installable: the lifecycle gate on the bundled
// metadata produces the actionable "not offered" rejection, and nothing
// is recorded — identical to the online install.
//
// Reference: TS-014-05-02 (PM binding decision 1), TS-014-01-03,
// ADR-027 §3
func TestStandardInstallBundle_RetiredRejected(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStateRetired, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "not offered for adoption") {
		t.Errorf("stderr missing the retired-not-adoptable message: %q", stderr)
	}
	if !strings.Contains(stderr, "retired") {
		t.Errorf("stderr missing the retired attribution: %q", stderr)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record exists for a retired release, want nothing recorded")
	}
}

// TestStandardInstallBundle_DeprecatedWarning verifies that a deprecated
// bundled release installs WITH a warning: the warning states the
// deprecation, the removal date, and the no-updates note; the installed
// record keeps the deprecated lifecycle state — identical to the online
// install.
//
// Reference: TS-014-05-02 (PM binding decision 1), TS-014-01-03,
// ADR-023 §3, ADR-027 §3
func TestStandardInstallBundle_DeprecatedWarning(t *testing.T) {
	const (
		id      = "anvil-standard-flutter"
		version = "2.0.0"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStateDeprecated, "2027-01-01T00:00:00Z", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, stdout, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("deprecated install-bundle failed, want success with warning: %v (stderr: %q)", err, stderr)
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

// ── Compatibility ────────────────────────────────────────────────────

// TestStandardInstallBundle_CompatibilityRejected verifies that a bundle
// whose metadata declares an unsupported contract version is rejected by
// the compatibility validation on the bundled metadata (the same
// ValidateCompatibility used online), with nothing recorded.
//
// Reference: TS-014-05-02 (PM binding decision 3: the offline validation
// path is equivalent to the online path)
func TestStandardInstallBundle_CompatibilityRejected(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	md := installTestRelease(t, id, version, offlineInstallDeadLocation,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	// The corpus matrix supports only contract major 1 (ADR-024 §3.1):
	// declaring major 2 is incompatible.
	md.ContractVersion = "2.0.0"
	bundlePath := installBundleTestFile(t, installBundleTestBundle(t, md, content))
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "not compatible") {
		t.Errorf("stderr missing the compatibility rejection: %q", stderr)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record exists after a compatibility rejection, want nothing recorded")
	}
}

// ── Idempotency / Version Conflict ───────────────────────────────────

// TestStandardInstallBundle_IdempotentReinstall verifies that
// re-installing the same identity and version from a bundle is
// idempotent (ADR-023 §3): the full validation still runs (re-validated
// from the bundle), the record's validation results are refreshed via
// Update (installedAt preserved, updatedAt re-stamped), the command
// reports "already installed (re-validated)", no duplicate record is
// created — identical to the online install semantics — and the
// recorded resolution source follows the LAST adoption: re-installing
// from a different bundle path updates the record to that path.
//
// Reference: TS-014-05-02 (PM binding decision 5; DoD: offline
// installation is idempotent by identity plus version; product gap 3:
// last adoption wins)
func TestStandardInstallBundle_IdempotentReinstall(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	if _, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("first install-bundle failed: %v (stderr: %q)", err, stderr)
	}
	first := installTestReadRecord(t, id)

	// Re-install from a SECOND copy of the same bundle at a different
	// path: the idempotent re-validation must refresh the recorded
	// resolution source to the current bundle path (last adoption wins).
	bundlePath2 := installBundleTestFile(t, mustReadFile(t, bundlePath))
	_, stdout, stderr, err := executeCommand("standard", "install-bundle", bundlePath2,
		"--trust-anchors", anchorsFile)
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
	if second.Resolution.Source != bundlePath2 {
		t.Errorf("record resolution source = %q after re-install from %q, want the last-adoption path %q (last adoption wins)", second.Resolution.Source, bundlePath2, bundlePath2)
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

// mustReadFile reads a file for test fixtures, failing the test on
// error.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestStandardInstallBundle_VersionConflict verifies that installing a
// different version from a bundle over an installed standard is rejected
// with an actionable error referencing the update flow (TS-014-03-02):
// a version change is an update, an explicit adoption event — there is
// no offline update.
//
// Reference: TS-014-05-02 (PM binding decision 5), TS-014-03-03
func TestStandardInstallBundle_VersionConflict(t *testing.T) {
	const id = "anvil-standard-laravel"
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)
	bundle123, _ := installBundleTestRelease(t, id, "1.2.3",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	bundle124, _ := installBundleTestRelease(t, id, "1.2.4",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)

	if _, _, stderr, err := executeCommand("standard", "install-bundle", bundle123,
		"--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("first install-bundle failed: %v (stderr: %q)", err, stderr)
	}

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundle124,
		"--trust-anchors", anchorsFile)
	// Version conflict is a configuration conflict → exit 2
	// (TS-019-03-02, D-06 — the shared record-persistence path).
	requireExitCode(t, err, output.ExitCodeConfig)
	if !strings.Contains(stderr, "already installed at version") {
		t.Errorf("stderr missing version-conflict message: %q", stderr)
	}
	if !strings.Contains(stderr, "update flow") {
		t.Errorf("stderr missing the update-flow guidance: %q", stderr)
	}

	// The recorded version is unchanged: there is no offline update.
	rec := installTestReadRecord(t, id)
	if rec.Version != "1.2.3" {
		t.Errorf("record version = %s after a rejected conflict, want 1.2.3", rec.Version)
	}
}

// ── Anchors (fail-closed) ────────────────────────────────────────────

// TestStandardInstallBundle_NoAnchors verifies the default-fail anchor
// behavior (ADR-022 §3; PM decision D-07): an offline install without a
// configured anchor fails with an actionable message naming the
// publisher, the anchor path, the --trust-anchors flag, and the
// ANVIL_TRUST_ANCHORS environment variable — the anchors come from the
// operator, never from the bundle — and nothing is recorded.
//
// Reference: TS-014-05-02 (PM binding decisions 4, 7), TS-014-04-02
func TestStandardInstallBundle_NoAnchors(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)

	t.Run("trust anchors file missing", func(t *testing.T) {
		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", filepath.Join(t.TempDir(), "no-anchors.json"))
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "no trust anchors file found") {
			t.Errorf("stderr missing the missing-anchors message: %q", stderr)
		}
		if !strings.Contains(stderr, "--trust-anchors") || !strings.Contains(stderr, registry.EnvTrustAnchors) {
			t.Errorf("stderr missing the anchors override guidance: %q", stderr)
		}
		if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
			t.Errorf("record exists after a missing-anchors failure, want nothing recorded")
		}
	})

	t.Run("empty allowlist fails fast with the actionable no-anchor message", func(t *testing.T) {
		emptyAnchors := filepath.Join(t.TempDir(), "trust-anchors.json")
		if err := os.WriteFile(emptyAnchors, []byte(`{"publishers": {}}`), 0o644); err != nil {
			t.Fatalf("write empty anchors: %v", err)
		}
		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", emptyAnchors)
		requireExitCode(t, err, output.ExitCodeGeneral)
		// An empty allowlist means no publisher is trusted — the
		// first-run bootstrap file. It must fail the same way as a
		// missing file: at the pre-fetch gate, with the override
		// guidance (ADR-022 fail-fast, no first-use acceptance).
		if !strings.Contains(stderr, "no trust anchors file found") {
			t.Errorf("stderr missing the no-anchor message: %q", stderr)
		}
		if !strings.Contains(stderr, "--trust-anchors") || !strings.Contains(stderr, registry.EnvTrustAnchors) {
			t.Errorf("stderr missing publisher/override guidance: %q", stderr)
		}
		if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
			t.Errorf("record exists without anchors, want nothing recorded")
		}
	})
}

// ── Not Found / Size Cap ─────────────────────────────────────────────

// TestStandardInstallBundle_FileNotFound verifies the not-found exit
// code 3 contract: a missing bundle file fails with exit code 3
// (TS-P8-07), mirroring the online install's not-found convention.
//
// Reference: TS-014-05-02 (PM binding decision 1)
func TestStandardInstallBundle_FileNotFound(t *testing.T) {
	installTestEnv(t, nil)

	_, _, stderr, err := executeCommand("standard", "install-bundle",
		filepath.Join(t.TempDir(), "no-bundle.tar.gz"))
	requireExitCode(t, err, output.ExitCodeRuntime)
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr missing the not-found message: %q", stderr)
	}
}

// TestStandardInstallBundle_SizeCap verifies that a bundle archive
// exceeding the file size cap is rejected with an actionable error
// instead of being buffered unbounded — the bounded file read.
//
// Reference: TS-014-05-02 (security hardening; TS-014-05-01 footprint
// notes)
func TestStandardInstallBundle_SizeCap(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	// Shrink the cap below any real bundle: the read must reject the
	// file before OpenBundle ever runs. The content is large enough
	// that the compressed bundle exceeds the shrunken cap.
	origCap := standardBundleMaxFileBytes
	standardBundleMaxFileBytes = 1024
	t.Cleanup(func() { standardBundleMaxFileBytes = origCap })

	bigContent := make([]byte, 4096)
	rand.New(rand.NewSource(42)).Read(bigContent)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, bigContent, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
		"--trust-anchors", anchorsFile)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !strings.Contains(stderr, "size cap") {
		t.Errorf("stderr missing the size-cap rejection: %q", stderr)
	}
	if _, statErr := os.Stat(installTestRecordPath(t, id)); !os.IsNotExist(statErr) {
		t.Errorf("record exists after a size-cap rejection, want nothing recorded")
	}
}

// ── JSON Error Envelope ──────────────────────────────────────────────

// TestStandardInstallBundle_JSONErrorEnvelope verifies that a failing
// offline install with --json produces the TS-P8-05 error envelope on
// stdout (the machine-readable error surface; identical to the online
// install).
//
// Reference: TS-014-05-02 (PM binding decision 1), TS-P8-05
func TestStandardInstallBundle_JSONErrorEnvelope(t *testing.T) {
	installTestEnv(t, nil)

	_, stdout, _, _ := executeCommand("standard", "install-bundle",
		filepath.Join(t.TempDir(), "no-bundle.tar.gz"), "--json")

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

// ── Offline/Online Equivalence ───────────────────────────────────────

// TestStandardInstallBundle_EquivalentValidationPath pins the offline
// equivalence claim (ADR-023 §3): the offline flow validates the bundled
// metadata with the same engines the online flow uses — the same strict
// parse (via OpenBundle), the same lifecycle gate and
// ValidateCompatibility (ValidateAdoptionBeforeFetch), and the same
// VerifyTrust engine (via Bundle.Verify) — and records the same record
// shape, with the resolution distinguishing the bundle source. A
// framework-free project records the shape-only compatibility fact, and
// a project declaring a framework whose version cannot be determined is
// rejected exactly like the online install.
//
// Reference: TS-014-05-02 (PM binding decision 3; DoD: the offline
// validation path is equivalent to the online path)
func TestStandardInstallBundle_EquivalentValidationPath(t *testing.T) {
	const (
		id      = "anvil-standard-laravel"
		version = "1.2.3"
	)
	content := installTestStandardContent(id)
	pub, priv := installTestKeypair(t)

	installTestEnv(t, nil)
	bundlePath, _ := installBundleTestRelease(t, id, version,
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv)
	anchorsFile := installTestAnchorsFile(t, t.TempDir(), id, pub)

	t.Run("framework-free project records shape-only validation", func(t *testing.T) {
		_, _, stderr, err := executeCommand("standard", "install-bundle", bundlePath,
			"--trust-anchors", anchorsFile)
		if err != nil {
			t.Fatalf("framework-free offline install failed: %v (stderr: %q)", err, stderr)
		}
		rec := installTestReadRecord(t, id)
		if rec.Compatibility == nil || !rec.Compatibility.Valid {
			t.Fatalf("record compatibility = %+v, want a valid shape-only result", rec.Compatibility)
		}
		if rec.Compatibility.FrameworkVersionChecked {
			t.Errorf("frameworkVersionChecked = true in a framework-free project, want false (recorded, not assumed)")
		}
	})

	t.Run("framework declared but undeterminable rejects the install", func(t *testing.T) {
		// A different standard id so the previous install does not
		// interfere with the rejection.
		const otherID = "anvil-standard-flutter"
		otherContent := installTestStandardContent(otherID)
		otherBundle, _ := installBundleTestRelease(t, otherID, version,
			registry.LifecycleStatePublished, "", []string{"5.1.0"}, otherContent, pub, priv)
		installTestProject(t, t.TempDir(), "project:\n  name: my-app\n  framework: laravel\n")

		_, _, stderr, err := executeCommand("standard", "install-bundle", otherBundle,
			"--trust-anchors", anchorsFile)
		requireExitCode(t, err, output.ExitCodeGeneral)
		if !strings.Contains(stderr, "cannot be determined") {
			t.Errorf("stderr missing the undeterminable-version rejection: %q", stderr)
		}
		if _, statErr := os.Stat(installTestRecordPath(t, otherID)); !os.IsNotExist(statErr) {
			t.Errorf("record exists after the rejection, want nothing recorded")
		}
	})
}
