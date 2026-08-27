package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
)

func TestAC1_TamperRejectedWithGuidance(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "app"), 0755)
	_ = os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php echo 'ac1';"), 0644)
	_ = os.WriteFile(filepath.Join(src, "app", "hello.txt"), []byte("hello ac1"), 0644)
	tmp := t.TempDir()
	pkg, err := artifact.Package(artifact.PackageOptions{SourceDir: src, OutputDir: tmp, Formats: []string{"tar.gz"}, Version: "1.0.0", Source: "test-ac1", ProjectID: "test-ac1"})
	if err != nil { t.Fatalf("package: %v", err) }
	tampered := filepath.Join(tmp, "tampered.tar.gz")
	if err := TamperArtifact(pkg.ArtifactPath, tampered); err != nil { t.Fatalf("tamper: %v", err) }
	vr, _ := artifact.VerifyArtifact(tampered)
	if vr == nil || vr.Passed { t.Fatalf("tampered should FAIL") }
	_, err = VerifyBeforeExtract(tampered)
	if err == nil { t.Fatalf("VerifyBeforeExtract should reject tampered") }
	if !strings.Contains(strings.ToLower(err.Error()), "guidance") { t.Fatalf("guidance missing: %v", err) }
	if !strings.Contains(err.Error(), "abort before extract") { t.Fatalf("actionable guidance missing abort before extract") }
	// trusted must pass
	if _, err := VerifyBeforeExtract(pkg.ArtifactPath); err != nil { t.Fatalf("trusted should pass: %v", err) }
}

func TestAC2_PayloadIntegrityAndRepack(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "app"), 0755)
	_ = os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php echo 'ac2';"), 0644)
	tmp := t.TempDir()
	pkg, _ := artifact.Package(artifact.PackageOptions{SourceDir: src, OutputDir: tmp, Formats: []string{"tar.gz"}, Version: "1.0.0", Source: "test-ac2", ProjectID: "test-ac2"})
	installer := filepath.Join(tmp, "App.run")
	_ = os.WriteFile(installer, []byte("#!/bin/sh\n# wrapper\n"), 0755)
	pass, _, err := VerifyInstallerPayloadIntegrity(installer, pkg.ArtifactPath)
	if err != nil { t.Fatalf("payload integrity err: %v", err) }
	if !pass { t.Fatalf("trusted payload should PASS") }
	// repack with tampered payload must FAIL
	tampered := filepath.Join(tmp, "tampered2.tar.gz")
	_ = TamperArtifact(pkg.ArtifactPath, tampered)
	pass2, _, _ := VerifyInstallerPayloadIntegrity(installer, tampered)
	if pass2 { t.Fatalf("repacked tampered payload should be detected (FAIL)") }
}

func TestAC3_RedactionNoLeak(t *testing.T) {
	os.Setenv("DB_PASSWORD", "s3cr3t-db-pass-42")
	os.Setenv("ANVIL_SIGNING_KEY", "signing-key-xyz-999")
	defer func(){ os.Unsetenv("DB_PASSWORD"); os.Unsetenv("ANVIL_SIGNING_KEY")}()
	cases := []struct{in string; mustNotContain string}{
		{"DB_PASSWORD=s3cr3t-db-pass-42", "s3cr3t-db-pass-42"},
		{"ANVIL_SIGNING_KEY=signing-key-xyz-999", "signing-key-xyz-999"},
		{"postgres://anvil:s3cr3t-db-pass-42@db", "s3cr3t-db-pass-42"},
		{"/home/user/.ssh/id_rsa leaked", "id_rsa"},
	}
	for _, c := range cases {
		got := RedactInstallerLog(c.in)
		if strings.Contains(got, c.mustNotContain) { t.Fatalf("secret leaked for %q -> %q", c.in, got) }
		if !strings.Contains(got, "REDACTED") { t.Fatalf("expected REDACTED for %q got %q", c.in, got) }
	}
	// clean line should not be over-redacted
	clean := RedactInstallerLog("normal log line")
	if strings.Contains(clean, "REDACTED") { t.Fatalf("clean line over-redacted: %q", clean) }
}

func TestAC4_OfflineVerification(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "app"), 0755)
	_ = os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php echo 'ac4';"), 0644)
	tmp := t.TempDir()
	pkg, _ := artifact.Package(artifact.PackageOptions{SourceDir: src, OutputDir: tmp, Formats: []string{"tar.gz"}, Version: "1.0.0", Source: "test-ac4", ProjectID: "test-ac4"})
	prev := os.Getenv("ANVIL_REGISTRY")
	os.Unsetenv("ANVIL_REGISTRY")
	defer func(){ if prev!="" { os.Setenv("ANVIL_REGISTRY", prev)} }()
	vr, err := VerifyOffline(pkg.ArtifactPath)
	if err != nil || vr == nil || !vr.Passed { t.Fatalf("offline trusted should PASS: vr=%v err=%v", vr, err) }
	tampered := filepath.Join(tmp, "tampered-offline.tar.gz")
	_ = TamperArtifact(pkg.ArtifactPath, tampered)
	if _, err := VerifyOffline(tampered); err == nil { t.Fatalf("offline tampered should still FAIL") }
}

func TestSigningFeasibilityDoc(t *testing.T) {
	doc := SigningFeasibility()
	if !strings.Contains(doc, "Windows") || !strings.Contains(doc, "Linux") { t.Fatalf("signing doc incomplete") }
	if !strings.Contains(doc, "osslsigncode") && !strings.Contains(doc, "signtool") { t.Fatalf("signing doc missing tool") }
}
