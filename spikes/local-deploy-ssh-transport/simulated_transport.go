package spksshtransport

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SimulatedTransport mimics SSH scp via local FS with latency injection
// and failure classification. No real SSH needed for spike evidence.
//
// Idempotency: write to <dst>.tmp.<rand> then atomic rename.
// This guarantees partial writes never corrupt final artifact.
type SimulatedTransport struct {
	RemoteDir string
	// ThroughputMBps simulates network throughput for latency calc (0 = no delay)
	ThroughputMBps float64
	// FailAtByte injects disconnect at byte offset (0 = no failure). Used for AC2.
	FailAtByte int64
	// FailKind classifies injected failure
	FailKind string
	// AuthFail simulates auth failure (AC4)
	AuthFail bool
	// TimeoutFail simulates timeout
	TimeoutFail bool
}

// PushResult mirrors deployment.TransportResult for spike.
type PushResult struct {
	RemotePath string
	Duration   time.Duration
	SizeBytes  int64
}

// Push copies artifact at srcPath to simulated remote (RemoteDir/basename)
// with latency proportional to size/throughput. Returns failure kind on error.
func (t *SimulatedTransport) Push(srcPath string, logger io.Writer) (*PushResult, string, error) {
	if t.AuthFail {
		return nil, "auth_fail", fmt.Errorf("simulated auth fail: ssh-agent key rejected")
	}
	if t.TimeoutFail {
		return nil, "timeout", fmt.Errorf("simulated timeout: dial i/o timeout")
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, "configuration", err
	}
	size := info.Size()

	// sanitize log: never print KeyPath or key material — only basename
	safeSrc := filepath.Base(srcPath)
	safeDst := filepath.Join(t.RemoteDir, safeSrc)
	if logger != nil {
		fmt.Fprintf(logger, "[push] %s -> %s (%d bytes)\n", RedactSecrets(safeSrc), RedactSecrets(safeDst), size)
	}

	// latency injection: size / throughput
	start := time.Now()
	if t.ThroughputMBps > 0 {
		calcLatency := time.Duration(float64(size) / (t.ThroughputMBps * 1024 * 1024) * float64(time.Second))
		// sleep at most 100ms to keep tests fast; real latency is calcLatency for histogram
		sleepFor := calcLatency
		if sleepFor > 100*time.Millisecond {
			sleepFor = 100 * time.Millisecond
		}
		time.Sleep(sleepFor)
	}

	if err := os.MkdirAll(t.RemoteDir, 0755); err != nil {
		return nil, "transfer_failed", err
	}

	randSuffix := randomHex(6)
	tmpPath := safeDst + ".tmp." + randSuffix

	// open src
	src, err := os.Open(srcPath)
	if err != nil {
		return nil, "configuration", err
	}
	defer src.Close()

	dstTmp, err := os.Create(tmpPath)
	if err != nil {
		return nil, "permission_denied", err
	}

	// copy with optional mid-transfer disconnect
	var copied int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			// inject failure at FailAtByte
			if t.FailAtByte > 0 && copied < t.FailAtByte && copied+int64(n) >= t.FailAtByte {
				// write up to FailAtByte then fail
				toWrite := int(t.FailAtByte - copied)
				if toWrite > 0 {
					if _, werr := dstTmp.Write(buf[:toWrite]); werr != nil {
						dstTmp.Close()
						os.Remove(tmpPath)
						return nil, "partial_write", werr
					}
					copied += int64(toWrite)
				}
				dstTmp.Close()
				// leave partial tmp file to prove idempotency: retry must not see corrupt final file
				// do NOT remove tmp — but final dst must not exist yet
				kind := t.FailKind
				if kind == "" {
					kind = "partial_write"
				}
				return nil, kind, fmt.Errorf("simulated disconnect at %d bytes (mid-transfer)", copied)
			}
			if _, werr := dstTmp.Write(buf[:n]); werr != nil {
				dstTmp.Close()
				os.Remove(tmpPath)
				return nil, "transfer_failed", werr
			}
			copied += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dstTmp.Close()
			os.Remove(tmpPath)
			return nil, "transfer_failed", rerr
		}
	}
	// fsync then atomic rename
	if err := dstTmp.Sync(); err != nil {
		dstTmp.Close()
		os.Remove(tmpPath)
		return nil, "transfer_failed", err
	}
	dstTmp.Close()

	if err := os.Rename(tmpPath, safeDst); err != nil {
		os.Remove(tmpPath)
		return nil, "transfer_failed", err
	}

	duration := time.Since(start)
	// For evidence realism, if throughput was set, use calc latency as duration when calc > measured
	if t.ThroughputMBps > 0 {
		calcLatency := time.Duration(float64(size) / (t.ThroughputMBps * 1024 * 1024) * float64(time.Second))
		if calcLatency > duration {
			duration = calcLatency
		}
	}

	if logger != nil {
		fmt.Fprintf(logger, "[push] success %s (%d bytes) in %dms\n", RedactSecrets(filepath.Base(safeDst)), size, duration.Milliseconds())
	}

	return &PushResult{RemotePath: safeDst, Duration: duration, SizeBytes: size}, "", nil
}

// RedactSecrets removes secret-like substrings from log lines.
// Never expose KeyPath full value, private key content, or DEPLOY_SSH_KEY.
func RedactSecrets(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "deploy_ssh_key") || strings.Contains(lower, "private") || strings.Contains(lower, "begin open") {
		return "***REDACTED***"
	}
	// redact key-like paths: contains id_rsa, id_ed25519, .pem, or generic "key" with slash
	if strings.Contains(s, "/") && (strings.Contains(lower, "key") || strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem")) {
		return filepath.Base(s) + " [REDACTED_PATH]"
	}
	// also redact standalone key filenames even without slash (defense in depth for direct key path values)
	if strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem") {
		return "***REDACTED_KEY***"
	}
	return s
}

// SanitizeLogLine redacts secrets in a log line.
func SanitizeLogLine(line string) string {
	// redact env var values
	for _, env := range []string{"DEPLOY_SSH_KEY", "DEPLOY_SERVER_HOST", "DEPLOY_SERVER_USER"} {
		if val := os.Getenv(env); val != "" && strings.Contains(line, val) {
			line = strings.ReplaceAll(line, val, "***REDACTED_"+env+"***")
		}
	}
	// redact key path if leaked
	if strings.Contains(strings.ToLower(line), ".pem") || strings.Contains(strings.ToLower(line), "id_rsa") || strings.Contains(strings.ToLower(line), "id_ed25519") {
		parts := strings.Fields(line)
		for i, p := range parts {
			if strings.Contains(strings.ToLower(p), ".pem") || strings.Contains(p, "id_rsa") || strings.Contains(p, "id_ed25519") {
				parts[i] = filepath.Base(p) + "[REDACTED]"
			}
		}
		line = strings.Join(parts, " ")
	}
	return RedactSecrets(line)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
