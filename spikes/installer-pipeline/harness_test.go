package spkinstallerpipeline

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
)

func TestAC1_InstallerConfigEnvOverrideAndRedaction(t *testing.T) {
	dir := t.TempDir()
	// write anvil.yaml with installer block
	yamlContent := `project:
  name: demo-app
  version: 1.0.0
installer:
  name: DemoApp
  icon: fixtures/app.ico
  artifactSource: .
  osTargets: [windows, linux]
`
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write anvil.yaml: %v", err)
	}
	cfg, _, err := LoadInstallerConfig(dir)
	if err != nil {
		t.Fatalf("LoadInstallerConfig: %v", err)
	}
	if cfg.Name != "DemoApp" {
		t.Fatalf("want DemoApp got %q", cfg.Name)
	}
	if len(cfg.OSTargets) != 2 {
		t.Fatalf("want 2 osTargets got %v", cfg.OSTargets)
	}
	// env override (Execution > Project per ADR-005)
	t.Setenv("ANVIL_CFG_INSTALLER_NAME", "OverrideName")
	cfg2, overrides, err := LoadInstallerConfig(dir)
	if err != nil {
		t.Fatalf("env override load: %v", err)
	}
	if cfg2.Name != "OverrideName" {
		t.Fatalf("env override want OverrideName got %q", cfg2.Name)
	}
	if overrides["ANVIL_CFG_INSTALLER_NAME"] != "OverrideName" {
		t.Fatalf("overrides map missing ANVIL_CFG_INSTALLER_NAME")
	}
	// redaction: ANVIL_SIGNING_KEY must be redacted
	t.Setenv("ANVIL_SIGNING_KEY", "my-secret-key-xyz")
	line := "signing with key my-secret-key-xyz at /home/user/.ssh/id_rsa"
	redacted := RedactInstallerLog(line)
	if strings.Contains(redacted, "my-secret-key-xyz") {
		t.Fatalf("redaction failed, leak: %q", redacted)
	}
	if !strings.Contains(redacted, "REDACTED") {
		t.Fatalf("redaction missing REDACTED marker: %q", redacted)
	}
}

func TestAC1_SanitizeInstallerName(t *testing.T) {
	cases := map[string]string{
		" My App!@# ": "My App",
		"":            "anvil",
		"   ":         "anvil",
		"DemoApp":     "DemoApp",
	}
	for in, want := range cases {
		got := SanitizeInstallerName(in)
		if got != want {
			t.Fatalf("Sanitize %q: want %q got %q", in, want, got)
		}
	}
}

func TestAC2_PipelineUsesArtifactPackageAndOutputsInstaller(t *testing.T) {
	repoRoot := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoRoot, "anvil.yaml"), []byte("project:\n  name: test-app\n  version: 1.0.0\ninstaller:\n  name: TestApp\n  osTargets: [windows]\n"), 0644)
	icfg, _, err := LoadInstallerConfig(repoRoot)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	sourceDir := filepath.Join(repoRoot, "src")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php echo 'hi';"), 0644)
	outDir := filepath.Join(t.TempDir(), "dist-installer")
	var buf bytes.Buffer
	res, err := RunPipeline(PipelineConfig{
		Target:          "windows",
		DryRun:          false,
		JSONOutput:      false,
		OutputDir:       outDir,
		InstallerConfig: icfg,
		ProjectRoot:     repoRoot,
		SourceDir:       sourceDir,
		Version:         "1.0.0",
		Logger:          &buf,
	})
	if err != nil {
		t.Fatalf("RunPipeline windows: %v", err)
	}
	if res.OutputPath == "" {
		t.Fatalf("want installer output path")
	}
	if _, err := os.Stat(res.OutputPath); err != nil {
		t.Fatalf("installer output missing: %v", err)
	}
	if res.ArtifactID == "" || res.Checksum == "" {
		t.Fatalf("missing artifact_id/checksum")
	}
	if res.Verify == nil || !res.Verify.Passed {
		t.Fatalf("verify should pass")
	}
	if !strings.HasSuffix(res.OutputPath, "-Setup.exe") {
		t.Fatalf("windows installer filename want -Setup.exe got %q", res.OutputPath)
	}
	// also test linux target
	icfg.OSTargets = []string{"windows", "linux"}
	var buf2 bytes.Buffer
	outDir2 := filepath.Join(t.TempDir(), "dist-installer2")
	res2, err := RunPipeline(PipelineConfig{
		Target:          "linux",
		DryRun:          false,
		JSONOutput:      false,
		OutputDir:       outDir2,
		InstallerConfig: icfg,
		ProjectRoot:     repoRoot,
		SourceDir:       sourceDir,
		Version:         "1.0.0",
		Logger:          &buf2,
	})
	if err != nil {
		t.Fatalf("RunPipeline linux: %v", err)
	}
	if !strings.HasSuffix(res2.OutputPath, ".run") {
		t.Fatalf("linux installer want .run got %q", res2.OutputPath)
	}
}

func TestAC3_IdempotentAndVerifyBeforeTrust(t *testing.T) {
	repoRoot := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoRoot, "anvil.yaml"), []byte("project:\n  name: idem-app\ninstaller:\n  name: IdemApp\n  osTargets: [windows]\n"), 0644)
	icfg, _, _ := LoadInstallerConfig(repoRoot)
	sourceDir := filepath.Join(repoRoot, "src")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.WriteFile(filepath.Join(sourceDir, "app.txt"), []byte("idem content v1"), 0644)
	outDir := filepath.Join(t.TempDir(), "dist")
	// first build
	res1, err := RunPipeline(PipelineConfig{
		Target: "windows", OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", Logger: ioDiscard(),
	})
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	if res1.Idempotent {
		t.Fatalf("first build should not be idempotent")
	}
	// second build same checksum → idempotent
	res2, err := RunPipeline(PipelineConfig{
		Target: "windows", OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", Logger: ioDiscard(),
	})
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if !res2.Idempotent {
		t.Fatalf("second build should be idempotent")
	}
	if res1.OutputPath != res2.OutputPath {
		t.Fatalf("idempotent path mismatch %q vs %q", res1.OutputPath, res2.OutputPath)
	}
	// tamper test: VerifyArtifact must FAIL on tampered artifact
	tmpDir := t.TempDir()
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir, OutputDir: tmpDir, Formats: []string{"tar.gz"}, Version: "1.0.0", Source: "idem-app", ProjectID: "idem-app",
	})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	tampered := filepath.Join(tmpDir, "tampered.tar.gz")
	if err := TamperArtifact(pkgRes.ArtifactPath, tampered); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	vr, _ := artifact.VerifyArtifact(tampered)
	if vr == nil || vr.Passed {
		t.Fatalf("tampered artifact should FAIL verification")
	}
	if err := verifyBeforeEmbed(tampered); err == nil {
		t.Fatalf("verifyBeforeEmbed should abort on tampered")
	}
}

func TestAC4_DryRunAndEnvelope(t *testing.T) {
	repoRoot := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoRoot, "anvil.yaml"), []byte("project:\n  name: dry-app\ninstaller:\n  name: DryApp\n  osTargets: [windows]\n"), 0644)
	icfg, _, _ := LoadInstallerConfig(repoRoot)
	sourceDir := filepath.Join(repoRoot, "src")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.WriteFile(filepath.Join(sourceDir, "x.txt"), []byte("dry content"), 0644)
	outDir := filepath.Join(t.TempDir(), "dist")

	// dry-run human: no installer build
	var humanBuf bytes.Buffer
	res, err := RunPipeline(PipelineConfig{
		Target: "windows", DryRun: true, JSONOutput: false, OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", Logger: &humanBuf,
	})
	if err != nil {
		t.Fatalf("dry-run human: %v", err)
	}
	if res.OutputPath != "" || res.Installer != nil {
		t.Fatalf("dry-run must not produce installer, got %q", res.OutputPath)
	}
	if !strings.Contains(humanBuf.String(), "Dry-run") {
		t.Fatalf("dry-run human log missing Dry-run marker: %q", humanBuf.String())
	}

	// dry-run json: envelope v1 with dry_run:true
	var jsonBuf bytes.Buffer
	res2, err := RunPipeline(PipelineConfig{
		Target: "windows", DryRun: true, JSONOutput: true, OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", RawWriter: &jsonBuf, Logger: ioDiscard(),
	})
	if err != nil {
		t.Fatalf("dry-run json: %v", err)
	}
	_ = res2
	var env map[string]json.RawMessage
	if err := json.Unmarshal(jsonBuf.Bytes(), &env); err != nil {
		t.Fatalf("dry-run json unmarshal: %v", err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal(env["data"], &data)
	if data["dry_run"] != true {
		t.Fatalf("dry-run json data.dry_run want true got %v", data["dry_run"])
	}
	// error case: missing --target → exit 2 config
	_, err = RunPipeline(PipelineConfig{
		Target: "", OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", Logger: ioDiscard(),
	})
	if err == nil {
		t.Fatalf("missing target should error")
	}
}

func TestAC4_HumanJSONConsistency(t *testing.T) {
	repoRoot := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoRoot, "anvil.yaml"), []byte("project:\n  name: consist-app\ninstaller:\n  name: ConsistApp\n  osTargets: [windows]\n"), 0644)
	icfg, _, _ := LoadInstallerConfig(repoRoot)
	sourceDir := filepath.Join(repoRoot, "src")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.WriteFile(filepath.Join(sourceDir, "a.txt"), []byte("consist"), 0644)
	outDir := filepath.Join(t.TempDir(), "dist")
	var humanBuf bytes.Buffer
	res, err := RunPipeline(PipelineConfig{
		Target: "windows", OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", Logger: &humanBuf,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_ = os.Remove(statePath(outDir))
	var jsonBuf bytes.Buffer
	res2, err := RunPipeline(PipelineConfig{
		Target: "windows", DryRun: false, JSONOutput: true, OutputDir: outDir, InstallerConfig: icfg, ProjectRoot: repoRoot, SourceDir: sourceDir, Version: "1.0.0", RawWriter: &jsonBuf, Logger: ioDiscard(),
	})
	if err != nil {
		t.Fatalf("json build: %v", err)
	}
	_ = res2
	m := res.Manifest
	if m == nil {
		t.Fatalf("manifest nil")
	}
	if err := ValidateHumanJSONConsistency(humanBuf.String(), jsonBuf.String(), m); err != nil {
		t.Fatalf("human/json consistency: %v", err)
	}
	_ = res
}

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }
