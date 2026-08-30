package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGUIFormat(t *testing.T) {
	for _, test := range []struct {
		target, requested, want string
	}{
		{"windows", "auto", "msi"},
		{"windows", "msi", "msi"},
		{"windows", "nsis", "nsis"},
		{"linux", "rpm", "rpm"},
		{"linux", "deb", "deb"},
		{"linux", "appimage", "appimage"},
	} {
		got, err := resolveGUIFormat(test.target, test.requested)
		if err != nil || got != test.want {
			t.Fatalf("resolveGUIFormat(%q, %q) = %q, %v; want %q", test.target, test.requested, got, err, test.want)
		}
	}
	if _, err := resolveGUIFormat("windows", "rpm"); err == nil {
		t.Fatal("Windows rpm should fail")
	}
	if _, err := resolveGUIFormat("linux", "msi"); err == nil {
		t.Fatal("Linux msi should fail")
	}
}

func TestEnsureGUIScaffoldRepairsPartialProject(t *testing.T) {
	root := t.TempDir()
	confDir := filepath.Join(root, "installer-gui", "src-tauri")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "tauri.conf.json"), []byte(`{"bundle":{"identifier":"com.old.app"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureGUIScaffold(root, "My App", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "package.json", "vite.config.js", "src/main.js", "src/App.svelte", "src-tauri/src/main.rs"} {
		if _, err := os.Stat(filepath.Join(root, "installer-gui", name)); err != nil {
			t.Fatalf("missing repaired GUI file %s: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(confDir, "tauri.conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conf map[string]interface{}
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	if _, ok := conf["identifier"]; !ok {
		t.Fatal("missing top-level identifier")
	}
	if bundle, ok := conf["bundle"].(map[string]interface{}); ok {
		if _, bad := bundle["identifier"]; bad {
			t.Fatal("legacy bundle.identifier remains")
		}
	}
}

func TestNormalizeGUIPackageRepairsLegacySvelteVersions(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "package.json")
	legacy := `{"dependencies":{"@tauri-apps/api":"^2"},"devDependencies":{"@sveltejs/vite-plugin-svelte":"^4","svelte":"^4"}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeGUIPackage(base); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	dev := pkg["devDependencies"].(map[string]interface{})
	if dev["@sveltejs/vite-plugin-svelte"] != "^3" || dev["svelte"] != "^4" {
		t.Fatalf("incompatible versions remain: %v", dev)
	}
	if pkg["scripts"].(map[string]interface{})["build"] != "vite build" {
		t.Fatal("build script missing")
	}
}

func TestNormalizeGUIConfigRepairsFrontendDist(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "installer-gui", "src-tauri")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := `{"identifier":"com.example.app","build":{"frontendDist":"../src/dist"},"bundle":{"identifier":"legacy"}}`
	if err := os.WriteFile(filepath.Join(path, "tauri.conf.json"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeGUIConfig(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "tauri.conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["build"].(map[string]interface{})["frontendDist"] != "../dist" {
		t.Fatal("frontendDist not normalized")
	}
}

func TestNormalizeGUICargoRepairsTauriV1Feature(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "installer-gui", "src-tauri")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	cargo := "[dependencies]\ntauri = { version = \"2\", features = [\"shell-open\"] }\n[features]\ndefault = []\nsha2 = \"0.10\"\n"
	if err := os.WriteFile(filepath.Join(path, "Cargo.toml"), []byte(cargo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeGUICargo(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "shell-open") || !strings.Contains(text, "sha2 =") {
		t.Fatalf("Cargo.toml not normalized: %s", text)
	}
	if strings.Index(text, "sha2 =") > strings.Index(text, "[features]") {
		t.Fatal("dependencies must appear before features section")
	}
}
