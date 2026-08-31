package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	spike "maleolabs.com/anvil/spikes/local-deploy-ssh-transport"
)

func main() {
	count := flag.Int("count", 50, "number of artifacts to push")
	sizeMB := flag.Int("size-mb", 5, "size per artifact in MB (use 50 for lab)")
	throughput := flag.Float64("throughput", 5, "simulated throughput MB/s")
	realSSH := flag.Bool("real-ssh", false, "use real SSH transport if env set (requires DEPLOY_* env)")
	outDir := flag.String("out", "", "evidence output dir (default spikes/local-deploy-ssh-transport/evidence)")
	flag.Parse()

	if *realSSH {
		fmt.Fprintln(os.Stderr, "real-ssh mode requested but spike harness currently uses simulated transport; set DEPLOY_* and implement real transport call if needed (see internal/deployment.SSHTransport)")
	}

	wtEvidence := *outDir
	if wtEvidence == "" {
		// try to locate evidence dir relative to cwd
		wtEvidence = "spikes/local-deploy-ssh-transport/evidence"
		if _, err := os.Stat(wtEvidence); os.IsNotExist(err) {
			// fallback to temp
			wtEvidence = filepath.Join(os.TempDir(), "spike-evidence")
		}
	}
	if err := os.MkdirAll(wtEvidence, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir evidence: %v\n", err)
		os.Exit(1)
	}

	artifactsDir, _ := os.MkdirTemp("", "spike-artifacts-")
	remoteDir, _ := os.MkdirTemp("", "spike-remote-")
	defer os.RemoveAll(artifactsDir)
	// keep remote for inspection; caller can remove

	histLogPath := filepath.Join(wtEvidence, "retry.log")
	retryLogFile, _ := os.Create(histLogPath)
	defer retryLogFile.Close()

	fmt.Printf("=== Spike 1: SSH Transport Latency & Retry ===\n")
	fmt.Printf("count=%d size=%dMB throughput=%.1f MB/s\n", *count, *sizeMB, *throughput)
	fmt.Printf("artifactsDir=%s remoteDir=%s evidence=%s\n", artifactsDir, remoteDir, wtEvidence)

	// Run main harness
	cfg := spike.HarnessConfig{
		ArtifactsDir:   artifactsDir,
		RemoteDir:      remoteDir,
		Count:          *count,
		SizeMB:         *sizeMB,
		ThroughputMBps: *throughput,
		Logger:         os.Stdout,
	}
	start := time.Now()
	result, err := spike.RunHarness(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Harness done in %s: %d/%d success, p50=%dms p95=%dms p95<30s=%t\n",
		time.Since(start).Truncate(time.Millisecond),
		result.Histogram.SuccessCount(), len(result.Histogram.Samples),
		result.Histogram.P50().Milliseconds(), result.Histogram.P95().Milliseconds(),
		result.Histogram.P95() < 30*time.Second,
	)

	// Write histogram.csv
	csvPath := filepath.Join(wtEvidence, "histogram.csv")
	if err := result.Histogram.WriteCSVFile(csvPath); err != nil {
		fmt.Fprintf(os.Stderr, "write csv: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Histogram CSV: %s\n", csvPath)
	for _, b := range result.Histogram.Buckets() {
		fmt.Printf("  bucket %s: %d\n", b.Label, b.Count)
	}
	for k, v := range result.Histogram.FailureClassification() {
		fmt.Printf("  fail %s: %d\n", k, v)
	}

	// AC2 retry test
	fmt.Printf("\n=== Retry Idempotency (AC2) ===\n")
	retryArtifactsDir, _ := os.MkdirTemp("", "spike-retry-artifacts-")
	retryRemoteDir := filepath.Join(remoteDir, "retry-test")
	defer os.RemoveAll(retryArtifactsDir)

	retryEvidence, err := spike.RunRetryTest(retryArtifactsDir, retryRemoteDir, *sizeMB, retryLogFile)
	if err != nil {
		fmt.Printf("Retry test failed: %v\n", err)
		if retryEvidence != nil {
			fmt.Printf("Evidence: firstKind=%s checksumValid=%t match=%t\n", retryEvidence.FirstKind, retryEvidence.ChecksumValid, retryEvidence.ChecksumsMatch)
		}
		os.Exit(1)
	}
	// also tee retry evidence to stdout
	fmt.Printf("Retry success: first=%s (partial) second=%s retry=%dms checksumValid=%t match=%t\n",
		retryEvidence.FirstKind, retryEvidence.SecondKind, retryEvidence.RetryDuration.Milliseconds(), retryEvidence.ChecksumValid, retryEvidence.ChecksumsMatch)
	fmt.Fprintf(retryLogFile, "\n[summary] firstKind=%s secondKind=%s retryMs=%d checksumValid=%t checksumsMatch=%t src=%s dst=%s\n",
		retryEvidence.FirstKind, retryEvidence.SecondKind, retryEvidence.RetryDuration.Milliseconds(), retryEvidence.ChecksumValid, retryEvidence.ChecksumsMatch, retryEvidence.SourceChecksum, retryEvidence.RemoteChecksum)
	fmt.Printf("Retry log: %s\n", histLogPath)

	// also write checksum verify result to evidence
	verifyPath := filepath.Join(wtEvidence, "checksum_verify.log")
	_ = os.WriteFile(verifyPath, []byte(fmt.Sprintf("checksum_valid=%t source=%s remote=%s match=%t\n", retryEvidence.ChecksumValid, retryEvidence.SourceChecksum, retryEvidence.RemoteChecksum, retryEvidence.ChecksumsMatch)), 0644)

	fmt.Printf("\n=== AC3 Secret Redaction Check ===\n")
	secretLine := fmt.Sprintf("connect with DEPLOY_SSH_KEY=%s host=%s", os.Getenv("DEPLOY_SSH_KEY"), os.Getenv("DEPLOY_SERVER_HOST"))
	sanitized := spike.SanitizeLogLine(secretLine)
	fmt.Printf("Original (redacted in log): %s\nSanitized: %s\n", spike.RedactSecrets(secretLine), sanitized)
	if sanitized == secretLine && secretLine != "" && os.Getenv("DEPLOY_SSH_KEY") != "" {
		fmt.Fprintln(os.Stderr, "WARN: secret not redacted")
	}

	fmt.Printf("\n=== Evidence Gate ===\n")
	fmt.Printf("Files:\n - %s\n - %s\n - %s\n", csvPath, histLogPath, verifyPath)
	fmt.Printf("\nHarness completed. Use `go test ./spikes/local-deploy-ssh-transport -v` for unit gates.\n")
}
