package cmd

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

//go:embed embedded_trust_anchors.json
var embeddedTrustAnchors []byte

var standardTrustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage trust anchors for standard verification",
	Long: `Manage the trust anchors allowlist for standard verification (ADR-022).

Subcommands:
  add    Add a publisher anchor (with consent fingerprint prompt)
  list   List configured anchors`,
}

var standardTrustAddCmd = &cobra.Command{
	Use:   "add <standard-id> <base64-key>",
	Short: "Add a trust anchor",
	Long: `Add a publisher's Ed25519 public key to the trust anchors allowlist.

The key is strict base64 of 32-byte Ed25519 public key. Fail-closed: without
anchored key install fails. Unknown publisher prompts fingerprint consent.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE:         runStandardTrustAdd,
	Example:      "  anvil standard trust add anvil-standard-laravel <base64-key>",
}

func init() {
	standardTrustCmd.AddCommand(standardTrustAddCmd)
	standardCmd.AddCommand(standardTrustCmd)
}

func runStandardTrustAdd(cmd *cobra.Command, args []string) error {
	id := args[0]
	b64 := args[1]
	// Validate base64 strict and 32 bytes
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		return ReportError(cmd, &output.AppError{
			Message:    "invalid trust anchor: not strict base64 32-byte Ed25519 key",
			Reason:     fmt.Sprintf("decode error: %v", err),
			Resolution: "Provide strict base64 of 32-byte Ed25519 public key",
		})
	}
	// Show fingerprint consent in TTY
	fp := fmt.Sprintf("%x", raw[:8])
	if isTerminal(cmd) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Fingerprint %s for %s — add? [y/N]: ", fp, id)
		var resp string
		fmt.Fscanln(cmd.InOrStdin(), &resp)
		if strings.ToLower(strings.TrimSpace(resp)) != "y" && strings.ToLower(strings.TrimSpace(resp)) != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
			return nil
		}
	}
	path, err := registry.ResolveTrustAnchorsPath("", os.Getenv)
	if err != nil {
		return ReportError(cmd, &output.AppError{Message: "could not resolve trust anchors path", Err: err})
	}
	anchors, err := registry.LoadTrustAnchors(path)
	if err != nil && !os.IsNotExist(err) {
		// Try embedded fallback for init
		anchors = &registry.TrustAnchors{}
	}
	if anchors == nil {
		anchors = &registry.TrustAnchors{}
	}
	// Ensure dir
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Load existing JSON or create
	data, err := os.ReadFile(path)
	var m map[string]interface{}
	if err == nil {
		json.Unmarshal(data, &m)
	}
	if m == nil {
		m = map[string]interface{}{"publishers": map[string]string{}}
	}
	pubs, _ := m["publishers"].(map[string]interface{})
	if pubs == nil {
		pubs = map[string]interface{}{}
		m["publishers"] = pubs
	}
	// Also handle map[string]string shape
	if _, ok := m["publishers"].(map[string]string); ok {
		// keep as is
	}
	// Write with simple approach: use registry format
	// For minimal, just write publishers map
	out := map[string]map[string]string{"publishers": {id: b64}}
	if existing, err := os.ReadFile(path); err == nil {
		var existingMap map[string]map[string]string
		if json.Unmarshal(existing, &existingMap) == nil {
			if existingMap["publishers"] == nil {
				existingMap["publishers"] = map[string]string{}
			}
			existingMap["publishers"][id] = b64
			out = existingMap
		}
	}
	// If file was empty or had different shape, use out
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(styleFor(cmd).W, "Added trust anchor for %s (fingerprint %s) at %s\n", id, fp, path)
	return nil
}
