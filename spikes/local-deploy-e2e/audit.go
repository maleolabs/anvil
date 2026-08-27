package spklocaldeploye2e

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditEntry is the audit trail record for a deployment operation.
// AC5: who deployed (SSH user + timestamp) logged di server.
type AuditEntry struct {
	Timestamp  string `json:"timestamp"`   // RFC3339
	User       string `json:"user"`        // SSH user / deployer
	Action     string `json:"action"`      // install | activate | rollback | verify
	ProjectID  string `json:"project_id"`  // project scope
	ArtifactID string `json:"artifact_id"` // content-derived identity
	Version    string `json:"version,omitempty"`
	ReleaseID  string `json:"release_id,omitempty"`
	RemotePath string `json:"remote_path,omitempty"`
	Details    string `json:"details,omitempty"`
}

// AuditLogger appends audit entries to a JSON-lines file in the server install root.
// It is safe for concurrent use within the harness.
type AuditLogger struct {
	mu   sync.Mutex
	path string // <installRoot>/audit.log
	w    io.Writer
}

// NewAuditLogger creates an AuditLogger writing to <installRoot>/audit.log and to w (optional secondary logger).
func NewAuditLogger(installRoot string, w io.Writer) (*AuditLogger, error) {
	if installRoot == "" {
		return nil, fmt.Errorf("installRoot required for audit logger")
	}
	// Ensure dir exists; the caller may create later but we attempt here.
	_ = os.MkdirAll(installRoot, 0755)
	path := filepath.Join(installRoot, "audit.log")
	return &AuditLogger{path: path, w: w}, nil
}

// Log appends an entry with the current time and deployer user.
// It fsyncs after each write for durability (audit trail must survive crash).
func (a *AuditLogger) Log(user, action, projectID, artifactID, version, releaseID, remotePath, details string) (*AuditEntry, error) {
	entry := &AuditEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		User:       RedactUser(user),
		Action:     action,
		ProjectID:  projectID,
		ArtifactID: artifactID,
		Version:    version,
		ReleaseID:  releaseID,
		RemotePath: remotePath,
		Details:    details,
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

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
	if a.w != nil {
		fmt.Fprintf(a.w, "[audit] %s user=%s action=%s artifact=%s release=%s\n", entry.Timestamp, entry.User, entry.Action, entry.ArtifactID, entry.ReleaseID)
	}
	return entry, nil
}

// Entries reads all audit entries from the log file.
func (a *AuditLogger) Entries() ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []AuditEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip corrupt line, keep harness resilient
		}
		out = append(out, e)
	}
	return out, nil
}

// AuditLogPath returns the filesystem path of the audit log.
func (a *AuditLogger) AuditLogPath() string { return a.path }

// RedactUser redacts secret-like substrings from user values (never log private key).
func RedactUser(user string) string {
	lower := strings.ToLower(user)
	if strings.Contains(lower, "private") || strings.Contains(lower, "deploy_ssh_key") {
		return "***REDACTED***"
	}
	return user
}
