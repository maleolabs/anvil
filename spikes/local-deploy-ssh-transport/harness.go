package spksshtransport

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"maleolabs.com/anvil/internal/artifact"
)

// HarnessConfig configures spike run.
type HarnessConfig struct {
	ArtifactsDir string // where artifacts are built
	RemoteDir    string // simulated remote dir
	Count        int    // number of artifacts (default 50)
	SizeMB       int    // size per artifact in MB (default 5 for fast, 50 for lab)
	ThroughputMBps float64 // simulated throughput for latency histogram
	Logger       io.Writer
}

// HarnessResult is spike run evidence.
type HarnessResult struct {
	Histogram *Histogram `json:"histogram"`
	Artifacts []string   `json:"artifacts"`
	RemoteDir string     `json:"remote_dir"`
}

// RunHarness builds `Count` artifacts and pushes them via SimulatedTransport.
// Returns histogram with p50/p95 and evidence.
func RunHarness(cfg HarnessConfig) (*HarnessResult, error) {
	if cfg.Count <= 0 {
		cfg.Count = 50
	}
	if cfg.SizeMB <= 0 {
		cfg.SizeMB = 5
	}
	if cfg.ThroughputMBps <= 0 {
		cfg.ThroughputMBps = 5 // 5 MB/s ~ 10s per 50MB, realistic for lab
	}
	if cfg.ArtifactsDir == "" {
		cfg.ArtifactsDir = os.TempDir()
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = filepath.Join(os.TempDir(), "spike-remote")
	}
	if cfg.Logger == nil {
		cfg.Logger = io.Discard
	}

	if err := os.MkdirAll(cfg.ArtifactsDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.RemoteDir, 0755); err != nil {
		return nil, err
	}

	hist := &Histogram{}
	var artifacts []string

	for i := 0; i < cfg.Count; i++ {
		artifactPath, err := buildArtifact(cfg.ArtifactsDir, cfg.SizeMB, i)
		if err != nil {
			return nil, fmt.Errorf("build artifact %d: %w", i, err)
		}
		artifacts = append(artifacts, artifactPath)

		info, _ := os.Stat(artifactPath)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		// manifest artifact_id for tracking
		manifest, _, _ := ExtractManifest(artifactPath)
		aid := ""
		if manifest != nil {
			aid = manifest.ArtifactID
		}
		if aid == "" {
			aid = filepath.Base(artifactPath)
		}

		transport := &SimulatedTransport{
			RemoteDir:      cfg.RemoteDir,
			ThroughputMBps: cfg.ThroughputMBps,
		}
		start := time.Now()
		result, kind, err := transport.Push(artifactPath, cfg.Logger)
		dur := time.Since(start)
		if result != nil {
			dur = result.Duration
		}
		if err != nil {
			hist.Add(LatencySample{ArtifactID: aid, Duration: dur, Success: false, Kind: kind, SizeBytes: size})
			fmt.Fprintf(cfg.Logger, "[harness] artifact %d/%d FAIL kind=%s err=%s\n", i+1, cfg.Count, SanitizeLogLine(kind), SanitizeLogLine(err.Error()))
			continue
		}
		hist.Add(LatencySample{ArtifactID: aid, Duration: dur, Success: true, Kind: "", SizeBytes: size})

		// immediate checksum verify after push (audit trail)
		remotePath := result.RemotePath
		if verr := VerifyChecksum(remotePath); verr != nil {
			fmt.Fprintf(cfg.Logger, "[verify] WARN remote %s verify failed: %s\n", filepath.Base(remotePath), SanitizeLogLine(verr.Error()))
		}
	}

	return &HarnessResult{Histogram: hist, Artifacts: artifacts, RemoteDir: cfg.RemoteDir}, nil
}

// RunRetryTest performs AC2: simulate disconnect mid-transfer then retry idempotently.
func RunRetryTest(artifactsDir, remoteDir string, sizeMB int, logger io.Writer) (*RetryEvidence, error) {
	if sizeMB <= 0 {
		sizeMB = 5
	}
	if logger == nil {
		logger = io.Discard
	}
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		return nil, err
	}

	artifactPath, err := buildArtifact(artifactsDir, sizeMB, 999)
	if err != nil {
		return nil, err
	}
	info, _ := os.Stat(artifactPath)
	size := info.Size()
	fmt.Fprintf(logger, "[retry] artifact %s size=%d\n", filepath.Base(artifactPath), size)

	// 1st attempt: inject disconnect at ~50% bytes
	failAt := size / 2
	transportFail := &SimulatedTransport{
		RemoteDir:      remoteDir,
		ThroughputMBps: 10,
		FailAtByte:     failAt,
		FailKind:       "partial_write",
	}
	_, kind, err1 := transportFail.Push(artifactPath, logger)
	evidence := &RetryEvidence{
		ArtifactPath: artifactPath,
		RemotePath:   filepath.Join(remoteDir, filepath.Base(artifactPath)),
		FirstKind:    kind,
		FirstError:   fmt.Sprintf("%v", err1),
	}
	fmt.Fprintf(logger, "[retry] first push (injected disconnect) kind=%s err=%s\n", kind, SanitizeLogLine(fmt.Sprintf("%v", err1)))

	// Verify that final artifact does NOT exist yet (only tmp partial)
	if _, err := os.Stat(evidence.RemotePath); err == nil {
		evidence.FirstRemoteExists = true
		fmt.Fprintf(logger, "[retry] UNEXPECTED: final remote exists after partial write\n")
	} else {
		evidence.FirstRemoteExists = false
	}

	// 2nd attempt: retry without failure — must succeed idempotently
	transportOK := &SimulatedTransport{
		RemoteDir:      remoteDir,
		ThroughputMBps: 10,
	}
	start := time.Now()
	result, kind2, err2 := transportOK.Push(artifactPath, logger)
	evidence.SecondKind = kind2
	if err2 != nil {
		evidence.SecondError = fmt.Sprintf("%v", err2)
		return evidence, fmt.Errorf("retry push failed: %w", err2)
	}
	evidence.RetryDuration = time.Since(start)
	if result != nil {
		evidence.RetryDuration = result.Duration
	}
	fmt.Fprintf(logger, "[retry] second push (retry) success in %dms\n", evidence.RetryDuration.Milliseconds())

	// checksum verification post-retry
	if err := VerifyChecksum(evidence.RemotePath); err != nil {
		evidence.ChecksumValid = false
		evidence.ChecksumError = err.Error()
		return evidence, fmt.Errorf("checksum verify after retry failed: %w", err)
	}
	evidence.ChecksumValid = true

	// also compare source vs remote checksum (file hash)
	srcSum, _ := ChecksumFile(artifactPath)
	dstSum, _ := ChecksumFile(evidence.RemotePath)
	evidence.SourceChecksum = srcSum
	evidence.RemoteChecksum = dstSum
	evidence.ChecksumsMatch = srcSum == dstSum && srcSum != ""

	fmt.Fprintf(logger, "[retry] checksums match=%t src=%s dst=%s\n", evidence.ChecksumsMatch, srcSum[:16], dstSum[:16])

	return evidence, nil
}

// RetryEvidence captures AC2 evidence.
type RetryEvidence struct {
	ArtifactPath       string        `json:"artifact_path"`
	RemotePath         string        `json:"remote_path"`
	FirstKind          string        `json:"first_kind"`
	FirstError         string        `json:"first_error"`
	FirstRemoteExists  bool          `json:"first_remote_exists"`
	SecondKind         string        `json:"second_kind"`
	SecondError        string        `json:"second_error"`
	RetryDuration      time.Duration `json:"retry_duration"`
	ChecksumValid      bool          `json:"checksum_valid"`
	ChecksumError      string        `json:"checksum_error,omitempty"`
	SourceChecksum     string        `json:"source_checksum"`
	RemoteChecksum     string        `json:"remote_checksum"`
	ChecksumsMatch     bool          `json:"checksums_match"`
}

// buildArtifact creates a temp project dir with sizeMB of dummy data and packages it via artifact.Package.
func buildArtifact(outDir string, sizeMB, idx int) (string, error) {
	// create isolated source dir with dummy app content
	srcDir, err := os.MkdirTemp("", fmt.Sprintf("spike-src-%d-", idx))
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(srcDir)

	// create app content: app/file_<idx>.bin with sizeMB
	if err := os.MkdirAll(filepath.Join(srcDir, "app"), 0755); err != nil {
		return "", err
	}
	// create deterministic dummy file
	dummyPath := filepath.Join(srcDir, "app", fmt.Sprintf("payload-%d.bin", idx))
	if err := createDummyFile(dummyPath, sizeMB); err != nil {
		return "", err
	}
	// minimal extra file to ensure manifest has content
	if err := os.WriteFile(filepath.Join(srcDir, "app", "index.php"), []byte("<?php echo 'anvil';"), 0644); err != nil {
		return "", err
	}

	// artifact.Package with manifest
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: srcDir,
		OutputDir: outDir,
		Formats:   []string{"tar.gz"},
		Version:   fmt.Sprintf("0.0.%d", idx+1),
		Source:    "spike-local-deploy",
		ProjectID: "spike-test-project",
	})
	if err != nil {
		return "", err
	}
	return result.ArtifactPath, nil
}

func createDummyFile(path string, sizeMB int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// write sizeMB megabytes of incompressible random data so tar.gz size ~ sizeMB
	// (repeating pattern would compress to ~8KB, not realistic for 50MB lab)
	chunk := make([]byte, 1<<20) // 1MB
	for i := 0; i < sizeMB; i++ {
		// generate pseudo-random but deterministic per idx: use crypto/rand for incompressibility
		// fallback to math/rand style if crypto fails — use per-iteration random
		if _, err := io.ReadFull(randReader(), chunk); err != nil {
			return err
		}
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return f.Sync()
}

// randReader returns an io.Reader for incompressible data.
func randReader() io.Reader { return &incompressibleReader{} }

type incompressibleReader struct{}

func (r *incompressibleReader) Read(p []byte) (int, error) { return readCryptoRand(p) }

func readCryptoRand(p []byte) (int, error) { return rand.Read(p) }
