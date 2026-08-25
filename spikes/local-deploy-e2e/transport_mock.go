package spklocaldeploye2e

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/deployment"
)

// LocalFSTransport mocks SSH scp via local FS, implementing deployment.Transport.
// It copies ArtifactPayload.Path to RemoteDir/basename atomically (tmp → rename)
// and injects optional latency/failures for evidence classification.
//
// Reuses the atomicity pattern from spike1 SimulatedTransport (no real SSH needed).
type LocalFSTransport struct {
	RemoteDir      string  // simulated remote staging dir (e.g. /tmp/remote-staging)
	ThroughputMBps float64 // simulated throughput for latency calc (0 = no delay)
	FailAtByte     int64   // inject disconnect at byte offset (0 = no failure)
	FailKind       deployment.TransportErrorKind
	AuthFail       bool
	TimeoutFail    bool
}

// Deliver implements deployment.Transport.Deliver.
func (t *LocalFSTransport) Deliver(payload deployment.ArtifactPayload, target deployment.Target) (*deployment.TransportResult, error) {
	if t.AuthFail {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: "simulated auth fail: ssh-agent key rejected",
			Recoverable: false, Kind: deployment.KindAuthenticationFailed,
		}
	}
	if t.TimeoutFail {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: "simulated timeout: dial i/o timeout",
			Recoverable: true, Kind: deployment.KindConnectionRefused,
		}
	}
	if payload.Path == "" {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: "payload path empty",
			Recoverable: false, Kind: deployment.KindConfiguration,
		}
	}
	info, err := os.Stat(payload.Path)
	if err != nil {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("access payload: %v", err),
			Recoverable: false, Kind: deployment.KindConfiguration,
		}
	}
	size := info.Size()
	safeBase := filepath.Base(payload.Path)
	dst := filepath.Join(t.RemoteDir, safeBase)

	start := time.Now()
	if t.ThroughputMBps > 0 {
		calc := time.Duration(float64(size) / (t.ThroughputMBps * 1024 * 1024) * float64(time.Second))
		sleepFor := calc
		if sleepFor > 100*time.Millisecond {
			sleepFor = 100 * time.Millisecond
		}
		time.Sleep(sleepFor)
	}
	if err := os.MkdirAll(t.RemoteDir, 0755); err != nil {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("mkdir remote: %v", err),
			Recoverable: false, Kind: deployment.KindPermissionDenied,
		}
	}
	randSuffix := randomHex(6)
	tmpPath := dst + ".tmp." + randSuffix

	src, err := os.Open(payload.Path)
	if err != nil {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("open payload: %v", err),
			Recoverable: false, Kind: deployment.KindConfiguration,
		}
	}
	defer src.Close()
	dstTmp, err := os.Create(tmpPath)
	if err != nil {
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("create tmp: %v", err),
			Recoverable: false, Kind: deployment.KindPermissionDenied,
		}
	}
	var copied int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if t.FailAtByte > 0 && copied < t.FailAtByte && copied+int64(n) >= t.FailAtByte {
				toWrite := int(t.FailAtByte - copied)
				if toWrite > 0 {
					if _, werr := dstTmp.Write(buf[:toWrite]); werr != nil {
						dstTmp.Close()
						os.Remove(tmpPath)
						return nil, &deployment.TransportError{
							TargetID: target.ID(), Reason: fmt.Sprintf("write tmp: %v", werr),
							Recoverable: true, Kind: deployment.KindTransferFailed,
						}
					}
					copied += int64(toWrite)
				}
				dstTmp.Close()
				kind := t.FailKind
				if kind == "" {
					kind = deployment.KindTransferFailed
				}
				return nil, &deployment.TransportError{
					TargetID: target.ID(), Reason: fmt.Sprintf("simulated disconnect at %d bytes (mid-transfer)", copied),
					Recoverable: true, Kind: kind,
				}
			}
			if _, werr := dstTmp.Write(buf[:n]); werr != nil {
				dstTmp.Close()
				os.Remove(tmpPath)
				return nil, &deployment.TransportError{
					TargetID: target.ID(), Reason: fmt.Sprintf("write tmp: %v", werr),
					Recoverable: true, Kind: deployment.KindTransferFailed,
				}
			}
			copied += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dstTmp.Close()
			os.Remove(tmpPath)
			return nil, &deployment.TransportError{
				TargetID: target.ID(), Reason: fmt.Sprintf("read payload: %v", rerr),
				Recoverable: true, Kind: deployment.KindTransferFailed,
			}
		}
	}
	if err := dstTmp.Sync(); err != nil {
		dstTmp.Close()
		os.Remove(tmpPath)
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("fsync tmp: %v", err),
			Recoverable: true, Kind: deployment.KindTransferFailed,
		}
	}
	dstTmp.Close()
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return nil, &deployment.TransportError{
			TargetID: target.ID(), Reason: fmt.Sprintf("rename tmp: %v", err),
			Recoverable: false, Kind: deployment.KindTransferFailed,
		}
	}
	dur := time.Since(start)
	if t.ThroughputMBps > 0 {
		calc := time.Duration(float64(size) / (t.ThroughputMBps * 1024 * 1024) * float64(time.Second))
		if calc > dur {
			dur = calc
		}
	}
	_ = dur // TransportResult does not carry duration; harness histogram can use it externally

	return &deployment.TransportResult{Success: true, TargetID: target.ID(), RemotePath: dst}, nil
}

// RedactSecrets scrubs secret-like substrings from log values (mirror spike1).
func RedactSecrets(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "deploy_ssh_key") || strings.Contains(lower, "private") || strings.Contains(lower, "begin open") {
		return "***REDACTED***"
	}
	if strings.Contains(s, "/") && (strings.Contains(lower, "key") || strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem")) {
		return filepath.Base(s) + " [REDACTED_PATH]"
	}
	if strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem") {
		return "***REDACTED_KEY***"
	}
	return s
}

// SanitizeLogLine redacts env var values from a log line.
func SanitizeLogLine(line string) string {
	for _, env := range []string{"DEPLOY_SSH_KEY", "DEPLOY_SERVER_HOST", "DEPLOY_SERVER_USER"} {
		if val := os.Getenv(env); val != "" && strings.Contains(line, val) {
			line = strings.ReplaceAll(line, val, "***REDACTED_"+env+"***")
		}
	}
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
