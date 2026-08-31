package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// RedactedLogKeys returns keys for redacted logging — password field names are dynamic per forms.
func RedactedLogKeys(forms config.InstallerForms, collected map[string]map[string]string) []string {
	// Collect top-level form names + indicate password fields redacted
	keys := []string{}
	for formName := range collected {
		keys = append(keys, formName)
	}
	// Append password field names for redaction audit without values
	for _, f := range config.PasswordFieldNames(forms) {
		keys = append(keys, "redacted:"+f)
	}
	return keys
}

// VerifyGUIBeforeExtract reuses VerifyBeforeExtract for Tauri IPC — fail-closed, offline fs-only.
// Returns exit-code style error: caller should exit 1 on tamper.
func VerifyGUIBeforeExtract(artifactPath string) error {
	if err := VerifyBeforeExtract(artifactPath); err != nil {
		return fmt.Errorf("verification gate FAIL -- exit 1: %w", err)
	}
	return nil
}

// ExtractGUIPayload wraps SafeExtractPath validation for GUI extraction.
func ExtractGUIPayload(artifactPath, destDir string) error {
	if strings.TrimSpace(artifactPath) == "" || strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("artifactPath or destDir empty")
	}
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("artifact not found %q: %w", artifactPath, err)
	}
	// Validate destDir with SafeExtractPath semantics (no traversal)
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}
	// Ensure traversal check via SafeExtractPath with dummy entry
	if _, err := SafeExtractPath(absDest, "dummy"); err != nil && strings.Contains(err.Error(), "traversal") {
		return err
	}
	return nil
}

// WriteFormsJSON0600ForGUI writes forms payload to INSTALLER_FORMS_JSON with 0600, offline.
// Redacted log: only keys, never values, password fields dynamic.
func WriteFormsJSON0600ForGUI(forms config.InstallerForms, collected map[string]map[string]string) (string, error) {
	path := os.Getenv("INSTALLER_FORMS_JSON")
	if strings.TrimSpace(path) == "" {
		path = "/tmp/installer-forms.json"
	}
	data, err := json.MarshalIndent(collected, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	_ = os.Chmod(path, 0o600)
	// Redacted log
	keys := RedactedLogKeys(forms, collected)
	fmt.Fprintf(os.Stderr, "[installer-gui] forms collected (redacted) keys=%v gui=true\n", keys)
	return path, nil
}
