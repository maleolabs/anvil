// Package output provides redaction helpers for secrets in logs (AC3).
//
// Reference: sto:local-deploy-config AC3, spikes/local-deploy-ssh-transport RedactSecrets,
// spikes/local-deploy-ux RedactSecrets, Artifact local-deploy-transport redaction.
package output

import (
	"os"
	"path/filepath"
	"strings"
)

// RedactSecrets removes secret-like substrings from log lines.
// Never expose DEPLOY_SSH_KEY value, private key content, or full key paths.
//
// Behavior mirrors spike evidence:
//   - if string contains deploy_ssh_key / private / begin open → ***REDACTED***
//   - if contains "/" and key-like component (id_rsa, id_ed25519, .pem, key) → base + " [REDACTED_PATH]"
//   - standalone key filenames even without slash → ***REDACTED_KEY***
//
// This function is safe to call on any log line.
func RedactSecrets(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "deploy_ssh_key") || strings.Contains(lower, "private") || strings.Contains(lower, "begin open") {
		return "***REDACTED***"
	}
	// redact key-like paths: contains id_rsa, id_ed25519, .pem, or generic "key" with slash
	if strings.Contains(s, "/") && (strings.Contains(lower, "key") || strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem")) {
		return filepath.Base(s) + " [REDACTED_PATH]"
	}
	// also redact standalone key filenames even without slash (defense in depth)
	if strings.Contains(lower, "id_rsa") || strings.Contains(lower, "id_ed25519") || strings.Contains(lower, ".pem") {
		return "***REDACTED_KEY***"
	}
	return s
}

// SanitizeLogLine redacts secrets in a log line, including env var values
// currently set (DEPLOY_SSH_KEY, DEPLOY_SERVER_HOST, etc.) and key paths.
//
// It replaces any occurrence of the env var's actual value with a redacted
// placeholder, then applies path-based redaction.

// RedactWithForms redacts dynamic password field values from logs.
func RedactWithForms(line string, passwordFields []string) string {
	hasPassword := false
	for _, f := range passwordFields {
		if f == "" {
			continue
		}
		// if line mentions password field name, redact value pattern field=... or field:...
		if strings.Contains(strings.ToLower(line), strings.ToLower(f)) {
			hasPassword = true
			line = redactPasswordField(line, f)
		}
	}
	if hasPassword {
		return line
	}
	return line
}

func redactPasswordField(line, field string) string {
	// redact patterns like "field: value" or "field=value" or ""field": "value""
	// simplistic: replace field occurrence with field=***REDACTED***
	lowerLine := strings.ToLower(line)
	lowerField := strings.ToLower(field)
	idx := strings.Index(lowerLine, lowerField)
	for idx != -1 {
		// find delimiter after field name
		after := line[idx+len(field):]
		// find separator := or : or =
		sepIdx := -1
		for i, ch := range after {
			if ch == ':' || ch == '=' {
				sepIdx = i
				break
			}
			if i > 10 {
				break
			}
		}
		if sepIdx != -1 {
			start := idx + len(field) + sepIdx + 1
			// skip spaces/quotes
			for start < len(line) && (line[start] == ' ' || line[start] == '"' || line[start] == '\'') {
				start++
			}
			end := start
			for end < len(line) && line[end] != ' ' && line[end] != ',' && line[end] != '"' && line[end] != '\'' && line[end] != '}' {
				end++
			}
			if end > start {
				line = line[:start] + "***REDACTED***" + line[end:]
				lowerLine = strings.ToLower(line)
			}
		}
		// find next occurrence
		next := strings.Index(strings.ToLower(line[idx+len(field):]), lowerField)
		if next == -1 {
			break
		}
		idx = idx + len(field) + next
	}
	return line
}

// SanitizeWithForms extends SanitizeLogLine with dynamic password field redaction.
func SanitizeWithForms(line string, passwordFields []string) string {
	line = SanitizeLogLine(line)
	line = RedactWithForms(line, passwordFields)
	// also redact any remaining password literal patterns "password"
	if strings.Contains(strings.ToLower(line), "password") && !strings.Contains(line, "***REDACTED***") {
		// generic password redaction: mask after colon/equal
		line = redactPasswordField(line, "password")
	}
	return line
}

func SanitizeLogLine(line string) string {
	// redact env var values if they leak verbatim
	for _, env := range []string{"DEPLOY_SSH_KEY", "DEPLOY_SERVER_HOST", "DEPLOY_SERVER_USER"} {
		if val := os.Getenv(env); val != "" && strings.Contains(line, val) {
			line = strings.ReplaceAll(line, val, "***REDACTED_"+env+"***")
		}
	}
	// redact private key content marker
	if strings.Contains(strings.ToLower(line), "-----begin") {
		return "***REDACTED***"
	}
	// redact key path if leaked (file path with .pem / id_rsa etc.)
	if strings.Contains(strings.ToLower(line), ".pem") || strings.Contains(strings.ToLower(line), "id_rsa") || strings.Contains(strings.ToLower(line), "id_ed25519") {
		parts := strings.Fields(line)
		for i, p := range parts {
			lower := strings.ToLower(p)
			if strings.Contains(lower, ".pem") || strings.Contains(p, "id_rsa") || strings.Contains(p, "id_ed25519") {
				parts[i] = filepath.Base(p) + "[REDACTED]"
			}
		}
		line = strings.Join(parts, " ")
	}
	return RedactSecrets(line)
}
