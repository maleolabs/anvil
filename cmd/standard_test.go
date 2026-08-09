// Package cmd implements the Anvil CLI commands.
//
// Tests for the standard command group (TS-014-02-02): the parent-only
// group registration, the static index path resolution (--index flag →
// ANVIL_REGISTRY_INDEX → default), and the dual-run coexistence with the
// v1.x adapter discovery (ADR-023).
//
// The standard subcommands read a local static index. Tests assemble
// fixture-based index trees in t.TempDir() from registry-valid metadata
// documents (standardFixtureDoc) and structurally decodable but
// schema-invalid documents (standardInvalidDoc), so every test is
// self-contained.
//
// Reference: TS-014-02-02, ADR-023, ADR-030
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/registry"
)

// writeStandardIndexDoc writes content to relPath under dir (creating
// parent directories), so tests can assemble static index layouts in
// t.TempDir().
func writeStandardIndexDoc(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// standardFixtureDoc returns a registry-valid metadata document for the
// given standard id, version, lifecycle state, and optional removal
// date. The document is built by marshaling the Go mirror, so ids and
// versions are always JSON-escaped correctly; trust material reuses the
// corpus fixture values, so registry.Parse accepts the document.
func standardFixtureDoc(id, version, state, removalDate string) string {
	md := registry.Metadata{
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Capability: registry.Capability{
			FrameworkVersion: []string{"5.1.0", "5.2.0", "5.3.0"},
		},
		Distribution: registry.Distribution{
			Type:     registry.DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/" + id + "/releases/download/v" + version + "/" + id + ".tar.gz",
		},
		Lifecycle: registry.Lifecycle{
			State:       state,
			RemovalDate: removalDate,
		},
		Trust: registry.Trust{
			ContentDigests: []registry.ContentDigest{{
				Algorithm: registry.DigestAlgorithmSHA256,
				Encoding:  registry.DigestEncodingBase16,
				Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			}},
			Attestation: registry.Attestation{
				Algorithm: registry.AttestationAlgorithmEd25519,
				Signature: "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				PublicKey: "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA==",
			},
		},
	}
	raw, err := json.Marshal(md)
	if err != nil {
		panic(fmt.Sprintf("marshal test metadata: %v", err)) // cannot happen: Metadata marshals losslessly
	}
	return string(raw)
}

// standardInvalidDoc returns a document that structurally decodes — so
// the index client loads it (TS-014-02-01) — but fails strict registry
// validation (TS-014-01-02): the lifecycle state is not one of the three
// governed machine values. The rest of the document is registry-valid so
// the only validation problem is the lifecycle enum.
func standardInvalidDoc(id, version string) string {
	return `{
		"id": "` + id + `",
		"version": "` + version + `",
		"contractVersion": "1.0.0",
		"capability": {"frameworkVersion": ["5.1.0"]},
		"distribution": {"type": "github-releases", "location": "https://github.com/maleolabs/` + id + `/releases/download/v` + version + `/` + id + `.tar.gz"},
		"lifecycle": {"state": "unknown-state"},
		"trust": {
			"contentDigests": [
				{"algorithm": "sha-256", "encoding": "base16", "digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}
			],
			"attestation": {
				"algorithm": "ed25519",
				"signature": "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				"publicKey": "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
			}
		}
	}`
}

// ── Command Group Registration ───────────────────────────────────────

// TestStandardCommand_RegistersSubcommands verifies that the standard
// group is a parent-only namespace (ADR-010 §6.7) with the list and
// inspect subcommands registered.
//
// Reference: TS-014-02-02, ADR-010 §6.7
func TestStandardCommand_RegistersSubcommands(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"standard"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"standard\"]) returned error: %v", err)
	}
	if group == nil {
		t.Fatal("standard command group not found")
	}

	// Parent-only: no RunE, Run, or Args (ADR-010 §6.7).
	if group.RunE != nil {
		t.Error("standard group has RunE set; parent groups should not have execution logic")
	}
	if group.Run != nil {
		t.Error("standard group has Run set; parent groups should not have execution logic")
	}
	if group.Args != nil {
		t.Error("standard group has custom Args validator; parent groups should not")
	}

	registered := make(map[string]bool)
	for _, sub := range group.Commands() {
		registered[sub.Name()] = true
	}
	for _, want := range []string{"list", "inspect"} {
		if !registered[want] {
			t.Errorf("standard subcommand %q not registered", want)
		}
	}
}

// TestStandardGroup_HelpListsStandardVocabulary verifies that the
// canonical group help speaks the standard vocabulary (TS-019-01-02):
// it lists the standard-named subcommands, carries the "formerly
// adapter" migration pointer (product review note c), and never prints
// a deprecation warning — the standard surface is the canonical one.
func TestStandardGroup_HelpListsStandardVocabulary(t *testing.T) {
	_, stdout, stderr, err := executeCommand("standard", "--help")
	if err != nil {
		t.Fatalf("'standard --help' must succeed: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"list",
		"inspect",
		"install",
		"install-bundle",
		"update",
		`formerly named "adapter"`,
		"docs/migration-guide-v2.md",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("standard --help must contain %q, got:\n%s", want, stdout)
		}
	}

	if strings.Contains(stdout, "is deprecated") || strings.Contains(stderr, "is deprecated") {
		t.Errorf("standard --help must not carry a deprecation warning, stdout: %s, stderr: %s", stdout, stderr)
	}
}

// ── Domain Group Registration (root.go) ──────────────────────────────

// TestStandardDomainGroup_Development verifies that the standard group
// is listed under the Development domain in the root domain help
// grouping, mirroring the adapter entry — the command surface must be
// discoverable from the CLI's main navigation (CR finding 1, product
// gap 1).
func TestStandardDomainGroup_Development(t *testing.T) {
	var development *domainGroup
	for i := range rootDomainGroups {
		if rootDomainGroups[i].Name == "Development" {
			development = &rootDomainGroups[i]
		}
	}
	if development == nil {
		t.Fatal("Development domain group must exist")
	}
	if !containsString(development.Commands, "standard") {
		t.Errorf("standard must be grouped under Development, got: %v", development.Commands)
	}
}

// TestStandardDomainGroup_HelpOutput verifies that the rendered top-level
// help lists the standard command inside the Development section.
func TestStandardDomainGroup_HelpOutput(t *testing.T) {
	_, stdout, _, err := executeCommand()
	if err != nil {
		t.Fatalf("bare 'anvil' help must succeed: %v", err)
	}

	block := stdout
	if start := strings.Index(stdout, "Product Domains:"); start >= 0 {
		block = stdout[start:]
	}
	if end := strings.Index(block, `Use "anvil [command] --help"`); end >= 0 {
		block = block[:end]
	}

	// Scan the Development section: a two-space-indented header line
	// followed by four-space-indented command entries.
	lines := strings.Split(block, "\n")
	section := ""
	found := false
	for _, line := range lines {
		if len(line) > 0 && line[0] != ' ' {
			section = ""
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			section = strings.TrimSpace(line)
			continue
		}
		if section == "Development" && strings.Contains(line, "standard") {
			found = true
		}
	}
	if !found {
		t.Errorf("Development help section should list the standard command, got:\n%s", block)
	}
}

// ── Index Path Resolution ────────────────────────────────────────────

// TestStandardIndexResolution_FlagWins verifies the --index flag value
// takes precedence over the environment variable.
func TestStandardIndexResolution_FlagWins(t *testing.T) {
	t.Setenv(envStandardIndex, "/env/index")

	path, source, err := resolveStandardIndex("/flag/index", true, os.Getenv)
	if err != nil {
		t.Fatalf("resolveStandardIndex: %v", err)
	}
	if path != "/flag/index" {
		t.Errorf("path = %q, want %q", path, "/flag/index")
	}
	if source != standardIndexFlag {
		t.Errorf("source = %q, want %q", source, standardIndexFlag)
	}
}

// TestStandardIndexResolution_EnvWhenNoFlag verifies the
// ANVIL_REGISTRY_INDEX environment variable is used when no --index flag
// is passed.
func TestStandardIndexResolution_EnvWhenNoFlag(t *testing.T) {
	t.Setenv(envStandardIndex, "/env/index")

	path, source, err := resolveStandardIndex("", false, os.Getenv)
	if err != nil {
		t.Fatalf("resolveStandardIndex: %v", err)
	}
	if path != "/env/index" {
		t.Errorf("path = %q, want %q", path, "/env/index")
	}
	if source != standardIndexEnv {
		t.Errorf("source = %q, want %q", source, standardIndexEnv)
	}
}

// TestStandardIndexResolution_EmptyFlagFallsThrough verifies an
// explicitly passed but empty --index value is treated as unset: the
// resolution falls through to the environment variable.
func TestStandardIndexResolution_EmptyFlagFallsThrough(t *testing.T) {
	t.Setenv(envStandardIndex, "/env/index")

	path, source, err := resolveStandardIndex("", true, os.Getenv)
	if err != nil {
		t.Fatalf("resolveStandardIndex: %v", err)
	}
	if path != "/env/index" {
		t.Errorf("path = %q, want %q", path, "/env/index")
	}
	if source != standardIndexEnv {
		t.Errorf("source = %q, want %q", source, standardIndexEnv)
	}
}

// TestStandardIndexResolution_Default verifies the default index path is
// the Anvil global config directory plus "registry" — the
// config.GlobalConfigDir convention (ADR-005 §7.1) — when neither the
// flag nor the environment variable is set.
func TestStandardIndexResolution_Default(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv(envStandardIndex, "")

	path, source, err := resolveStandardIndex("", false, os.Getenv)
	if err != nil {
		t.Fatalf("resolveStandardIndex: %v", err)
	}
	want := filepath.Join(cfgDir, "anvil", "registry")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if source != standardIndexDefault {
		t.Errorf("source = %q, want %q", source, standardIndexDefault)
	}

	// The default must agree with config.GlobalConfigDir (the repo
	// convention the resolution follows).
	globalDir, err := config.GlobalConfigDir()
	if err != nil {
		t.Fatalf("config.GlobalConfigDir: %v", err)
	}
	if want := filepath.Join(globalDir, "registry"); path != want {
		t.Errorf("path = %q, want the GlobalConfigDir convention %q", path, want)
	}
}

// ── Post-Gate Registry-Only State (TS-017-02-02) ─────────────────────

// TestStandardAdapterPostGate_RegistryOnlyDiscovery verifies the
// post-gate state (TS-017-02-02, ADR-028 §3, §7): after the switch-over
// gate the dual-run window is closed and discovery is registry-only —
// the registry-driven standard discovery ("anvil standard list", "anvil
// standard inspect") works, and the adapter alias surface resolves
// through the registry records (the recorded standard + the executable
// resolution contract), not through the removed closed-set binary scan.
func TestStandardAdapterPostGate_RegistryOnlyDiscovery(t *testing.T) {
	// Adapter side: the standard is RECORDED (registry-driven installed
	// definition) and the executable resolves through the stubbed
	// lookup seam.
	seedInstalledStandard(t, "anvil-standard-laravel", "1.2.3")
	adapterDir := t.TempDir()
	stubAdapterLookup(t, adapterDir)
	writeFakeAdapter(t, adapterDir, "anvil-adapter-laravel",
		`{"capabilities":{"deployment_model":"server"}}`,
		`{"extension":{"framework":"laravel","keys":[]}}`)

	// Registry side: one published standard in a fixture index.
	indexDir := t.TempDir()
	writeStandardIndexDoc(t, indexDir, "anvil-standard-laravel/1.2.3.json",
		standardFixtureDoc("anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished, ""))

	// The adapter alias surface resolves the recorded adapter
	// (registry-driven installed view).
	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "server") {
		t.Errorf("adapter list should resolve the recorded adapter, got:\n%s", stdout)
	}

	_, stdout, stderr, err = executeCommand("adapter", "inspect", "laravel")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter: laravel") {
		t.Errorf("adapter inspect should render the adapter, got:\n%s", stdout)
	}

	// Registry-driven discovery works alongside it.
	_, stdout, stderr, err = executeCommand("standard", "list", "--index", indexDir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "anvil-standard-laravel") || !strings.Contains(stdout, "1.2.3") {
		t.Errorf("standard list should list the published standard, got:\n%s", stdout)
	}

	_, stdout, stderr, err = executeCommand("standard", "inspect", "anvil-standard-laravel", "1.2.3", "--index", indexDir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Standard: anvil-standard-laravel 1.2.3") {
		t.Errorf("standard inspect should render the standard, got:\n%s", stdout)
	}
}
