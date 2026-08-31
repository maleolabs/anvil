package deployment

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// TestTransportAC1_P95 verifies AC1: 50 artifact push p95 <30s via real SSH transport.
// Uses in-process SSH server + histogram, real latency should be <<30s locally.
func TestTransportAC1_P95(t *testing.T) {
	keyPath, pub := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{pub})
	const count = 50
	h := &Histogram{}
	artifactsDir := t.TempDir()
	remoteDir := filepath.Join(t.TempDir(), "remote")
	for i := 0; i < count; i++ {
		artifactPath := buildTestArtifact(t, artifactsDir, 1, i)
		info, _ := os.Stat(artifactPath)
		size := info.Size()
		transport := NewSSHTransport("127.0.0.1", "testuser", keyPath, server.Port(), WithTimeout(5*time.Second), WithRemoteDir(remoteDir))
		target := &testTarget{id: TargetID(fmt.Sprintf("node-%d", i))}
		start := time.Now()
		res, err := transport.Deliver(ArtifactPayload{Path: artifactPath}, target)
		dur := time.Since(start)
		if err != nil {
			h.Add(LatencySample{ArtifactID: fmt.Sprintf("a-%d", i), Duration: dur, Success: false, Kind: string(KindTransferFailed), SizeBytes: size})
			t.Errorf("push %d failed: %v", i, err)
			continue
		}
		if res == nil || !res.Success {
			t.Errorf("push %d no result", i)
		}
		h.Add(LatencySample{ArtifactID: fmt.Sprintf("a-%d", i), Duration: dur, Success: true, SizeBytes: size})
		if v, err := artifact.VerifyArtifact(artifactPath); err != nil || !v.Passed {
			t.Errorf("verify src %d failed: %v", i, err)
		}
		remotePath := filepath.Join(remoteDir, filepath.Base(artifactPath))
		if v, err := artifact.VerifyArtifact(remotePath); err != nil || !v.Passed {
			t.Errorf("verify remote %d failed: %v", i, err)
		}
	}
	if h.SuccessCount() != count {
		t.Fatalf("SuccessCount = %d want %d", h.SuccessCount(), count)
	}
	if p95 := h.P95(); p95 >= 30*time.Second {
		t.Errorf("p95 = %v want <30s (AC1)", p95)
	}
	var buf bytes.Buffer
	if err := h.WriteCSV(&buf); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	csv := buf.String()
	if !strings.Contains(csv, "p95_ms") || !strings.Contains(csv, "p95_within_30s") {
		t.Error("histogram CSV missing p95 fields")
	}
	if !strings.Contains(csv, "true") {
		t.Error("histogram p95_within_30s not true")
	}
	b := h.Buckets()
	sum := 0
	for _, bucket := range b {
		sum += bucket.Count
	}
	if sum != count {
		t.Errorf("buckets sum = %d want %d", sum, count)
	}
	t.Logf("AC1 p95=%v p50=%v buckets=%v", h.P95(), h.P50(), b)
}

// TestTransportAC2_RetryMidTransfer verifies AC2: retry mid-transfer tidak corrupt, checksum PASS,
// and partial tmp does not leave corrupt final artifact (atomic tmp.<rand> -> rename).
func TestTransportAC2_RetryMidTransfer(t *testing.T) {
	keyPath, pub := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{pub})
	artifactsDir := t.TempDir()
	remoteDir := filepath.Join(t.TempDir(), "remote2")
	artifactPath := buildTestArtifact(t, artifactsDir, 2, 99)

	// Inject failure: first SCP fails mid-transfer, Deliver's internal retry should succeed on 2nd attempt.
	server.failNextSCP.Store(true)

	transport := NewSSHTransport("127.0.0.1", "testuser", keyPath, server.Port(), WithTimeout(5*time.Second), WithRemoteDir(remoteDir))
	target := &testTarget{id: TargetID("node-retry")}
	res, err := transport.Deliver(ArtifactPayload{Path: artifactPath}, target)
	if err != nil {
		t.Fatalf("retry Deliver failed (expected success via internal retry): %v", err)
	}
	if res == nil || res.RemotePath == "" {
		t.Fatalf("no remote path")
	}
	remotePath := res.RemotePath
	if _, err := os.Stat(remotePath); err != nil {
		t.Fatalf("remote not exists after retry: %v", err)
	}
	entries, _ := os.ReadDir(remoteDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover tmp file after retry: %s", e.Name())
		}
	}
	srcSum := checksumFileHex(artifactPath)
	dstSum := checksumFileHex(remotePath)
	if srcSum == "" || dstSum == "" {
		t.Fatalf("checksum empty src=%q dst=%q", srcSum, dstSum)
	}
	if srcSum != dstSum {
		t.Fatalf("checksums mismatch after retry src=%s dst=%s", srcSum[:16], dstSum[:16])
	}
	if v, err := artifact.VerifyArtifact(remotePath); err != nil || !v.Passed {
		t.Fatalf("VerifyArtifact remote after retry FAIL: %v %+v", err, v)
	}
}

// TestTransportAC3_KindsAndExitCodes verifies AC3: 6 Kind classification + exit codes + Guidance.
func TestTransportAC3_KindsAndExitCodes(t *testing.T) {
	kinds := AllKinds()
	if len(kinds) < 6 {
		t.Fatalf("AllKinds length = %d want >=6", len(kinds))
	}
	for _, k := range kinds {
		te := &TransportError{Kind: k, Reason: "test"}
		g := te.Guidance()
		if g == "" {
			t.Errorf("Guidance empty for kind %q", k)
		}
		code := te.ExitCode()
		if code != 1 && code != 2 && code != 4 {
			t.Errorf("ExitCode for %q = %d want 1/2/4", k, code)
		}
		switch k {
		case KindConfiguration:
			if code != 2 {
				t.Errorf("KindConfiguration exit code = %d want 2", code)
			}
		case KindAuthenticationFailed, KindHostKeyVerificationFailed, KindPermissionDenied:
			if code != 4 {
				t.Errorf("kind %q exit code = %d want 4", k, code)
			}
		case KindTimeout, KindConnectionRefused, KindUnreachable, KindTransferFailed, KindUnknown:
			if code != 1 {
				t.Errorf("kind %q exit code = %d want 1", k, code)
			}
		}
	}
	if KindTimeout == KindConnectionRefused {
		t.Error("KindTimeout must be distinct from KindConnectionRefused")
	}
}

// TestTransportAC4_NoSecretLeak verifies AC4: KeyPath redacted, no secret leak.
func TestTransportAC4_NoSecretLeak(t *testing.T) {
	keyPath := "/home/user/.ssh/id_ed25519"
	payloadPath := filepath.Join(t.TempDir(), "art.tar.gz")
	_ = os.WriteFile(payloadPath, []byte("dummy"), 0644)
	transport := NewSSHTransport("127.0.0.1", "testuser", keyPath, 22, WithTimeout(2*time.Second))
	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	msg := err.Error()
	if strings.Contains(msg, keyPath) {
		t.Errorf("secret leak: error still contains full KeyPath %q: %q", keyPath, msg)
	}
	if !strings.Contains(msg, "[REDACTED") {
		t.Errorf("error should contain redacted marker, got %q", msg)
	}
	if got := output.RedactSecrets(keyPath); got == keyPath {
		t.Fatalf("RedactSecrets did not redact key path")
	}
	if got := output.RedactSecrets("/tmp/mykey.pem"); !strings.Contains(got, "REDACTED") {
		t.Fatalf("should redact .pem")
	}
	t.Setenv("DEPLOY_SSH_KEY", "/tmp/secret-key")
	line := "DEPLOY_SSH_KEY=/tmp/secret-key"
	sanitized := output.SanitizeLogLine(line)
	if strings.Contains(sanitized, "/tmp/secret-key") {
		t.Errorf("env secret leaked: %q", sanitized)
	}
	line2 := "-----BEGIN OPENSSH PRIVATE KEY-----"
	if got := output.SanitizeLogLine(line2); !strings.Contains(got, "REDACTED") {
		t.Errorf("private key content not redacted: %q", got)
	}
}

func buildTestArtifact(t *testing.T, outDir string, sizeMB, idx int) string {
	t.Helper()
	srcDir, err := os.MkdirTemp("", fmt.Sprintf("art-src-%d-", idx))
	if err != nil {
		t.Fatalf("mk src: %v", err)
	}
	defer os.RemoveAll(srcDir)
	if err := os.MkdirAll(filepath.Join(srcDir, "app"), 0755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	dummyPath := filepath.Join(srcDir, "app", fmt.Sprintf("payload-%d.bin", idx))
	if err := createDummyFile(dummyPath, sizeMB); err != nil {
		t.Fatalf("dummy: %v", err)
	}
	_ = os.WriteFile(filepath.Join(srcDir, "app", "index.php"), []byte("<?php echo 'anvil';"), 0644)
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: srcDir,
		OutputDir: outDir,
		Formats:   []string{"tar.gz"},
		Version:   fmt.Sprintf("0.0.%d", idx+1),
		Source:    "test-transport",
		ProjectID: "test-project",
	})
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	return result.ArtifactPath
}

func createDummyFile(path string, sizeMB int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	chunk := make([]byte, 1<<20)
	for i := 0; i < sizeMB; i++ {
		if _, err := rand.Read(chunk); err != nil {
			return err
		}
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return f.Sync()
}

func checksumFileHex(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
