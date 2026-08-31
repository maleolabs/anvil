package spksshtransport

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/artifact"
)

func TestHistogram_Percentiles(t *testing.T) {
	h := &Histogram{}
	for i := 1; i <= 100; i++ {
		h.Add(LatencySample{ArtifactID: "a", Duration: time.Duration(i) * time.Millisecond, Success: true})
	}
	if got := h.P50(); got != 50*time.Millisecond {
		t.Errorf("P50 = %v want 50ms", got)
	}
	if got := h.P95(); got != 95*time.Millisecond {
		t.Errorf("P95 = %v want 95ms", got)
	}
	if got := h.P95(); got >= 30*time.Second {
		t.Errorf("P95 unexpectedly >=30s: %v", got)
	}
	if c := h.SuccessCount(); c != 100 {
		t.Errorf("SuccessCount = %d want 100", c)
	}
}

func TestHistogram_FailureClassification(t *testing.T) {
	h := &Histogram{}
	h.Add(LatencySample{ArtifactID: "a1", Duration: 10 * time.Millisecond, Success: true})
	h.Add(LatencySample{ArtifactID: "a2", Duration: 5 * time.Millisecond, Success: false, Kind: "auth_fail"})
	h.Add(LatencySample{ArtifactID: "a3", Duration: 7 * time.Millisecond, Success: false, Kind: "timeout"})
	h.Add(LatencySample{ArtifactID: "a4", Duration: 8 * time.Millisecond, Success: false, Kind: "auth_fail"})
	m := h.FailureClassification()
	if m["auth_fail"] != 2 {
		t.Errorf("auth_fail count = %d want 2", m["auth_fail"])
	}
	if m["timeout"] != 1 {
		t.Errorf("timeout count = %d want 1", m["timeout"])
	}
}

func TestHistogram_Buckets(t *testing.T) {
	h := &Histogram{}
	h.Add(LatencySample{Duration: 50 * time.Millisecond, Success: true})
	h.Add(LatencySample{Duration: 200 * time.Millisecond, Success: true})
	h.Add(LatencySample{Duration: 2 * time.Second, Success: true})
	buckets := h.Buckets()
	if buckets[0].Count != 1 {
		t.Errorf("bucket 0-100ms count = %d want 1", buckets[0].Count)
	}
	if buckets[1].Count != 1 {
		t.Errorf("bucket 100-500ms count = %d want 1", buckets[1].Count)
	}
	if buckets[3].Count != 1 {
		t.Errorf("bucket 1s-5s count = %d want 1", buckets[3].Count)
	}
}

func TestHistogram_WriteCSV(t *testing.T) {
	h := &Histogram{}
	h.Add(LatencySample{ArtifactID: "abc", Duration: 123 * time.Millisecond, Success: true, SizeBytes: 1024})
	var buf bytes.Buffer
	if err := h.WriteCSV(&buf); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "artifact_id") {
		t.Error("CSV missing header")
	}
	if !strings.Contains(out, "abc") {
		t.Error("CSV missing sample")
	}
	if !strings.Contains(out, "p95_ms") {
		t.Error("CSV missing p95")
	}
}

func TestSimulatedTransport_Success(t *testing.T) {
	srcDir := t.TempDir()
	artifactPath, err := buildArtifact(srcDir, 1, 0)
	if err != nil {
		t.Fatalf("buildArtifact: %v", err)
	}
	remoteDir := filepath.Join(t.TempDir(), "remote")
	tr := &SimulatedTransport{RemoteDir: remoteDir, ThroughputMBps: 10}
	var log bytes.Buffer
	res, kind, err := tr.Push(artifactPath, &log)
	if err != nil {
		t.Fatalf("Push failed: %v kind=%s", err, kind)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if _, err := os.Stat(res.RemotePath); err != nil {
		t.Fatalf("remote not exists: %v", err)
	}
	if err := VerifyChecksum(res.RemotePath); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if log.String() == "" {
		t.Error("expected log output")
	}
}

func TestSimulatedTransport_IdempotentRetry(t *testing.T) {
	srcDir := t.TempDir()
	remoteDir := filepath.Join(t.TempDir(), "remote")
	evidence, err := RunRetryTest(srcDir, remoteDir, 1, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("RunRetryTest: %v", err)
	}
	if !evidence.ChecksumValid {
		t.Error("checksum not valid after retry")
	}
	if !evidence.ChecksumsMatch {
		t.Error("checksums mismatch after retry")
	}
	if evidence.FirstRemoteExists {
		t.Error("final remote should not exist after partial write")
	}
	if evidence.FirstKind != "partial_write" {
		t.Errorf("FirstKind = %q want partial_write", evidence.FirstKind)
	}
}

func TestSimulatedTransport_AuthFailClassification(t *testing.T) {
	srcDir := t.TempDir()
	artifactPath, err := buildArtifact(srcDir, 1, 1)
	if err != nil {
		t.Fatalf("buildArtifact: %v", err)
	}
	remoteDir := filepath.Join(t.TempDir(), "remote")
	tr := &SimulatedTransport{RemoteDir: remoteDir, AuthFail: true}
	_, kind, err := tr.Push(artifactPath, nil)
	if err == nil {
		t.Fatal("expected auth fail")
	}
	if kind != "auth_fail" {
		t.Errorf("kind = %q want auth_fail", kind)
	}
}

func TestSimulatedTransport_TimeoutClassification(t *testing.T) {
	srcDir := t.TempDir()
	artifactPath, err := buildArtifact(srcDir, 1, 2)
	if err != nil {
		t.Fatalf("buildArtifact: %v", err)
	}
	tr := &SimulatedTransport{RemoteDir: filepath.Join(t.TempDir(), "r"), TimeoutFail: true}
	_, kind, err := tr.Push(artifactPath, nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if kind != "timeout" {
		t.Errorf("kind = %q want timeout", kind)
	}
}

func TestRedactSecrets_NoKeyPathLeak(t *testing.T) {
	// simulate log that might contain key path
	keyPath := "/home/user/.ssh/id_ed25519"
	logLine := "using key " + keyPath + " for host 1.2.3.4"
	sanitized := SanitizeLogLine(logLine)
	if strings.Contains(sanitized, keyPath) {
		t.Errorf("sanitized log still contains full key path: %q", sanitized)
	}
	// raw redaction
	if got := RedactSecrets(keyPath); got == keyPath {
		t.Errorf("RedactSecrets did not redact key path")
	}
	// env var redaction
	t.Setenv("DEPLOY_SSH_KEY", "/tmp/secret-key")
	line := "DEPLOY_SSH_KEY=/tmp/secret-key"
	sanitized2 := SanitizeLogLine(line)
	if strings.Contains(sanitized2, "/tmp/secret-key") {
		t.Errorf("env secret leaked: %q", sanitized2)
	}
}

func TestArtifactManifestCompliance(t *testing.T) {
	srcDir := t.TempDir()
	artifactPath, err := buildArtifact(srcDir, 1, 99)
	if err != nil {
		t.Fatalf("buildArtifact: %v", err)
	}
	m, _, err := ExtractManifest(artifactPath)
	if err != nil {
		t.Fatalf("ExtractManifest: %v", err)
	}
	if m.ArtifactID == "" {
		t.Error("manifest artifact_id empty")
	}
	if m.Checksum == "" {
		t.Error("manifest checksum empty")
	}
	if m.ChecksumType == "" {
		t.Error("manifest checksum_type empty")
	}
	if m.ProjectID == "" {
		t.Error("manifest project_id empty")
	}
	// verify whole artifact
	if err := VerifyChecksum(artifactPath); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	// also test artifact.VerifyArtifact directly
	res, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if !res.Passed {
		t.Fatalf("VerifyArtifact not passed: %+v", res.Checks)
	}
}

func TestHarness_AC1_P95Within30s(t *testing.T) {
	// fast small harness: 5 artifacts, 1MB each, high throughput => p95 <30s
	artifactsDir := filepath.Join(t.TempDir(), "arts")
	remoteDir := filepath.Join(t.TempDir(), "remote")
	res, err := RunHarness(HarnessConfig{
		ArtifactsDir:   artifactsDir,
		RemoteDir:      remoteDir,
		Count:          5,
		SizeMB:         1,
		ThroughputMBps: 20,
		Logger:         &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunHarness: %v", err)
	}
	if res.Histogram.SuccessCount() != 5 {
		t.Errorf("SuccessCount = %d want 5", res.Histogram.SuccessCount())
	}
	if p95 := res.Histogram.P95(); p95 >= 30*time.Second {
		t.Errorf("p95 = %v want <30s", p95)
	}
}
