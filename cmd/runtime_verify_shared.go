package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/runtime"
)

// verifySharedCmd represents the "anvil runtime verify-shared" command that
// checks shared resource integrity.
//
// Reference: ST-P5-05
var verifySharedCmd = &cobra.Command{
	Use:   "verify-shared",
	Short: "Verify shared resource integrity",
	Long: `Check that shared resources are intact and accessible.

This command verifies that all shared resource directories (config, storage,
logs, temp) exist on the filesystem, are proper directories, and are
properly isolated from release directories.

It uses SharedResourceManager to perform the checks.`,
	Example: `  anvil runtime verify-shared
  anvil runtime verify-shared --install-root /opt/anvil`,
	SilenceUsage: true,
	RunE:         runVerifyShared,
}

func init() {
	runtimeCmd.AddCommand(verifySharedCmd)

	verifySharedCmd.Flags().String(
		"install-root",
		runtime.DefaultInstallRoot,
		"override the default Runtime install root directory",
	)
}

// runVerifyShared executes the shared resource verification.
//
// It checks each shared directory individually and reports PASS/FAIL.
// Returns an error (non-zero exit) if any shared resource is compromised.
func runVerifyShared(cmd *cobra.Command, args []string) error {
	cfg := runtime.DefaultRuntimeConfig()

	if installRoot, _ := cmd.Flags().GetString("install-root"); installRoot != "" {
		cfg.InstallRoot = installRoot
	}

	sharedMgr := runtime.NewSharedResourceManager(cfg)

	fmt.Fprintf(cmd.OutOrStdout(), "Shared Resource Verification\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Install Root: %s\n\n", cfg.InstallRoot)

	// Check each shared resource directory.
	allOK := true
	failures := []string{}

	for _, dir := range sharedMgr.AllSharedDirPaths() {
		status := "PASS"
		info, err := os.Stat(dir)
		if err != nil {
			status = "FAIL"
			allOK = false
			failures = append(failures, dir)
			fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", status, dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  directory does not exist\n")
		} else if !info.IsDir() {
			status = "FAIL"
			allOK = false
			failures = append(failures, dir)
			fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", status, dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  exists but is not a directory\n")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", status, dir)
		}
	}

	// Validate isolation.
	if err := sharedMgr.ValidateIsolation(); err != nil {
		allOK = false
		fmt.Fprintf(cmd.OutOrStdout(), "[FAIL] isolation: %s\n", err)
		failures = append(failures, err.Error())
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "[PASS] shared resources are isolated from releases\n")
	}

	// Summary.
	fmt.Fprintln(cmd.OutOrStdout(), "")
	if allOK {
		fmt.Fprintln(cmd.OutOrStdout(), "Shared resources are intact.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Shared resources are compromised:")
	for _, f := range failures {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", f)
	}
	return fmt.Errorf("shared resources are compromised")
}
