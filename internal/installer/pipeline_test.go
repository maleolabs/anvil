package installer

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
)

func TestInstallerRun_ReuseAndTemplate(t *testing.T) {
	tmpSrc := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpSrc, "main.txt"), []byte("proj content"), 0644)
	outDir := t.TempDir()
	forms := config.InstallerForms{
		"superAdmin": {
			Fields: []config.FormField{
				{Name: "email", Type: "email", Required: true},
				{Name: "password", Type: "password", Required: true},
				{Name: "role", Type: "select", Options: []string{"admin", "user"}},
			},
		},
	}
	// Build first artifact for reuse test
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: tmpSrc,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    "proj",
		ProjectID: "proj",
		Formats:   []string{"tar.gz"},
	})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	// Run installer pipeline with reuse
	res, err := Run(PipelineConfig{
		SourceDir:     tmpSrc,
		OutputDir:     outDir,
		Version:       "1.0.0",
		Source:        "proj",
		ProjectID:     "proj",
		ReuseArtifact: pkgRes.ArtifactPath,
		Forms:         forms,
	})
	if err != nil {
		t.Fatalf("Run reuse: %v", err)
	}
	if !res.FormsEmbedded {
		t.Fatalf("should embed forms")
	}
	if !res.Reused {
		t.Fatalf("should be marked reused")
	}
	// Test template resolution for superAdmin identifier
	tmpl := "{{forms.superAdmin.email}}"
	values := map[string]map[string]string{"superAdmin": {"email": "user@forms.com"}}
	got := ResolveSetupEmail(tmpl, values, "admin@example.com")
	if got != "user@forms.com" {
		t.Fatalf("template resolve got %q", got)
	}
	got = ResolveSetupEmail(tmpl, map[string]map[string]string{}, "admin@example.com")
	if got != "admin@example.com" {
		t.Fatalf("fallback got %q", got)
	}
	// Run without reuse (fresh build)
	outDir2 := t.TempDir()
	res2, err := Run(PipelineConfig{
		SourceDir: tmpSrc,
		OutputDir: outDir2,
		Version:   "1.0.0",
		Source:    "proj",
		ProjectID: "proj",
		Forms:     forms,
	})
	if err != nil {
		t.Fatalf("Run fresh: %v", err)
	}
	if res2.Reused {
		t.Fatalf("fresh should not be reused")
	}
	if !res2.FormsEmbedded {
		t.Fatalf("fresh should embed")
	}
}
