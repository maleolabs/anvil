// Package deployment — HMAC hash-chain audit JSON-lines 0600, redacted, SSH principal binding.
//
// Reference: anvil-cli/sto:local-deploy-guard AC2, adr:local-deploy-transport redaction, spike audit.go
// Audit file: <installRoot>/audit.log (JSON-lines), mode 0600, O_APPEND|O_SYNC, HMAC chain, redacted.
// DeployUser is bound to SSH principal (creds.User / fingerprint) — not spoofable string.
package deployment

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"maleolabs.com/anvil/internal/filelock"
	"maleolabs.com/anvil/internal/output"
)

// AuditEntry is JSON-lines record with HMAC chain (prev_hash -> hash).
type AuditEntry struct {
	Timestamp  string `json:"timestamp"` // RFC3339Nano UTC
	Env        string `json:"env"`       // target env
	User       string `json:"user"`      // SSH principal (bound, not string spoofable)
	Action     string `json:"action"`    // deploy|install|activate|rollback|guard
	ProjectID  string `json:"project_id,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Version    string `json:"version,omitempty"`
	ReleaseID  string `json:"release_id,omitempty"`
	RemotePath string `json:"remote_path,omitempty"` // redacted
	Details    string `json:"details,omitempty"`     // redacted
	PrevHash   string `json:"prev_hash,omitempty"`   // hex of previous entry hash
	Hash       string `json:"hash,omitempty"`        // HMAC sha256 of this entry (excluding hash field)
	Result     string `json:"result,omitempty"`      // allow|deny
}

// AuditLogger appends HMAC-chained entries to JSON-lines file with mode 0600.
type AuditLogger struct {
	mu      sync.Mutex
	path    string // <installRoot>/audit.log
	keyPath string // <installRoot>/audit.hmac.key
	key     []byte
}

// NewAuditLogger creates logger writing to <installRoot>/audit.log, key at <installRoot>/audit.hmac.key (0600).
// If installRoot empty => error. Key is loaded from keyPath or env ANVIL_AUDIT_HMAC_KEY, or generated (32 bytes) and persisted 0600.
func NewAuditLogger(installRoot string) (*AuditLogger, error) {
	if strings.TrimSpace(installRoot) == "" {
		return nil, fmt.Errorf("installRoot required for audit logger")
	}
	if err := os.MkdirAll(installRoot, 0750); err != nil {
		return nil, fmt.Errorf("mkdir audit dir: %w", err)
	}
	path := filepath.Join(installRoot, "audit.log")
	keyPath := filepath.Join(installRoot, "audit.hmac.key")

	// Resolve HMAC key: env override preferred, then file, then generate
	var key []byte
	if envKey := strings.TrimSpace(os.Getenv("ANVIL_AUDIT_HMAC_KEY")); envKey != "" {
		// Accept hex or raw
		if b, err := hex.DecodeString(envKey); err == nil && len(b) >= 16 {
			key = b
		} else {
			// hash raw env value to 32 bytes for stable length
			h := sha256.Sum256([]byte(envKey))
			key = h[:]
		}
	} else if data, err := os.ReadFile(keyPath); err == nil && len(data) >= 16 {
		// key file may be hex
		trim := strings.TrimSpace(string(data))
		if b, err := hex.DecodeString(trim); err == nil && len(b) >= 16 {
			key = b
		} else {
			// raw bytes fallback
			key = []byte(trim)
		}
	}
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate audit hmac key: %w", err)
		}
		hexKey := hex.EncodeToString(key)
		if err := os.WriteFile(keyPath, []byte(hexKey+"\n"), 0600); err != nil {
			return nil, fmt.Errorf("write audit hmac key: %w", err)
		}
		_ = os.Chmod(keyPath, 0600)
	} else {
		// Ensure key file exists with 0600 for persistence
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			hexKey := hex.EncodeToString(key)
			_ = os.WriteFile(keyPath, []byte(hexKey+"\n"), 0600)
			_ = os.Chmod(keyPath, 0600)
		} else {
			_ = os.Chmod(keyPath, 0600)
		}
	}

	// Ensure audit log exists with 0600
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
		_ = f.Chmod(0600)
		f.Close()
	}

	l := &AuditLogger{path: path, keyPath: keyPath, key: key}
	// self-heal perms
	_ = os.Chmod(path, 0600)
	_ = os.Chmod(keyPath, 0600)
	return l, nil
}

// Log appends entry with current time, SSH principal binding, redacted fields, HMAC chain, fsync.
// It holds the process-local mutex and an exclusive file lock (flock) to prevent
// cross-process hash-chain branching (concurrent Log from different processes).
func (a *AuditLogger) Log(env, action, projectID, artifactID, version, releaseID, remotePath, details string, principal string, result string) (*AuditEntry, error) {
	// Bind user to SSH principal — never use spoofable string directly
	user := strings.TrimSpace(principal)
	if user == "" {
		user = "unknown"
	}
	// Redact sensitive details via output.SanitizeLogLine / RedactSecrets
	remotePath = output.SanitizeLogLine(redactPath(remotePath))
	details = output.SanitizeLogLine(details)
	details = output.RedactSecrets(details)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Open (or create) with O_APPEND|O_SYNC, then acquire exclusive flock for the
	// whole read-last-hash + append critical section.
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_RDWR|os.O_SYNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	_ = f.Chmod(0600)
	if err := filelock.Lock(f, true, false); err != nil {
		return nil, fmt.Errorf("flock audit log: %w", err)
	}
	defer filelock.Unlock(f)

	prevHash := lastHashFromFileLocked(f)

	entry := &AuditEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Env:        strings.TrimSpace(env),
		User:       output.SanitizeLogLine(user),
		Action:     strings.TrimSpace(action),
		ProjectID:  strings.TrimSpace(projectID),
		ArtifactID: strings.TrimSpace(artifactID),
		Version:    strings.TrimSpace(version),
		ReleaseID:  strings.TrimSpace(releaseID),
		RemotePath: remotePath,
		Details:    details,
		PrevHash:   prevHash,
		Result:     strings.TrimSpace(result),
	}
	// Compute HMAC over entry without Hash field (canonical JSON)
	entry.Hash = computeHMAC(a.key, entry)

	line, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		return nil, fmt.Errorf("write audit log: %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("fsync audit log: %w", err)
	}
	return entry, nil
}

// lastHash reads last entry's hash for chaining; empty if no prior.
func (a *AuditLogger) lastHash() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastHashLocked()
}

// lastHashLocked is the lock-free variant; caller must hold a.mu.
// It reads without cross-process lock — callers that need flock should use
// lastHashFromFileLocked. Kept for backward compat (in-process mu only).
func (a *AuditLogger) lastHashLocked() string {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil && e.Hash != "" {
			return e.Hash
		}
	}
	return ""
}

// lastHashFromFileLocked reads last hash from an already flock-locked file descriptor.
func lastHashFromFileLocked(f *os.File) string {
	if _, err := f.Seek(0, 0); err != nil {
		return ""
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		// fallback: read via fd
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, rerr := f.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if rerr != nil {
				break
			}
		}
		data = buf
	}
	if len(data) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil && e.Hash != "" {
			return e.Hash
		}
	}
	return ""
}

// Entries reads all entries (for verification / tests) with shared flock.
// It returns an error on the first corrupt (non-JSON) line instead of silently
// skipping — tamper must not be hidden. Callers that historically relied on
// continue will now surface corruption via VerifyChain or explicit error.
func (a *AuditLogger) Entries() ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Use shared flock if file exists to avoid racing with concurrent Log's exclusive lock.
	f, err := os.OpenFile(a.path, os.O_RDONLY, 0600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	_ = filelock.Lock(f, false, false)
	defer filelock.Unlock(f)

	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []AuditEntry
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("corrupt audit entry at line %d: %w", idx+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// VerifyChain verifies HMAC hash-chain integrity; returns first mismatch index or -1 if ok.
func (a *AuditLogger) VerifyChain() (int, error) {
	entries, err := a.Entries()
	if err != nil {
		return -1, err
	}
	var prev string
	for i, e := range entries {
		expectedPrev := prev
		if e.PrevHash != expectedPrev {
			return i, fmt.Errorf("chain broken at %d: prev_hash %q != expected %q", i, e.PrevHash, expectedPrev)
		}
		// Recompute hash without Hash field
		copyE := e
		copyE.Hash = ""
		got := computeHMAC(a.key, &copyE)
		if got != e.Hash {
			return i, fmt.Errorf("hmac mismatch at %d: got %q want %q", i, got, e.Hash)
		}
		prev = e.Hash
	}
	return -1, nil
}

// AuditLogPath returns filesystem path.
func (a *AuditLogger) AuditLogPath() string { return a.path }

// AuditKeyPath returns key file path (for permission checks).
func (a *AuditLogger) AuditKeyPath() string { return a.keyPath }

func computeHMAC(key []byte, e *AuditEntry) string {
	// Canonical payload: JSON of entry without Hash, stable field order via struct
	tmp := *e
	tmp.Hash = ""
	payload, _ := json.Marshal(tmp)
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func redactPath(p string) string {
	if p == "" {
		return ""
	}
	// Use RedactSecrets for key-like paths
	return output.RedactSecrets(p)
}
