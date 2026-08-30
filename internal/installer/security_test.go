package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
)

func buildTestArtifact(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "app"), 0755)
	_ = os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php echo 'hello';"), 0644)
	_ = os.WriteFile(filepath.Join(src, "app", "hello.txt"), []byte("hello security"), 0644)
	tmp := t.TempDir()
	pkg, err := artifact.Package(artifact.PackageOptions{
		SourceDir: src,
		OutputDir: tmp,
		Formats:   []string{"tar.gz"},
		Version:   "1.0.0",
		Source:    "test-security",
		ProjectID: "test-security",
	})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	return pkg.ArtifactPath
}

func tamperArtifact(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(b) > 200 {
		b[200] ^= 0xFF
	} else if len(b) > 0 {
		b[0] ^= 0xFF
	}
	return os.WriteFile(dst, b, 0644)
}

func TestVerifyBeforeExtract_Pass(t *testing.T) {
	artifactPath := buildTestArtifact(t)
	if err := VerifyBeforeExtract(artifactPath); err != nil {
		t.Fatalf("trusted artifact should PASS, got %v", err)
	}
}

func TestVerifyBeforeExtract_TamperFailClosedWithGuidance(t *testing.T) {
	artifactPath := buildTestArtifact(t)
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := tamperArtifact(artifactPath, tampered); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	err := VerifyBeforeExtract(tampered)
	if err == nil {
		t.Fatalf("tampered should FAIL closed")
	}
	msg := err.Error()
	if !strings.Contains(msg, "abort before extract") {
		t.Fatalf("guidance missing abort before extract: %q", msg)
	}
	if !strings.Contains(msg, "--dry-run") {
		t.Fatalf("guidance must mention --dry-run: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "checksum") {
		t.Fatalf("guidance must mention checksum: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "guidance") {
		t.Fatalf("guidance missing: %q", msg)
	}
}

func TestVerifyBeforeExtract_MissingFile(t *testing.T) {
	err := VerifyBeforeExtract("/tmp/nonexistent-artifact-xyz.tar.gz")
	if err == nil {
		t.Fatalf("missing file should FAIL")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "guidance") {
		t.Fatalf("missing file guidance absent: %v", err)
	}
}

func TestVerifyInstallerPayloadIntegrity_Pass(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "installer.run")
	_ = os.WriteFile(payload, []byte("#!/bin/sh\necho hello"), 0755)
	sha, err := FileSHA256(payload)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	binding := `{"installer_sha256":"` + sha + `"}`
	_ = os.WriteFile(payload+".checksum.json", []byte(binding), 0644)
	if err := VerifyInstallerPayloadIntegrity(payload); err != nil {
		t.Fatalf("payload integrity should PASS: %v", err)
	}
}

func TestVerifyInstallerPayloadIntegrity_Tamper(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload.tar.gz")
	// Use real artifact as payload for realism
	src := buildTestArtifact(t)
	b, _ := os.ReadFile(src)
	_ = os.WriteFile(payload, b, 0644)
	sha, _ := FileSHA256(payload)
	_ = os.WriteFile(payload+".checksum.json", []byte(`{"sha256":"`+sha+`"}`), 0644)
	// tamper payload
	f, _ := os.OpenFile(payload, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.Write([]byte("tamper"))
	f.Close()
	err := VerifyInstallerPayloadIntegrity(payload)
	if err == nil {
		t.Fatalf("tampered payload should FAIL")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "checksum") {
		t.Fatalf("expected checksum guidance: %v", err)
	}
}

func TestVerifyInstallerPayloadIntegrity_MissingChecksum(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload2.run")
	_ = os.WriteFile(payload, []byte("content"), 0644)
	// no .checksum.json
	err := VerifyInstallerPayloadIntegrity(payload)
	if err == nil {
		t.Fatalf("missing checksum should FAIL closed")
	}
	if !strings.Contains(err.Error(), ".checksum.json") {
		t.Fatalf("should mention .checksum.json: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "guidance") {
		t.Fatalf("missing checksum guidance absent: %v", err)
	}
}

func TestSafeExtractPath_Traversal(t *testing.T) {
	dest := t.TempDir()
	cases := []struct {
		name       string
		entry      string
		shouldFail bool
	}{
		{"traversal parent", "../../etc/passwd", true},
		{"absolute", "/etc/passwd", true},
		{"empty", "", true},
		{"clean", "app/hello.txt", false},
		{"nested traversal", "app/../../etc/shadow", true},
		{"dot", ".", true},
	}
	for _, c := range cases {
		_, err := SafeExtractPath(dest, c.entry)
		if c.shouldFail && err == nil {
			t.Fatalf("case %q should FAIL", c.name)
		}
		if !c.shouldFail && err != nil {
			t.Fatalf("case %q should PASS, got %v", c.name, err)
		}
	}
	// also ensure resolved path stays within dest for edge case dest prefix trick
	dest2 := filepath.Join(t.TempDir(), "foo")
	_ = os.MkdirAll(dest2, 0755)
	// trying to escape via prefix "/tmp/foo" vs "/tmp/foobar"
	outside := filepath.Join(filepath.Dir(dest2), "foobar", "evil.txt")
	_ = outside // placeholder to ensure prefix logic tested via SafeExtractPath
	// Use entry that would resolve to outside via traversal
	if _, err := SafeExtractPath(dest2, "../foobar/evil.txt"); err == nil {
		t.Fatalf("prefix bypass should fail")
	}
}

func TestRedactInstallerLog_PasswordAndEnv(t *testing.T) {
	t.Setenv("DB_PASSWORD", "s3cr3t-db-pass-42")
	t.Setenv("ANVIL_SIGNING_KEY", "signing-key-xyz-999")
	t.Setenv("DATABASE_URL", "postgres://anvil:s3cr3t-db-pass-42@db.example.com/anvil")
	cases := []struct {
		in      string
		mustNot string
	}{
		{"DB_PASSWORD=s3cr3t-db-pass-42 collected", "s3cr3t-db-pass-42"},
		{"ANVIL_SIGNING_KEY=signing-key-xyz-999 loaded", "signing-key-xyz-999"},
		{"connecting to postgres://anvil:s3cr3t-db-pass-42@db.example.com/anvil", "s3cr3t-db-pass-42"},
		{"private key BEGIN OPENSSH PRIVATE KEY leaked", "BEGIN"},
		{"/home/user/.ssh/id_rsa leaked", "id_rsa"},
	}
	for _, c := range cases {
		got := RedactInstallerLog(c.in)
		if strings.Contains(got, c.mustNot) {
			t.Fatalf("secret leaked for %q -> %q", c.in, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("expected REDACTED for %q got %q", c.in, got)
		}
	}
	clean := RedactInstallerLog("normal log line without secrets")
	if strings.Contains(clean, "REDACTED") {
		t.Fatalf("clean line over-redacted: %q", clean)
	}
}

func TestRedactInstallerLog_WithFormsFields(t *testing.T) {
	t.Setenv("DB_PASSWORD", "s3cr3t")
	line := `forms field db_password: s3cr3t and password=supersecret value`
	got := RedactInstallerLogWithForms(line, []string{"db_password", "password"})
	if strings.Contains(got, "supersecret") {
		t.Fatalf("password value leaked with forms: %q", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("expected REDACTED with forms fields: %q", got)
	}
}

func TestVerifyOffline_NoNetwork(t *testing.T) {
	// VerifyOffline is fs-only, no net/http, should behave same as VerifyBeforeExtract
	artifactPath := buildTestArtifact(t)
	if err := VerifyOffline(artifactPath); err != nil {
		t.Fatalf("offline trusted should PASS: %v", err)
	}
	tampered := filepath.Join(t.TempDir(), "tampered-offline.tar.gz")
	_ = tamperArtifact(artifactPath, tampered)
	if err := VerifyOffline(tampered); err == nil {
		t.Fatalf("offline tampered should still FAIL")
	}
	// Ensure env registry unset doesn't affect
	t.Setenv("ANVIL_REGISTRY", "")
	if err := VerifyOffline(artifactPath); err != nil {
		t.Fatalf("offline with unset registry should still PASS: %v", err)
	}
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	_ = os.WriteFile(p, []byte("hello"), 0644)
	sha1, err := FileSHA256(p)
	if err != nil {
		t.Fatalf("FileSHA256 err: %v", err)
	}
	if len(sha1) != 64 {
		t.Fatalf("sha256 hex len !=64 got %q", sha1)
	}
	sha2, _ := FileSHA256(p)
	if sha1 != sha2 {
		t.Fatalf("deterministic sha mismatch")
	}
	// tamper
	_ = os.WriteFile(p, []byte("hello!"), 0644)
	sha3, _ := FileSHA256(p)
	if sha1 == sha3 {
		t.Fatalf("tamper should change sha")
	}
}

func TestSafeExtractPath_CleanResolvesInside(t *testing.T) {
	dest := t.TempDir()
	out, err := SafeExtractPath(dest, "a/b/c.txt")
	if err != nil {
		t.Fatalf("clean should pass: %v", err)
	}
	if !strings.HasPrefix(out, dest) {
		t.Fatalf("resolved %q not inside %q", out, dest)
	}
}
