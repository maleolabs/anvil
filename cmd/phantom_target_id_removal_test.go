// Package cmd implements the Anvil CLI commands.
//
// ── Phantom target-id Argument Removal (TS-019-04-03) ─────────────────
//
// D10 (ADR-032): the <target-id> argument on the local target-centric
// deployment commands (install/activate/rollback/info) is a phantom
// argument — accepted by the command surface with no defined effect. It
// is a documented correlation label echoed in output/JSON, not a target
// selector (TD-006 option a; D10 disposition §4.3, review 02 §3 D10).
// The contract-bound deployment surface itself is accepted as-is
// (EPIC-016 disposition); only the phantom argument is removed, at
// window end per the announced deprecation schedule (ADR-032, EPIC-017
// mechanics): the argument no longer appears in argument handling or
// help text, and the pre-removal invocation form is rejected.
//
// "anvil deployment upload" is deliberately NOT affected: its
// <target-id> is part of the SSH transport contract (TransportResult
// identity; D10 disposition §3.1/§4.3 — upload is excluded from the
// phantom-argument list).
//
// Reference: TS-019-04-03, ADR-032 (D10), EPIC-019
package cmd

import (
	"strings"
	"testing"
)

// phantomTargetIDCommands lists the commands whose pre-removal
// invocation form (with the phantom <target-id> argument) must now be
// rejected. Each entry documents the removed argument and the accepted
// post-removal form (the removal announcement).
var phantomTargetIDCommands = []struct {
	args         []string // pre-removal invocation: must be rejected
	wantError    string   // rejection error fragment
	acceptedForm string   // documented post-removal form
}{
	{
		args:         []string{"deployment", "install", "my-target", "path/to/artifact.tar.gz"},
		wantError:    "requires 1 argument",
		acceptedForm: "anvil deployment install <artifact-path>",
	},
	{
		args:         []string{"deployment", "activate", "my-target", "my-project", "rel-001"},
		wantError:    "requires 2 argument",
		acceptedForm: "anvil deployment activate <project-id> <release-id>",
	},
	{
		args:         []string{"deployment", "rollback", "my-target", "my-project"},
		wantError:    "requires 1 argument",
		acceptedForm: "anvil deployment rollback <project-id>",
	},
	{
		args:         []string{"deployment", "info", "my-target"},
		wantError:    "unknown command",
		acceptedForm: "anvil deployment info",
	},
}

// TestPhantomTargetIDArgument_Rejected verifies that the pre-removal
// invocation form (with the phantom <target-id> argument) is rejected
// per the announced deprecation schedule (TS-019-04-03 DoD: "The
// target-id argument is rejected after removal"). No silent removal:
// invoking the old form fails with an argument/validation error instead
// of succeeding with the argument silently ignored.
func TestPhantomTargetIDArgument_Rejected(t *testing.T) {
	for _, tc := range phantomTargetIDCommands {
		_, _, stderr, err := executeCommand(tc.args...)
		if err == nil {
			t.Errorf("executeCommand(%q) must be rejected after the target-id removal, got nil error",
				strings.Join(tc.args, " "))
		}
		if !strings.Contains(stderr, tc.wantError) {
			t.Errorf("executeCommand(%q) stderr should reject with %q, got: %q",
				strings.Join(tc.args, " "), tc.wantError, stderr)
		}
	}
}

// TestPhantomTargetIDArgument_HelpNoLongerExposesIt verifies that the
// command help (Use line and Long text) no longer exposes <target-id>
// (TS-019-04-03 DoD: "Argument handling and help text no longer expose
// target-id").
func TestPhantomTargetIDArgument_HelpNoLongerExposesIt(t *testing.T) {
	for _, name := range []string{"install", "activate", "rollback", "info"} {
		sub, _, err := rootCmd.Find([]string{"deployment", name})
		if err != nil {
			t.Fatalf("deployment %s must remain registered: %v", name, err)
		}
		if strings.Contains(sub.Use, "target-id") {
			t.Errorf("deployment %s Use line must not expose <target-id>, got: %q", name, sub.Use)
		}
		if strings.Contains(sub.Long, "target-id") {
			t.Errorf("deployment %s help text (Long) must not expose <target-id>, got: %q", name, sub.Long)
		}
	}

	// The parent group help must not describe the removed argument either.
	if strings.Contains(deploymentCmd.Long, "target-id") {
		t.Errorf("deployment group help text must not expose <target-id>, got: %q", deploymentCmd.Long)
	}
}

// TestPhantomTargetIDArgument_UploadUnaffected verifies that the SSH
// transport command is not touched by this removal: its <target-id>
// remains part of the transport contract surface (D10 disposition
// §3.1/§4.3; Transition Plan §12.2 "deployment upload keeps semantics
// and behavior").
func TestPhantomTargetIDArgument_UploadUnaffected(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "upload"})
	if err != nil {
		t.Fatalf("deployment upload must remain registered: %v", err)
	}
	if sub.Use != "upload <target-id> <artifact-path>" {
		t.Errorf("upload Use = %q, want %q", sub.Use, "upload <target-id> <artifact-path>")
	}
}
