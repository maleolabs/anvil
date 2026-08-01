// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-01, EPIC-003
package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestArtifactCmd_Registered verifies that the artifact command is registered
// as a subcommand of the root command.
func TestArtifactCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			found = true
			break
		}
	}

	if !found {
		t.Error("artifact command not found in root command's subcommands")
	}
}

// TestPackageCmd_Registered verifies that the package subcommand is registered
// under the artifact command.
func TestPackageCmd_Registered(t *testing.T) {
	var artifactSub *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			artifactSub = c
			break
		}
	}

	if artifactSub == nil {
		t.Fatal("artifact command not found")
	}

	found := false
	for _, c := range artifactSub.Commands() {
		if c.Use == "package" {
			found = true
			break
		}
	}

	if !found {
		t.Error("package subcommand not found under artifact command")
	}
}

// TestPackageCmd_Help verifies that the package command has the expected help
// text and usage information.
func TestPackageCmd_Help(t *testing.T) {
	if packageCmd.Short == "" {
		t.Error("package command short description is empty")
	}

	if packageCmd.Long == "" {
		t.Error("package command long description is empty")
	}

	if packageCmd.Use != "package" {
		t.Errorf("package command Use = %q, want %q", packageCmd.Use, "package")
	}
}

// TestPackageCmd_RunE verifies the package command has a RunE handler set.
func TestPackageCmd_RunE(t *testing.T) {
	if packageCmd.RunE == nil {
		t.Error("package command RunE handler is nil")
	}
}

// TestPackageCmd_NoArgs verifies the package command rejects arguments.
func TestPackageCmd_NoArgs(t *testing.T) {
	if packageCmd.Args == nil {
		t.Error("package command Args validator is nil, expected cobra.NoArgs")
		return
	}

	// Use a cobra.Command to test the args validator (cobra.NoArgs requires
	// a non-nil command reference even though it doesn't inspect it).
	cmd := &cobra.Command{Use: "package"}

	// The command accepts no arguments - calling with args should fail.
	err := packageCmd.Args(cmd, []string{"extra-arg"})
	if err == nil {
		t.Error("expected error when passing arguments to package command")
	}
}
