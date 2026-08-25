// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil adapter install" (TS-007-037, TS-016-04-01):
// identifier-safety validation, the registry-based resolution flow
// (index resolution, version pinning, trust anchors, full adoption
// validation), the binary install from the standard's release channel
// (checksum-verified), the already-installed gate (with and without
// --force), and --json output validity. No test touches the network:
// the install directory seam points at t.TempDir(), the registry index
// and trust anchors are staged locally, and the release content,
// binary, and checksums are served by a local TLS test server.
//
// Reference: TS-007-037, TS-016-04-01, ADR-022 §3, ADR-026, ADR-030
package cmd

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// stubAdapterInstallDirAt points the install-directory seam at dir and
// registers cleanup. Shared with the adapter discovery/list tests.
func stubAdapterInstallDirAt(t *testing.T, dir string) {
	t.Helper()
	orig := adapterInstallDir
	adapterInstallDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { adapterInstallDir = orig })
}

// adapterInstallTestRelease is one release version staged on a test
// server: the release archive content, the adapter binary served as the
// release asset, and — when set — the bytes the SHA256SUMS.txt is
// computed over (defaults to binary; a differing checksumBinary lets a
// test serve a TAMPERED binary against pristine checksums to exercise
// the checksum gate).
type adapterInstallTestRelease struct {
	content        []byte
	binary         []byte
	checksumBinary []byte
	// md is the release's registry metadata document, served at
	// registry-metadata-<version>.json when set (TS-014-04-04). It must
	// declare the content digest AND the attestation-bound digests of
	// the release's binary assets; tests that leave it nil exercise the
	// pre-TS-014-04-04 release shape (no attestation material).
	md registry.Metadata
}

// adapterInstallTestServer stages ONE TLS server serving the release
// channel paths of one or more versions of a standard (the release
// archive, the adapter binary asset, and SHA256SUMS.txt under each
// /releases/download/v<version>/ path — the GitHub release layout the
// registry distribution channel uses, ADR-030). It returns the server
// URL and a request log.
//
// A SINGLE server serves every version of a test: the TLS trust of both
// HTTP clients (the standard content client and the shared binary
// client) is established once per test, so multi-version tests never
// replace a CA pool mid-test (reviewer finding T-008: replacing the pool
// per httptest server silently breaks TLS trust for previously staged
// servers).
func adapterInstallTestServer(t *testing.T, id string, releases map[string]adapterInstallTestRelease) (string, *[]string) {
	t.Helper()
	var gotRequests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequests = append(gotRequests, r.URL.Path)
		// /releases/download/v<version>/<asset>
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/releases/download/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		version := strings.TrimPrefix(parts[0], "v")
		rel, ok := releases[version]
		if !ok {
			http.NotFound(w, r)
			return
		}
		checksummed := rel.checksumBinary
		if checksummed == nil {
			checksummed = rel.binary
		}
		asset := parts[1]
		archive := fmt.Sprintf("%s-%s.tar.gz", id, version)
		binaryAsset := adapterAssetName(strings.TrimPrefix(id, registry.StandardIDPrefix))
		switch asset {
		case archive:
			_, _ = w.Write(rel.content)
		case binaryAsset:
			_, _ = w.Write(rel.binary)
		case "SHA256SUMS.txt":
			binarySum := sha256.Sum256(checksummed)
			contentSum := sha256.Sum256(rel.content)
			// The release format: "sha256sum binaries/*" lines.
			_, _ = fmt.Fprintf(w, "%s  binaries/%s\n%s  binaries/%s\n",
				hex.EncodeToString(binarySum[:]), binaryAsset,
				hex.EncodeToString(contentSum[:]), archive)
		case "registry-metadata-" + version + ".json":
			// The release's registry metadata document (TS-014-04-04):
			// serves the attested contentDigests — including the named
			// binary digests — when the test staged one.
			if rel.md.ID == "" {
				http.NotFound(w, r)
				return
			}
			raw, err := json.MarshalIndent(rel.md, "", "  ")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	installTestAddTLSClient(t, server)
	return server.URL, &gotRequests
}

// installTestAddTLSClient makes BOTH the shared httpClient (adapter
// binary downloads and checksum fetches) and standardInstallHTTPClient
// (release content fetch) trust the test server's TLS certificate,
// ACCUMULATING the new CA into the pool both clients already trust —
// never replacing it, so a test staging several TLS servers keeps every
// server trustworthy (reviewer finding T-008). The standard content
// client keeps the production redirect policy (newStandardInstallHTTPClient).
func installTestAddTLSClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	pool := trustedClientPool(httpClient)
	if cert := server.Certificate(); cert != nil {
		pool.AddCert(cert)
	}

	origHTTP := httpClient
	httpClient = &http.Client{
		Timeout:   httpClient.Timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}
	t.Cleanup(func() { httpClient = origHTTP })

	origStd := standardInstallHTTPClient
	client := newStandardInstallHTTPClient()
	client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	standardInstallHTTPClient = client
	t.Cleanup(func() { standardInstallHTTPClient = origStd })
}

// trustedClientPool returns the RootCAs pool the client's transport
// currently trusts, or a fresh pool seeded from the system roots when the
// client carries none (never nil).
func trustedClientPool(c *http.Client) *x509.CertPool {
	if tr, ok := c.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil && tr.TLSClientConfig.RootCAs != nil {
		return tr.TLSClientConfig.RootCAs
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	return pool
}

// adapterInstallTestEnv isolates the command's global state for one test
// and stages the full registry environment of an adapter install:
//
//   - XDG_CONFIG_HOME → a temp dir (record store + default index/anchors
//     resolve into the test, never the real user config);
//   - ANVIL_COMPATIBILITY_MATRIX → the repository's compatibility matrix
//     record;
//   - ONE TLS server serving the release channel of the standard's
//     1.0.0 release (the release archive — the distribution location —
//     the adapter binary asset, and SHA256SUMS.txt in the release
//     format "<hash>  binaries/<asset>"; the checksum file is computed
//     over checksumBinary, so a test can serve a TAMPERED binary
//     (binary) against pristine checksums (checksumBinary) to exercise
//     the checksum gate); both HTTP clients trust the server (see
//     installTestAddTLSClient);
//   - a static index entry for the release and a trust anchors file
//     anchoring the publisher.
//
// It returns the index dir and the anchors file path for the command
// flags. The server request log is available for path assertions.
func adapterInstallTestEnv(t *testing.T, name string, content, binary, checksumBinary []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey, extraDigests ...registry.ContentDigest) (indexDir, anchorsFile string, requests *[]string) {
	t.Helper()

	const version = "1.0.0"
	id := adapterStandardIDForName(name)

	installTestEnv(t, nil)
	serverURL, requests := adapterInstallTestServer(t, id, map[string]adapterInstallTestRelease{
		version: {content: content, binary: binary, checksumBinary: checksumBinary},
	})

	indexDir = t.TempDir()
	anchorsFile = installTestAnchorsFile(t, t.TempDir(), id, pub)
	md := installTestRelease(t, id, version, serverURL+"/releases/download/v"+version+"/"+id+"-"+version+".tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, content, pub, priv, extraDigests...)
	installTestIndexEntry(t, indexDir, md)
	return indexDir, anchorsFile, requests
}

// adapterInstallBinary is deterministic adapter binary payload for tests.
func adapterInstallBinary(name string) []byte {
	return []byte("adapter binary payload for " + name + " (TS-016-04-01 registry-based adapter install tests)")
}

// TestAdapterInstall_InvalidName verifies that an unsafe adapter name —
// one that could escape the install directory as a path component — is
// rejected with a clear error before any registry, network, or
// filesystem activity.
//
// Reference: TS-007-037 §3, ADR-026
func TestAdapterInstall_InvalidName(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir())

	_, _, stderr, err := executeCommand("adapter", "install", "../evil")
	if err == nil {
		t.Fatal("expected error for unsafe adapter name, got nil")
	}
	if !strings.Contains(stderr, "invalid adapter name") {
		t.Errorf("stderr should reject the unsafe name, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil adapter list --available") {
		t.Errorf("stderr should point at the registry availability surface, got: %s", stderr)
	}
}

// TestAdapterInstall_UnofferedNameFailsWithRegistryError verifies the
// registry-based resolution (TS-016-04-01): an adapter whose standard is
// not offered in the index fails with an actionable error — the registry
// is the source of truth for what exists, and nothing is downloaded.
//
// Reference: TS-016-04-01, ADR-025 §3.5
func TestAdapterInstall_UnofferedNameFailsWithRegistryError(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, _, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), adapterInstallBinary("laravel"), adapterInstallBinary("laravel"), pub, priv)

	_, _, stderr, err := executeCommand("adapter", "install", "node", "--index", indexDir)
	if err == nil {
		t.Fatal("expected an error for an adapter not offered in the registry, got nil")
	}
	if !strings.Contains(stderr, "not offered") && !strings.Contains(stderr, "could not resolve adapter") {
		t.Errorf("stderr should report the registry resolution failure, got: %s", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "anvil-adapter-node")); !os.IsNotExist(statErr) {
		t.Errorf("no adapter binary may be installed for an unoffered adapter")
	}
}

// TestAdapterInstall_InstallsWhenAbsent verifies the full registry flow:
// the standard release is resolved from the index, validated (parse,
// lifecycle, compatibility, integrity, attestation, trust anchor),
// recorded, and the adapter binary is downloaded from the SAME release
// (checksum-verified) and placed next to the CLI.
//
// Reference: TS-007-037 AC-1, AC-2, TS-016-04-01
func TestAdapterInstall_InstallsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("adapter binary not installed at %s: %v", targetPath, err)
	}
	if string(data) != string(binary) {
		t.Errorf("installed binary content = %q, want the release asset", data)
	}
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm installation, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "standard anvil-standard-laravel 1.0.0 recorded") {
		t.Errorf("stdout should confirm the recorded standard adoption, got:\n%s", stdout)
	}

	// The binary must come from the standard's release channel, not from
	// a Core release: the release-download path of the served channel.
	found := false
	for _, path := range *requests {
		if strings.Contains(path, "/releases/download/v1.0.0/anvil-adapter-laravel-") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no request for the adapter binary on the standard release channel; requests: %v", *requests)
	}

	// The adoption is recorded (registry path): the installed-standard
	// record exists with the pinned version and validation results.
	rec := installTestReadRecord(t, "anvil-standard-laravel")
	if rec.ID != "anvil-standard-laravel" || rec.Version != "1.0.0" {
		t.Errorf("record identity = %s %s, want anvil-standard-laravel 1.0.0", rec.ID, rec.Version)
	}
	if rec.Trust == nil || !rec.Trust.Valid {
		t.Errorf("record trust = %+v, want a valid embedded result", rec.Trust)
	}
	if rec.Compatibility == nil || !rec.Compatibility.Valid {
		t.Errorf("record compatibility = %+v, want a valid embedded result", rec.Compatibility)
	}
}

// TestAdapterInstall_AlreadyInstalledWithoutForce verifies that an
// existing adapter is left untouched and the command reports it
// informatively with exit 0 (non-fatal) — the gate runs before any
// registry or network activity.
//
// Reference: TS-007-037 AC-1, §3
func TestAdapterInstall_AlreadyInstalledWithoutForce(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel")
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "already installed") || !strings.Contains(stdout, "--force") {
		t.Errorf("stdout should report already-installed state with --force hint, got:\n%s", stdout)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "existing binary")
}

// TestAdapterInstall_ForceReinstalls verifies that --force re-downloads,
// re-verifies, and replaces an existing adapter through the registry
// flow.
//
// Reference: TS-007-037 AC-1, §3, TS-016-04-01
func TestAdapterInstall_ForceReinstalls(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install --force returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(binary))
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm reinstallation, got:\n%s", stdout)
	}
}

// TestAdapterInstall_ChecksumMismatch verifies that a tampered adapter
// binary download is caught: the command fails, the replace never
// happens, and an existing binary survives.
//
// Reference: TS-007-037 AC-7
func TestAdapterInstall_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	pub, priv := installTestKeypair(t)
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), []byte("tampered"), adapterInstallBinary("laravel"), pub, priv)

	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(stderr, "binary verification failed") {
		t.Errorf("stderr should mention checksum verification, got: %s", stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "existing binary")
}

// TestAdapterInstall_MissingAnchorsFailFast verifies the anchor handling
// of the registry flow: without a trust anchors file, the install fails
// with an actionable error BEFORE any release content is fetched — no
// download is wasted (the anchors load is a local operation).
//
// Reference: TS-016-04-01, ADR-022 §3 (no first-use acceptance)
func TestAdapterInstall_MissingAnchorsFailFast(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, _, requests := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), adapterInstallBinary("laravel"), adapterInstallBinary("laravel"), pub, priv)

	// No --trust-anchors: the default path (XDG_CONFIG_HOME isolated by
	// installTestEnv) does not exist.
	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir)
	if err == nil {
		t.Fatal("expected missing-anchors error, got nil")
	}
	if !strings.Contains(stderr, "no trust anchors file found") {
		t.Errorf("stderr should report the missing trust anchors file, got: %s", stderr)
	}
	if len(*requests) != 0 {
		t.Errorf("no release content may be fetched without anchors; requests: %v", *requests)
	}
}

// TestAdapterInstall_TrustFailure verifies that a trust anchor mismatch —
// the operator anchored a DIFFERENT key than the release's attestation
// public key — aborts the install with an actionable error and no binary
// is installed (no first-use acceptance, ADR-022 §3).
//
// Reference: TS-016-04-01, ADR-022 §3
func TestAdapterInstall_TrustFailure(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	otherPub, _ := installTestKeypair(t)
	indexDir, _, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), adapterInstallBinary("laravel"), adapterInstallBinary("laravel"), pub, priv)

	// Anchor the WRONG key: the release was signed with pub.
	wrongAnchors := installTestAnchorsFile(t, t.TempDir(), "anvil-standard-laravel", otherPub)

	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", wrongAnchors)
	if err == nil {
		t.Fatal("expected trust failure, got nil")
	}
	if !strings.Contains(stderr, "trust verification failed") {
		t.Errorf("stderr should report the trust verification failure, got: %s", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "anvil-adapter-laravel")); !os.IsNotExist(statErr) {
		t.Errorf("no adapter binary may be installed after a trust failure")
	}
}

// TestAdapterInstall_UsesRecordedVersion verifies version pinning
// (TS-016-04-01): when the standard is already installed at a version,
// the adapter binary is adopted at the RECORDED version — never silently
// bumped to a newer index release (version change is an update,
// TS-014-03-02).
//
// Reference: TS-016-04-01, TS-014-03-02, ADR-022 §3
func TestAdapterInstall_UsesRecordedVersion(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv)

	// First install records anvil-standard-laravel 1.0.0 (the only
	// offered version) and installs the binary.
	if _, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("first adapter install failed: %v (stderr: %s)", err, stderr)
	}

	// A NEWER release 1.1.0 is added to the index: an install must NOT
	// silently move to it — the recorded version pins the adoption. Both
	// versions are served by the SAME TLS server (reviewer finding T-008:
	// one server per test keeps the TLS trust of both clients
	// unambiguous).
	const newVersion = "1.1.0"
	newContent := []byte("release content v1.1.0")
	newBinary := []byte("adapter binary v1.1.0")
	serverURL, _ := adapterInstallTestServer(t, "anvil-standard-laravel", map[string]adapterInstallTestRelease{
		newVersion: {content: newContent, binary: newBinary},
	})

	newMD := installTestRelease(t, "anvil-standard-laravel", newVersion,
		serverURL+"/releases/download/v"+newVersion+"/anvil-standard-laravel-"+newVersion+".tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"}, newContent, pub, priv)
	installTestIndexEntry(t, indexDir, newMD)

	// Reinstall with --force: the binary must be re-adopted at the
	// RECORDED 1.0.0, not the new 1.1.0.
	if _, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("forced reinstall failed: %v (stderr: %s)", err, stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(binary))
	rec := installTestReadRecord(t, "anvil-standard-laravel")
	if rec.Version != "1.0.0" {
		t.Errorf("record version = %q, want the pinned 1.0.0 (no silent bump)", rec.Version)
	}
	for _, path := range *requests {
		if strings.Contains(path, "/releases/download/v1.1.0/") {
			t.Errorf("adapter install must not fetch the newer release: request %s", path)
		}
	}
}

// TestAdapterInstall_HighestAdoptableVersion verifies that a fresh
// install (no recorded standard) adopts the highest ADOPTABLE version
// offered in the index.
//
// Reference: TS-016-04-01
func TestAdapterInstall_HighestAdoptableVersion(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv)

	// Add a retired 2.0.0 (NOT adoptable) and a published 1.1.0
	// (adoptable, higher than 1.0.0) — ALL served by ONE TLS server
	// (reviewer finding T-008: one server per test keeps the TLS trust
	// of both clients unambiguous).
	serverURL, _ := adapterInstallTestServer(t, "anvil-standard-laravel", map[string]adapterInstallTestRelease{
		"1.1.0": {content: []byte("release content 1.1.0"), binary: []byte("adapter binary 1.1.0")},
		"2.0.0": {content: []byte("release content 2.0.0"), binary: []byte("adapter binary 2.0.0")},
	})
	for _, version := range []string{"1.1.0", "2.0.0"} {
		state := registry.LifecycleStatePublished
		if version == "2.0.0" {
			state = registry.LifecycleStateRetired
		}
		md := installTestRelease(t, "anvil-standard-laravel", version,
			serverURL+"/releases/download/v"+version+"/anvil-standard-laravel-"+version+".tar.gz",
			state, "", []string{"5.1.0"}, []byte("release content "+version), pub, priv)
		installTestIndexEntry(t, indexDir, md)
	}

	// The retired 2.0.0 is NOT offered; 1.1.0 is the highest adoptable.
	if _, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("adapter install failed: %v (stderr: %s)", err, stderr)
	}
	rec := installTestReadRecord(t, "anvil-standard-laravel")
	if rec.Version != "1.1.0" {
		t.Errorf("record version = %q, want the highest adoptable 1.1.0 (retired 2.0.0 not offered)", rec.Version)
	}
}

// TestAdapterInstall_RetiredOnlyNotOffered verifies that an index whose
// only releases are retired cannot be adopted: the command fails with an
// actionable error and installs nothing.
//
// Reference: TS-016-04-01, ADR-027 §3
func TestAdapterInstall_RetiredOnlyNotOffered(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	content := installTestStandardContent("anvil-standard-laravel")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // nothing must be fetched
	}))
	defer server.Close()
	installTestEnv(t, server)

	indexDir := t.TempDir()
	md := installTestRelease(t, "anvil-standard-laravel", "1.0.0", server.URL+"/release.tar.gz",
		registry.LifecycleStateRetired, "", []string{"5.1.0"}, content, pub, priv)
	installTestIndexEntry(t, indexDir, md)

	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir)
	if err == nil {
		t.Fatal("expected error for a retired-only standard, got nil")
	}
	if !strings.Contains(stderr, "no adoptable release") {
		t.Errorf("stderr should report that no adoptable release is offered, got: %s", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "anvil-adapter-laravel")); !os.IsNotExist(statErr) {
		t.Errorf("no adapter binary may be installed for a retired-only standard")
	}
}

// TestAdapterInstall_ShowsProgressSteps verifies that non-JSON installs
// report the registry + binary phases, so interactive users see live
// progress instead of a silent hang.
//
// Reference: TS-007-037 §3, TS-016-04-01, TS-008-009
func TestAdapterInstall_ShowsProgressSteps(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), adapterInstallBinary("laravel"), adapterInstallBinary("laravel"), pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Install adapter laravel",
		"Step: Resolve anvil-standard-laravel from the registry index",
		"Step: Verify release (integrity, attestation, trust anchors)",
		"Step: Download anvil-adapter-laravel-" + runtime.GOOS + "-" + runtime.GOARCH,
		"Step: Verify binary",
		"Step: Install to " + filepath.Join(dir, "anvil-adapter-laravel"),
		"Adapter laravel installed",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestAdapterInstall_ProgressOnFailure verifies that a failed install
// marks the overall workflow as failed in the progress output while the
// error detail goes to stderr.
//
// Reference: TS-007-037 AC-7, TS-008-009
func TestAdapterInstall_ProgressOnFailure(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), []byte("tampered"), adapterInstallBinary("laravel"), pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(stdout, "Install adapter laravel failed") {
		t.Errorf("stdout should mark the workflow as failed, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "binary verification failed") {
		t.Errorf("stderr should mention checksum verification, got: %s", stderr)
	}
}

// TestAdapterInstall_JSON verifies the --json envelope: a success object
// with adapter/standard/version/status/path/message under the standard
// envelope (TS-P8-05).
//
// Reference: TS-007-037 AC-6, TS-016-04-01
func TestAdapterInstall_JSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "flutter", installTestStandardContent("anvil-standard-flutter"), adapterInstallBinary("flutter"), adapterInstallBinary("flutter"), pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "flutter", "--json", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an install result: %v\n%s", err, raw)
	}
	if result.Adapter != "flutter" {
		t.Errorf("result.adapter = %q, want %q", result.Adapter, "flutter")
	}
	if result.Standard != "anvil-standard-flutter" || result.Version != "1.0.0" {
		t.Errorf("result standard = %s %s, want anvil-standard-flutter 1.0.0", result.Standard, result.Version)
	}
	if result.Status != "installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "installed")
	}
	wantPath := filepath.Join(dir, "anvil-adapter-flutter")
	if result.Path != wantPath {
		t.Errorf("result.path = %q, want %q", result.Path, wantPath)
	}
	if result.Message == "" {
		t.Error("result.message should not be empty")
	}
}

// TestAdapterInstall_AlreadyInstalledJSON verifies the JSON shape for the
// already-installed gate (still a success envelope, exit 0).
//
// Reference: TS-007-037 AC-6
func TestAdapterInstall_AlreadyInstalledJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing")

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter install --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an install result: %v\n%s", err, raw)
	}
	if result.Status != "already installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "already installed")
	}
	if !strings.Contains(result.Message, "--force") {
		t.Errorf("result.message should hint --force, got: %s", result.Message)
	}
}

// TestAdapterInstall_InvalidNameJSON verifies that an unsafe adapter
// name produces the error envelope with --json (errors are conveyed
// through the machine-readable envelope, TS-P8-05) AND that the process
// still exits non-zero — a failure must never exit 0 (TS-019-03-02).
//
// Reference: TS-007-037 AC-6, ADR-026
func TestAdapterInstall_InvalidNameJSON(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir())

	_, stdout, _, err := executeCommand("adapter", "install", "../evil", "--json")
	if err == nil {
		t.Fatal("adapter install --json should return an error for an invalid adapter name (exit non-zero), got nil")
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if !strings.Contains(envelope.Error, "invalid adapter name") {
		t.Errorf("envelope error should mention the invalid adapter name, got: %s", envelope.Error)
	}
}

// TestAdapterInstall_AssetNameUsesCurrentPlatform verifies that the
// command requests the asset name for the current platform
// (anvil-adapter-<name>-<goos>-<goarch>) from the standard's release
// channel — the resolution contract naming is unchanged (T-002/T-004).
//
// Reference: TS-007-037 §3, TS-016-04-01, ADR-025 §3.4
func TestAdapterInstall_AssetNameUsesCurrentPlatform(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel", installTestStandardContent("anvil-standard-laravel"), adapterInstallBinary("laravel"), adapterInstallBinary("laravel"), pub, priv)

	if _, _, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile); err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	want := fmt.Sprintf("/releases/download/v1.0.0/anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH)
	found := false
	for _, path := range *requests {
		if path == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no request for the platform asset %s; requests: %v", want, *requests)
	}
}

// ── Attestation-bound binary verification (TS-014-04-04) ────────────

// testBinaryDigestEntry builds the named content digest entry the release
// pipeline publishes for a binary asset (TS-014-04-04): the sha-256
// base16 digest of the asset payload, bound to the asset name.
func testBinaryDigestEntry(assetName string, payload []byte) registry.ContentDigest {
	sum := sha256.Sum256(payload)
	return registry.ContentDigest{
		Algorithm: registry.DigestAlgorithmSHA256,
		Encoding:  registry.DigestEncodingBase16,
		Digest:    hex.EncodeToString(sum[:]),
		Name:      assetName,
	}
}

// TestAdapterInstall_AttestationBoundBinary_Valid verifies the
// TS-014-04-04 trust path end-to-end: a release whose metadata document
// carries the attestation-bound digest of the platform binary installs
// successfully, and the same-channel SHA256SUMS.txt is NEVER fetched —
// the attested digest supersedes it (an attacker who swaps the binary
// AND the unsigned checksum file is still caught).
func TestAdapterInstall_AttestationBoundBinary_Valid(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	assetName := adapterAssetName("laravel")
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel",
		installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv,
		testBinaryDigestEntry(assetName, binary))

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(binary))
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm installation, got:\n%s", stdout)
	}
	if strings.Contains(stderr, "warning:") {
		t.Errorf("no degradation warning expected on the attestation path, got: %s", stderr)
	}
	for _, path := range *requests {
		if strings.Contains(path, "SHA256SUMS.txt") {
			t.Errorf("attestation-bound path must not fetch SHA256SUMS.txt; requests: %v", *requests)
		}
	}
}

// TestAdapterInstall_AttestationBoundBinary_TamperedBinary verifies the
// tampered-binary abort (TS-014-04-04): the downloaded binary does not
// match the attestation-bound digest — the install fails with an
// actionable error, the replace never happens, and an existing binary
// survives.
func TestAdapterInstall_AttestationBoundBinary_TamperedBinary(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	pub, priv := installTestKeypair(t)
	pristine := adapterInstallBinary("laravel")
	tampered := []byte("TAMPERED binary payload (TS-014-04-04)")
	assetName := adapterAssetName("laravel")
	// The metadata declares the digest of the PRISTINE binary while the
	// release channel serves the TAMPERED bytes (the same-channel
	// attacker's move).
	indexDir, anchorsFile, _ := adapterInstallTestEnv(t, "laravel",
		installTestStandardContent("anvil-standard-laravel"), tampered, tampered, pub, priv,
		testBinaryDigestEntry(assetName, pristine))

	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("expected an attestation-bound digest mismatch error, got nil")
	}
	if !strings.Contains(stderr, "attestation-bound digest mismatch") {
		t.Errorf("stderr should report the attestation-bound mismatch, got: %s", stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "existing binary")
}

// TestAdapterInstall_AttestationBoundBinary_TamperedChecksum verifies
// the same-channel checksum file is no longer the trust source when
// attestation material exists (TS-014-04-04): the release serves a
// TAMPERED SHA256SUMS.txt, but the pristine binary matches its
// attestation-bound digest — the install succeeds and never consults the
// checksum file.
func TestAdapterInstall_AttestationBoundBinary_TamperedChecksum(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	assetName := adapterAssetName("laravel")
	// checksumBinary differs from binary → the served SHA256SUMS.txt is
	// tampered; the attestation-bound digest still verifies the binary.
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel",
		installTestStandardContent("anvil-standard-laravel"), binary, []byte("tampered-checksum-bytes"), pub, priv,
		testBinaryDigestEntry(assetName, binary))

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(binary))
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm installation, got:\n%s", stdout)
	}
	if strings.Contains(stderr, "warning:") {
		t.Errorf("no degradation warning expected on the attestation path, got: %s", stderr)
	}
	for _, path := range *requests {
		if strings.Contains(path, "SHA256SUMS.txt") {
			t.Errorf("attestation-bound path must not fetch the tampered checksum file; requests: %v", *requests)
		}
	}
}

// TestAdapterInstall_NoAttestationMaterial_FallsBackWithWarning
// verifies the backward-compatible degradation (TS-014-04-04): a release
// WITHOUT the new trust material (the already-published v1.0.0 shape —
// no named binary digests) keeps installing through today's same-channel
// checksum, WITH an explicit warning — never a silent trust downgrade,
// never fail-closed for old releases.
func TestAdapterInstall_NoAttestationMaterial_FallsBackWithWarning(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	indexDir, anchorsFile, requests := adapterInstallTestEnv(t, "laravel",
		installTestStandardContent("anvil-standard-laravel"), binary, binary, pub, priv)

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(binary))
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm installation, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "no attestation-bound digest") {
		t.Errorf("stderr should carry the explicit degradation warning, got: %s", stderr)
	}
	found := false
	for _, path := range *requests {
		if strings.Contains(path, "SHA256SUMS.txt") {
			found = true
		}
	}
	if !found {
		t.Errorf("fallback path must fetch SHA256SUMS.txt; requests: %v", *requests)
	}
}

// ── Asset names are signed material — CLI path (security review F-2) ─

// TestAdapterInstall_TamperedMetadataNameStripFailsClosed verifies the
// F-2 attack on the CLI path: a same-channel attacker swaps the binary,
// the SHA256SUMS.txt, AND the registry metadata document with the asset
// NAME STRIPPED from the signed entry (hoping the digest bytes alone
// keep the signature valid and force the checksum fallback). The name is
// signed material — the payload changes, the attestation fails, and the
// adoption aborts BEFORE any binary is installed.
func TestAdapterInstall_TamperedMetadataNameStripFailsClosed(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	trojan := []byte("TROJAN binary payload (F-2 name-strip attack)")
	assetName := adapterAssetName("laravel")

	// Stage a release whose metadata declares the digest of the TROJAN
	// binary under a properly signed named entry (the attacker recomputes
	// the digest), then STRIP the name AFTER signing — the signature no
	// longer covers the entry's bytes.
	md := installTestRelease(t, "anvil-standard-laravel", "1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"},
		installTestStandardContent("anvil-standard-laravel"), pub, priv,
		testBinaryDigestEntry(assetName, trojan))
	md.Trust.ContentDigests[1].Name = "" // attacker strips the name post-signing

	indexDir, anchorsFile, _ := adapterInstallTestEnvWithRelease(t, "laravel", md, trojan, trojan)
	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("expected the adoption to fail on the stripped-name metadata, got nil")
	}
	if !strings.Contains(stderr, "attestation") && !strings.Contains(stderr, "signature") {
		t.Errorf("stderr should report the broken attestation, got: %s", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "anvil-adapter-laravel")); !os.IsNotExist(statErr) {
		t.Errorf("no binary may be installed from a tampered-metadata release")
	}
}

// TestAdapterInstall_TamperedMetadataRenameFailsClosed verifies the
// cross-asset rename on the CLI path: renaming a signed entry (installing
// the laravel binary as flutter) changes the signed name bytes, the
// attestation fails, and the adoption aborts.
func TestAdapterInstall_TamperedMetadataRenameFailsClosed(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	pub, priv := installTestKeypair(t)
	binary := adapterInstallBinary("laravel")
	assetName := adapterAssetName("laravel")

	md := installTestRelease(t, "anvil-standard-laravel", "1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		registry.LifecycleStatePublished, "", []string{"5.1.0"},
		installTestStandardContent("anvil-standard-laravel"), pub, priv,
		testBinaryDigestEntry(assetName, binary))
	// Attacker renames the entry AFTER signing (identity confusion).
	md.Trust.ContentDigests[1].Name = adapterAssetName("flutter")

	indexDir, anchorsFile, _ := adapterInstallTestEnvWithRelease(t, "laravel", md, binary, binary)
	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force", "--index", indexDir, "--trust-anchors", anchorsFile)
	if err == nil {
		t.Fatal("expected the adoption to fail on the renamed metadata, got nil")
	}
	if !strings.Contains(stderr, "attestation") && !strings.Contains(stderr, "signature") {
		t.Errorf("stderr should report the broken attestation, got: %s", stderr)
	}
}

// adapterInstallTestEnvWithRelease is the low-level variant of
// adapterInstallTestEnv: it stages the release channel from an EXPLICIT
// metadata document (used by the tampered-metadata tests, which need a
// document that was mutated AFTER signing) instead of deriving one.
//
// The document's distribution.location is REWRITTEN to the local test
// server's release channel (the pattern of adapterInstallTestEnv): the
// location is NOT signed material (the canonical attestation payload
// covers id, version, and the digest/name entries only), so mutating it
// keeps the signature valid while routing every download through the
// local server — a test that left the hardcoded github.com location in
// place would make the CLI fetch from the real network (reviewer
// finding N-1: 60s timeout instead of exercising the F-2 path).
func adapterInstallTestEnvWithRelease(t *testing.T, name string, md registry.Metadata, binary, checksumBinary []byte) (indexDir, anchorsFile string, requests *[]string) {
	t.Helper()
	version := md.Version
	id := md.ID

	installTestEnv(t, nil)
	serverURL, requests := adapterInstallTestServer(t, id, map[string]adapterInstallTestRelease{
		version: {content: installTestStandardContent(id), binary: binary, checksumBinary: checksumBinary, md: md},
	})
	md.Distribution.Location = serverURL + "/releases/download/v" + version + "/" + id + "-" + version + ".tar.gz"

	indexDir = t.TempDir()
	anchorsFile = installTestAnchorsFile(t, t.TempDir(), id, mustPub(t, md))
	installTestIndexEntry(t, indexDir, md)
	return indexDir, anchorsFile, requests
}

// mustPub extracts the test public key from a metadata document's
// declared attestation publicKey.
func mustPub(t *testing.T, md registry.Metadata) ed25519.PublicKey {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(md.Trust.Attestation.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		t.Fatalf("metadata carries no valid test public key: %v", err)
	}
	return ed25519.PublicKey(raw)
}
