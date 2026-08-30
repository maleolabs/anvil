package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryIndexConfigured locks the distinction between an
// unconfigured (missing/empty) index directory and a configured one.
// The index layout is <index>/<standard-id>/<version>.json, so the scan
// must find documents inside subdirectories (config bootstrap regression
// guard: the first version only scanned the top level).
func TestRegistryIndexConfigured(t *testing.T) {
	if registryIndexConfigured(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Error("a missing index directory must not be configured")
	}
	if registryIndexConfigured(t.TempDir()) {
		t.Error("an empty index directory must not be configured")
	}

	topLevel := t.TempDir()
	if err := os.WriteFile(filepath.Join(topLevel, "doc.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !registryIndexConfigured(topLevel) {
		t.Error("a top-level .json document must mark the index as configured")
	}

	layout := t.TempDir()
	dir := filepath.Join(layout, "anvil-standard-laravel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if registryIndexConfigured(layout) {
		t.Error("an index with only subdirectories (no documents yet) must not be configured")
	}
	if err := os.WriteFile(filepath.Join(dir, "1.1.1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !registryIndexConfigured(layout) {
		t.Error("a document at <index>/<id>/<version>.json must mark the index as configured")
	}

	// Unreadable entries must not abort the scan.
	broken := t.TempDir()
	sub := filepath.Join(broken, "std")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0); err == nil {
		defer os.Chmod(sub, 0o755)
	}
	if err := os.WriteFile(filepath.Join(broken, "ok.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !registryIndexConfigured(broken) {
		t.Error("an unreadable subdirectory must not hide a configured document")
	}
}

// TestEnsureDefaultConfigLayout verifies the first-run bootstrap: the
// registry index directory and the trust anchors file are created with
// an empty allowlist, and an existing (edited) anchors file is never
// overwritten.
func TestEnsureDefaultConfigLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	if err := ensureDefaultConfigLayout(); err != nil {
		t.Fatalf("ensureDefaultConfigLayout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "anvil", "registry")); err != nil {
		t.Errorf("registry index directory was not created: %v", err)
	}
	anchors := filepath.Join(home, "anvil", "trust-anchors.json")
	raw, err := os.ReadFile(anchors)
	if err != nil {
		t.Fatalf("trust anchors file was not created: %v", err)
	}
	if !strings.Contains(string(raw), `"publishers"`) {
		t.Errorf("trust anchors file must carry an empty publishers allowlist, got: %s", raw)
	}

	// An operator-edited anchors file must survive a second run.
	edited := "{\n  \"publishers\": {\n    \"anvil-standard-laravel\": \"c2VjcmV0\"\n  }\n}\n"
	if err := os.WriteFile(anchors, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultConfigLayout(); err != nil {
		t.Fatalf("second ensureDefaultConfigLayout: %v", err)
	}
	raw, err = os.ReadFile(anchors)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != edited {
		t.Errorf("ensureDefaultConfigLayout must not overwrite an existing anchors file")
	}
}
