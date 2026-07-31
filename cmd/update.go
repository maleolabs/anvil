// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010, ADR-012
package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	// updateRepo is the public repository where releases are published.
	updateRepo = "maleolabs/anvil"

	// updateAPITemplate is the GitHub API URL for the latest release.
	updateAPITemplate = "https://api.github.com/repos/%s/releases/latest"

	// updateDownloadTemplate is the download URL for a release binary.
	updateDownloadTemplate = "https://github.com/%s/releases/latest/download/%s"
)

// updateCmd represents the 'anvil update' command.
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Anvil CLI to the latest version",
	Long: `Check for the latest Anvil release and update the CLI binary.

Downloads the latest version from GitHub Releases, verifies the
binary checksum, and replaces the current executable.

Uses the public mirror repository (maleolabs/anvil) as the update
source. Requires write permission to the installation directory.

Examples:
  anvil update                          # check and apply update
  anvil update --check                  # only check, don't apply
  anvil update --force                  # update even if same version`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

// ghRelease is a partial GitHub Release API response.
type ghRelease struct {
	TagName string `json:"tag_name"`
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().Bool("check", false, "Only check for updates, don't apply")
	updateCmd.Flags().Bool("force", false, "Update even if already at the latest version")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	force, _ := cmd.Flags().GetBool("force")

	currentVersion := strings.TrimPrefix(CliVersion, "v")

	// ── Fetch latest release from GitHub API ──
	latestVersion, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	latestVersion = strings.TrimPrefix(latestVersion, "v")

	// ── Compare versions ──
	if currentVersion == latestVersion && !force {
		fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (v%s).\n", currentVersion)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Current version: v%s\n", currentVersion)
	fmt.Fprintf(cmd.OutOrStdout(), "Latest version:  v%s\n", latestVersion)

	if checkOnly {
		if currentVersion != latestVersion {
			fmt.Fprintln(cmd.OutOrStdout(), "An update is available. Run 'anvil update' to apply.")
		}
		return nil
	}

	// ── Apply update ──
	fmt.Fprintln(cmd.OutOrStdout(), "Downloading...")

	// Determine binary name for current platform
	binaryName := fmt.Sprintf("anvil-%s-%s", runtime.GOOS, runtime.GOARCH)

	downloadURL := fmt.Sprintf(updateDownloadTemplate, updateRepo, binaryName)

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "anvil-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download binary
	resp, err := http.Get(downloadURL) //nolint:gosec
	if err != nil {
		return fmt.Errorf("download %s: %w", binaryName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: server returned HTTP %d", binaryName, resp.StatusCode)
	}

	// Write to temp file while computing sha256
	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download %s: %w", binaryName, err)
	}
	tmpFile.Close()

	downloadedHash := hex.EncodeToString(hash.Sum(nil))

	// Verify checksum
	fmt.Fprintln(cmd.OutOrStdout(), "Verifying checksum...")
	if err := verifyChecksum(binaryName, downloadedHash); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Make temp file executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("set executable permission: %w", err)
	}

	// Replace the current binary
	fmt.Fprintf(cmd.OutOrStdout(), "Installing to %s\n", execPath)
	if err := replaceBinary(tmpPath, execPath); err != nil {
		return fmt.Errorf("install update: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully updated to v%s.\n", latestVersion)
	return nil
}

// fetchLatestVersion gets the latest release tag from the GitHub API.
func fetchLatestVersion() (string, error) {
	apiURL := fmt.Sprintf(updateAPITemplate, updateRepo)

	//nolint:gosec
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse GitHub API response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no releases found in %s", updateRepo)
	}

	return release.TagName, nil
}

// verifyChecksum downloads SHA256SUMS.txt and verifies the binary hash.
func verifyChecksum(binaryName, downloadedHash string) error {
	checksumsURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/SHA256SUMS.txt", updateRepo)

	//nolint:gosec
	resp, err := http.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums not available (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	// Parse SHA256SUMS format: "<hash>  <filename>"
	// The filename may be "anvil-linux-amd64" or "binaries/anvil-linux-amd64".
	expectedHash := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// SHA256SUMS uses two-space separator: "<hash>  <filename>"
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}

		filename := strings.TrimSpace(parts[1])
		hash := strings.TrimSpace(parts[0])

		// Match with or without "binaries/" prefix
		if filename == binaryName || filename == "binaries/"+binaryName {
			expectedHash = hash
			break
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("checksum for %s not found in SHA256SUMS.txt", binaryName)
	}

	if downloadedHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s",
			binaryName, expectedHash, downloadedHash)
	}

	return nil
}

// replaceBinary atomically replaces the current binary with the downloaded one.
func replaceBinary(tmpPath, targetPath string) error {
	// Try direct rename (works on same filesystem)
	if err := os.Rename(tmpPath, targetPath); err == nil {
		return nil
	}

	// Fallback: copy then remove (works across filesystems, e.g. snap, flatpak)
	src, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer src.Close()

	// Write to target, preserving permissions
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		// Try with sudo by writing to a temp location
		dir := filepath.Dir(targetPath)
		if !isWritable(dir) {
			return fmt.Errorf("cannot write to %s (try running with sudo)", dir)
		}
		return fmt.Errorf("write to %s: %w", targetPath, err)
	}
	defer target.Close()

	if _, err := io.Copy(target, src); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	return nil
}

// isWritable checks if a directory is writable by opening a test file.
func isWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".anvil-write-test-*")
	if err != nil {
		return false
	}
	os.Remove(f.Name())
	f.Close()
	return true
}
