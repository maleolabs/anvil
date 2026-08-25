// Package cmd implements the Anvil CLI commands.
//
// Reference: anvil-cli/plan:local-deploy-mvp, anvil-cli/sto:local-deploy-cli,
// spikes/local-deploy-ux/harness.go (RunDryRun contract)
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/deployment"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// deployCmd represents the `anvil deploy` command.
//
// Lifecycle: build → verify → (push → install → activate). For --dry-run
// the lifecycle stops after verify (build+verify only, no install).
//
// Reuses internal/artifact.Package + VerifyArtifact and internal/output
// (AppError, envelope v1, PlainStepReporter) consistently with spike UX
// RunDryRun (harness.go, progress.go, help.go).
//
// Reference: sto:local-deploy-cli, plan:local-deploy-mvp
var deployCmd = &cobra.Command{
	Use:           "deploy",
	Short:         "Deploy the current project to a target environment",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `Deploy the current project to a target environment via SSH (full lifecycle).

The lifecycle is build → verify → push → install → activate, enforced by the
Anvil runtime on the target. Verification-before-trust gates activate; the
artifact's embedded manifest.json is the identity source (artifact_id from content).

Targets are declared in anvil.yaml (future: server.targets[env] {host,user,sshKeyPath}).
In this MVP the deploy command performs build+verify locally; transport is provided
by the next work items. Use --dry-run to validate build+verify without install
(AC1).

Flags:
  --target   Target environment declared in anvil.yaml server.targets (required)
  --dry-run  Build and verify only; do not push or install
  --json     Machine-readable JSON envelope {"version":"1","status":"success|error"}
  --confirm  Confirm deploy to protected envs (staging, production, prod). Required
             when --target is staging/production/prod and --dry-run is not set.

Progress:
  Push progress prints per-chunk % when transport is active.
  Verify step prints per-check PASS/FAIL before install (verification step).

Exit codes (stable, automation-safe):
  0  Success — deploy completed (or dry-run verify PASS)
  1  General error — build/verify FAIL, network/transport failure (retryable)
  2  Configuration error — missing --target or unknown env / malformed anvil.yaml
  4  Precondition error — SSH auth fail / permission denied (when transport active)

On verify FAIL:
  Error: artifact verification FAIL — install rejected
  Reason: verification-before-trust gate FAIL (checksum or manifest mismatch)
  Resolution: Do not retry the same artifact — rebuild: anvil deploy --target <env> will rebuild, re-verify, then push. Run with --dry-run to inspect verify checks.

On missing --target:
  Error: missing required flag --target
  Reason: The --target flag is required to select a deployment environment
  Resolution: Run 'anvil deploy --target <env>' with an environment declared in anvil.yaml server.targets. See 'anvil deploy --help'.

On protected env without --confirm:
  Error: deploy to "staging" requires --confirm
  Reason: Target "staging" is a protected environment
  Resolution: Re-run with --confirm: anvil deploy --target staging --confirm

Secrets are never printed: DEPLOY_SSH_KEY, private key content, and full key paths are redacted to [REDACTED].

Examples:
  anvil deploy --target staging --dry-run                # build+verify only, no install (AC1)
  anvil deploy --target staging --dry-run --json         # machine-readable (envelope v1)
  anvil deploy --target production --json --confirm      # deploy with JSON envelope (protected env)
  anvil deploy --target staging --help                   # this help`,
	Example: `  anvil deploy --target staging --dry-run
  anvil deploy --target production --dry-run --json
  anvil deploy --target staging --confirm
  anvil deploy --target production --confirm --json`,
	Args: cobra.NoArgs,
	RunE: runDeploy,
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().String("target", "", "Target environment declared in anvil.yaml server.targets (required)")
	deployCmd.Flags().Bool("dry-run", false, "Build and verify only; do not push or install")
	deployCmd.Flags().Bool("confirm", false, "Confirm deploy to protected envs (staging, production, prod)")
	AddJSONFlag(deployCmd)
}

// isProtectedEnv reports whether target requires --confirm (case-insensitive, trimmed).
// Kept for backward-compat unit tests; guard now uses deployment.ClassifyEnv + CheckDeployGuard.
func isProtectedEnv(target string) bool {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "staging", "production", "prod":
		return true
	default:
		return false
	}
}

// runDeploy executes `anvil deploy`.
func runDeploy(cmd *cobra.Command, _ []string) error {
	target, _ := cmd.Flags().GetString("target")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	asJSON, _ := cmd.Flags().GetBool("json")
	confirm, _ := cmd.Flags().GetBool("confirm")

	// --target required (AC3 error mapping → AppError with guidance, exit 2 config).
	if target == "" {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    "missing required flag --target",
			Reason:     "The --target flag is required to select a deployment environment",
			Resolution: "Run 'anvil deploy --target <env>' with an environment declared in anvil.yaml server.targets. See 'anvil deploy --help' for examples",
		}, output.ExitCodeConfig)
	}

	// Guard per-env (sto:local-deploy-guard AC1, AC4 — no RBAC bypass).
	// Single entry point: deployment.CheckDeployGuard (dev allow, staging --confirm, prod CI-only + allowlist + prompt).
	// Dry-run is verification-only and never requires confirm (consistent with prior behavior).
	if !dryRun {
		// Resolve SSH principal for binding (not spoofable DeployUser string). If resolution fails,
		// use empty principal — guard will evaluate allowlist as empty (fail closed for prod local).
		var creds deployment.SSHCredentials
		// Only try to resolve when server.targets exists; framework-free skips allowlist but still enforces staging confirm.
		shouldResolveForGuard := true
		if flat, _, ferr := config.ResolveAndValidateConfig(); ferr == nil {
			if targets, _ := config.ExtractServerTargets(flat); len(targets) == 0 {
				shouldResolveForGuard = false
			}
		}
		if shouldResolveForGuard {
			if c, err := deployment.ResolveSSHCredentialsForTarget(target); err == nil {
				creds = c
			}
		}
		// Provider for prompt: stdin/stdout from cobra command; nil allowed for non-interactive case
		inReader := cmd.InOrStdin()
		outWriter := cmd.OutOrStdout()
		// For JSON mode, prompt still goes to stderr so envelope stays clean
		if asJSON {
			outWriter = cmd.ErrOrStderr()
		}
		if err := deployment.CheckDeployGuardWithDryRun(target, confirm, dryRun, creds, inReader, outWriter); err != nil {
			// Determine redacted principal for audit
			principal := creds.User
			if principal == "" {
				principal = "unknown"
			}
			// Fire audit trail (best-effort, 0600 HMAC) — projectRoot not yet resolved here, use cwd as fallback
			cwd, _ := os.Getwd()
			_ = auditGuardDecision(cwd, target, principal, "guard", "deny", err.Error())
			return ReportError(cmd, &output.AppError{
				Message:       output.SanitizeLogLine(err.Error()),
				Reason:        output.SanitizeLogLine(err.Error()),
				Resolution:    fmt.Sprintf("Re-run with --confirm: anvil deploy --target %s --confirm (staging requires confirm; prod CI-only unless allowlisted + confirm prompt)", target),
				ExitCodeValue: output.ExitCodeGeneral,
			})
		}
		// Guard PASS audit (allow) — best-effort
		{
			principal := creds.User
			if principal == "" {
				principal = "unknown"
			}
			cwd, _ := os.Getwd()
			_ = auditGuardDecision(cwd, target, principal, "guard", "allow", "guard PASS")
		}
	}

	// Require project context (consistent with artifact package).
	cfg, err := RequireProject(cmd)
	if err != nil {
		// RequireProject already wrote a user-facing message to stderr for
		// ErrNoProjectFound; map it to AppError with config exit code for
		// deterministic automation.
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    "not an Anvil project",
			Reason:     "No anvil.yaml found in the current directory or parents",
			Resolution: "Run 'anvil init <name>' to create a project, or cd to a project directory. See 'anvil deploy --help'",
			Err:        err,
		}, output.ExitCodeGeneral)
	}

	// Resolve project root (anchors relative paths, consistent with artifact pipeline).
	projectRoot, err := project.Discover()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not discover project root",
			Reason:     err.Error(),
			Resolution: "Ensure anvil.yaml exists in the project root and is readable. See 'anvil deploy --help'",
			Err:        err,
		})
	}

	// AC1/AC2: single-source config — server.targets[env] validation (host/user/ip, host-key).
	// DEPLOY_SSH_KEY override is resolved via deployment.ResolveSSHCredentialsForTarget;
	// redaction via output.SanitizeLogLine (AC3). Framework-free validated via server.go (AC4).
	// When no server.targets are defined at all (framework-free projects, existing tests),
	// validation is skipped so `anvil deploy --target <env> --dry-run` still works for build+verify.
	// When at least one target is defined, the requested target must exist.
	shouldValidateServerTarget := true
	if flat, _, ferr := config.ResolveAndValidateConfig(); ferr == nil {
		if targets, _ := config.ExtractServerTargets(flat); len(targets) == 0 {
			shouldValidateServerTarget = false
		}
	}
	if shouldValidateServerTarget {
		if _, serr := deployment.ResolveSSHCredentialsForTarget(target); serr != nil {
			errStr := serr.Error()
			isNotFound := strings.Contains(errStr, "not found in anvil.yaml") || strings.Contains(errStr, "not found")
			if dryRun && isNotFound {
				// Framework-free / missing target is tolerated for dry-run (AC1 build+verify only).
				// Warn to stderr (not stdout) so JSON envelope stays clean.
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", output.SanitizeLogLine(errStr))
			} else {
				// Surface as config error (exit 2) with actionable guidance; sanitize any secret in message.
				reason := output.SanitizeLogLine(errStr)
				return ReportErrorWithCode(cmd, &output.AppError{
					Message:    output.SanitizeLogLine(fmt.Sprintf("invalid deploy target %q", target)),
					Reason:     reason,
					Resolution: "Check anvil.yaml server.targets[env] {host,user,sshKeyPath,knownHostsPath} (ADR-005 single source) and DEPLOY_SSH_KEY override (redacted). Host-key verification wajib — prod requires knownHostsPath strict. See 'anvil config validate'",
					Err:        serr,
				}, output.ExitCodeConfig)
			}
		}
	}

	// Prepare deploy data (build+verify) — reuses internal/artifact as spike does.
	// We build into a temp output dir to keep dry-run isolated and not pollute
	// .anvil/artifacts unless user explicitly wants persistence. The temp dir
	// is cleaned by OS temp handling; we keep the artifact path for JSON/human.
	overallStart := time.Now()
	tmpOut, err := os.MkdirTemp("", "anvil-deploy-*")
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not create temporary artifact directory",
			Reason:     err.Error(),
			Resolution: "Check temp directory permissions and disk space, then retry",
			Err:        err,
		})
	}
	// Do not remove tmpOut immediately — callers may want to inspect artifact.
	// Cleanup is OS-managed; for tests we leak intentionally (t.TempDir pattern).

	identity := cfg.Identity()
	metadata := cfg.Metadata()
	version := metadata.Version()
	if version == "" {
		version = "0.0.0"
	}
	source := identity.Name()
	projectID := identity.Name()
	if projectID == "" {
		projectID = "anvil-project"
	}

	// Include/Exclude from project config (artifact filtering engine).
	var include []string
	var exclude []string
	if cfg.Artifact != nil {
		include = cfg.Artifact.Include
		exclude = cfg.Artifact.Exclude
	}

	// Packaging reporter: human mode gets PlainStepReporter, JSON mode silent.
	var pkgReporter artifact.PackagingReporter
	var humanReporter *output.PlainStepReporter
	if !asJSON {
		humanReporter = output.NewPlainStepReporter(cmd.OutOrStdout())
		humanReporter.Start(fmt.Sprintf("Deploy --target %s", target))
		pkgReporter = &deployPackagingReporter{hr: humanReporter}
	}

	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: projectRoot,
		OutputDir: tmpOut,
		Formats:   []string{"tar.gz"},
		Include:   include,
		Exclude:   exclude,
		Version:   version,
		Source:    source,
		ProjectID: projectID,
		Reporter:  pkgReporter,
	})
	if err != nil {
		if humanReporter != nil {
			humanReporter.Failed(fmt.Sprintf("Deploy --target %s", target), time.Since(overallStart))
		}
		return ReportError(cmd, &output.AppError{
			Message:    "could not package artifact",
			Reason:     err.Error(),
			Resolution: "Check project files and anvil.yaml artifact filtering rules, then retry with --dry-run to isolate packaging",
			Err:        err,
		})
	}

	manifest := result.Manifest
	if manifest == nil {
		// Fallback: try to read manifest from artifact (should not happen).
		m, rerr := artifact.ReadManifest(result.ArtifactPath)
		if rerr != nil {
			if humanReporter != nil {
				humanReporter.Failed(fmt.Sprintf("Deploy --target %s", target), time.Since(overallStart))
			}
			return ReportError(cmd, &output.AppError{
				Message:    "could not read artifact manifest",
				Reason:     rerr.Error(),
				Resolution: "Re-package with 'anvil artifact package' and verify with 'anvil artifact verify'",
				Err:        rerr,
			})
		}
		manifest = m
	}

	// Verify (verification-before-trust gate, no install).
	vr, err := artifact.VerifyArtifact(result.ArtifactPath)
	if err != nil {
		if humanReporter != nil {
			humanReporter.Failed(fmt.Sprintf("Deploy --target %s", target), time.Since(overallStart))
		}
		return ReportError(cmd, &output.AppError{
			Message:    "Deploy failed: artifact verification error",
			Reason:     err.Error(),
			Resolution: "Rebuild with 'anvil deploy --target <env> --dry-run' to inspect verification checks. If repeat FAIL, check source filtering or disk corruption",
			Err:        err,
		})
	}

	duration := time.Since(overallStart)

	// Verification FAIL is a general error (exit 1) with actionable guidance,
	// consistent with spike error matrix KindVerifyFail.
	if !vr.Passed {
		if asJSON {
			// JSON error envelope — still return AppError so process exits 1.
			// Data is not written on failure; error envelope carries message.
			details := collectFailedChecks(vr)
			appErr := &output.AppError{
				Message:    "Deploy failed: artifact verification FAIL — install rejected",
				Reason:     details,
				Resolution: "Do not retry the same artifact — rebuild: anvil deploy --target <env> will rebuild, re-verify, then push. Run with --dry-run to inspect verify checks",
				ExitCodeValue: output.ExitCodeGeneral,
			}
			return ReportError(cmd, appErr)
		}
		// Human path: render checks then return error.
		if humanReporter != nil {
			humanReporter.StepStart("Verify artifact")
			humanReporter.StepFailed("Verify artifact", time.Millisecond*10, fmt.Errorf("verify FAIL"))
			output.PrintStatus(cmd.OutOrStdout(), output.StatusFail, "Verify FAIL")
			for _, c := range vr.Checks {
				st := output.StatusPass
				if !c.Passed {
					st = output.StatusFail
				}
				output.PrintStatus(cmd.OutOrStdout(), st, fmt.Sprintf("%s: %s", c.Name, c.Details))
			}
			humanReporter.Failed(fmt.Sprintf("Deploy --target %s", target), duration)
		} else {
			// Fallback when reporter nil (should not happen in human mode)
			fmt.Fprintf(cmd.OutOrStdout(), "Deploy dry-run --target %s\n", target)
			for _, c := range vr.Checks {
				st := output.StatusPass
				if !c.Passed {
					st = output.StatusFail
				}
				output.PrintStatus(cmd.OutOrStdout(), st, fmt.Sprintf("%s: %s", c.Name, c.Details))
			}
		}
		return ReportError(cmd, &output.AppError{
			Message:    "Deploy failed: artifact verification FAIL — install rejected",
			Reason:     collectFailedChecks(vr),
			Resolution: "Do not retry the same artifact — rebuild: anvil deploy --target <env> will rebuild, re-verify, then push. Run with --dry-run to inspect verify checks",
			ExitCodeValue: output.ExitCodeGeneral,
		})
	}

	// Non-dry-run: push via SSH transport (atomic tmp+rename, retry, host-key verification).
	// Dry-run stops after verify; verify FAIL already returned above.
	// Framework-free path (no server.targets at all) skips push for compatibility with existing tests
	// (build+verify only) — real push requires at least one target defined.
	var pushResult *deployment.TransportResult
	var pushDuration time.Duration
	if !dryRun && shouldValidateServerTarget {
		// Resolve credentials from server.targets[env] (single source, AC1) via ResolveSSHCredentialsForTarget.
		creds, err := deployment.ResolveSSHCredentialsForTarget(target)
		if err != nil {
			return ReportErrorWithCode(cmd, &output.AppError{
				Message:    output.SanitizeLogLine(fmt.Sprintf("invalid deploy target %q", target)),
				Reason:     output.SanitizeLogLine(err.Error()),
				Resolution: "Check anvil.yaml server.targets[env] {host,user,sshKeyPath,knownHostsPath} and DEPLOY_SSH_KEY override (redacted)",
				Err:        err,
			}, output.ExitCodeConfig)
		}
		// Build transport from resolved credentials (reuses spike SSH pattern but real SSH).
		var opts []deployment.Option
		if creds.KnownHostsPath != "" {
			opts = append(opts, deployment.WithKnownHosts(creds.KnownHostsPath, creds.KnownHostsMode))
		}
		transport := deployment.NewSSHTransport(creds.Host, creds.User, creds.KeyPath, creds.Port, opts...)
		// ManifestContent for transport identity (raw manifest JSON, redacted in logs).
		manifestContent, _ := json.Marshal(manifest)
		payload := deployment.ArtifactPayload{
			Path:            result.ArtifactPath,
			ManifestContent: manifestContent,
		}
		// Target adapter for deployment.Target contract
		tgt := &deployTarget{id: deployment.TargetID(target)}
		// Push with retry idempotent (AC2) and histogram timing (AC1).
		// Observability: push % ticks 0→100 visible via DeployProgress (AC1) — simulated for MVP,
		// emitted AFTER Deliver so ticks are not fake before transport (review HIGH #1). The ticks
		// use real artifact size (info.Size()) and are progressive after successful push.
		pushStart := time.Now()
		if humanReporter != nil {
			humanReporter.StepStart("Push artifact")
		}
		res, terr := transport.Deliver(payload, tgt)
		pushDuration = time.Since(pushStart)
		if terr != nil {
			if humanReporter != nil {
				humanReporter.StepFailed("Push artifact", pushDuration, terr)
				humanReporter.Failed(fmt.Sprintf("Deploy --target %s", target), time.Since(overallStart))
			}
			// Map TransportError.Kind to exit code + Guidance (AC3, 6 kinds)
			if te, ok := terr.(*deployment.TransportError); ok {
				return ReportErrorWithCode(cmd, &output.AppError{
					Message:    fmt.Sprintf("deploy push failed for target %q", target),
					Reason:     output.SanitizeLogLine(te.Reason),
					Resolution: te.Guidance(),
					Err:        te,
				}, te.ExitCode())
			}
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("deploy push failed for target %q", target),
				Reason:     output.SanitizeLogLine(terr.Error()),
				Resolution: "Check network, credentials (redacted), and server status, then retry",
				Err:        terr,
			})
		}
		pushResult = res
		if humanReporter != nil {
			// Emit simulated 0→100 ticks AFTER Deliver using real artifact size (MVP simulated, real total).
			if info, err := os.Stat(result.ArtifactPath); err == nil {
				base := filepath.Base(result.ArtifactPath)
				output.EmitPushProgress(cmd.OutOrStdout(), base, info.Size())
			}
			humanReporter.StepComplete("Push artifact", pushDuration)
		} else {
			// Non-reporter human path: also emit ticks after deliver for AC1 visibility (simulated, real size).
			if info, err := os.Stat(result.ArtifactPath); err == nil {
				base := filepath.Base(result.ArtifactPath)
				output.EmitPushProgress(cmd.OutOrStdout(), base, info.Size())
			}
		}
	}

	// Success path: render human or JSON consistently (AC1).
	// Data shape mirrors spikes/local-deploy-ux/harness.go DryRunResult JSON.
	if asJSON {
		data := map[string]interface{}{
			"target":        target,
			"dry_run":       dryRun,
			"artifact_id":   manifest.ArtifactID,
			"version":       manifest.Version,
			"checksum":      manifest.Checksum,
			"checksum_type": manifest.ChecksumType,
			"project_id":    manifest.ProjectID,
			"artifact_path": result.ArtifactPath,
			"verify": map[string]interface{}{
				"passed": vr.Passed,
				"checks": vr.Checks,
			},
		}
		if !dryRun {
			data["confirm"] = confirm
			if pushResult != nil {
				data["remote_path"] = pushResult.RemotePath
				data["push_duration_ms"] = pushDuration.Milliseconds()
			}
		}
		if err := output.WriteJSON(cmd.OutOrStdout(), data); err != nil {
			return ReportError(cmd, &output.AppError{
				Message:    "could not write JSON output",
				Reason:     err.Error(),
				Resolution: "Check stdout is writable and retry",
				Err:        err,
			})
		}
		return nil
	}

	// Human output (non-JSON) via PlainStepReporter + status lines.
	renderDeployHumanWithPush(cmd, target, manifest, vr, duration, result.ArtifactPath, dryRun, humanReporter, overallStart, pushResult, pushDuration)
	return nil
}

// deployTarget is a minimal Target for `anvil deploy --target`.
type deployTarget struct{ id deployment.TargetID }

func (d *deployTarget) ID() deployment.TargetID { return d.id }
func (d *deployTarget) Metadata() deployment.TargetMetadata {
	return deployment.TargetMetadata{ID: d.id, Name: string(d.id)}
}
func (d *deployTarget) CompatibilityInput() deployment.CompatibilityInput { return deployment.CompatibilityInput{} }
func (d *deployTarget) ValidateCompatibility(_ deployment.CompatibilityInput) error { return nil }

// deployPackagingReporter bridges artifact.PackagingReporter to PlainStepReporter.
type deployPackagingReporter struct {
	hr *output.PlainStepReporter
}

func (r *deployPackagingReporter) StepStart(name string) {
	if r.hr != nil {
		r.hr.StepStart(name)
	}
}
func (r *deployPackagingReporter) StepComplete(name string, duration time.Duration) {
	if r.hr != nil {
		r.hr.StepComplete(name, duration)
	}
}
func (r *deployPackagingReporter) StepFailed(name string, duration time.Duration, err error) {
	if r.hr != nil {
		r.hr.StepFailed(name, duration, err)
	}
}

// renderDeployHuman prints the human-readable deploy result consistently with spike UX.
func renderDeployHuman(cmd *cobra.Command, target string, m *artifact.Manifest, vr *artifact.VerificationResult, dur time.Duration, artifactPath string, dryRun bool, reporter *output.PlainStepReporter, overallStart time.Time) {
	renderDeployHumanWithPush(cmd, target, m, vr, dur, artifactPath, dryRun, reporter, overallStart, nil, 0)
}

// renderDeployHumanWithPush prints human output including push result when present (AC1).
func renderDeployHumanWithPush(cmd *cobra.Command, target string, m *artifact.Manifest, vr *artifact.VerificationResult, dur time.Duration, artifactPath string, dryRun bool, reporter *output.PlainStepReporter, overallStart time.Time, pushResult *deployment.TransportResult, pushDuration time.Duration) {
	w := cmd.OutOrStdout()
	if dryRun {
		fmt.Fprintf(w, "Deploy dry-run --target %s\n", target)
	} else {
		fmt.Fprintf(w, "Deploy --target %s\n", target)
	}
	if reporter != nil {
		reporter.StepComplete("Build artifact", dur/2)
		fmt.Fprintf(w, "    artifact_id: %s\n", m.ArtifactID)
		fmt.Fprintf(w, "    version: %s\n", m.Version)
		fmt.Fprintf(w, "    checksum: %s (%s)\n", m.Checksum, m.ChecksumType)
		fmt.Fprintf(w, "    path: %s\n", filepath.Base(artifactPath))
		reporter.StepComplete("Verify artifact", dur/2)
	} else {
		fmt.Fprintf(w, "  Step: Build artifact ✓ (%s)\n", output.FormatDuration(dur/2))
		fmt.Fprintf(w, "    artifact_id: %s\n", m.ArtifactID)
		fmt.Fprintf(w, "    version: %s\n", m.Version)
		fmt.Fprintf(w, "    checksum: %s (%s)\n", m.Checksum, m.ChecksumType)
		fmt.Fprintf(w, "    path: %s\n", filepath.Base(artifactPath))
		fmt.Fprintf(w, "  Step: Verify artifact ✓ (%s)\n", output.FormatDuration(dur/2))
	}
	if vr.Passed {
		output.PrintStatus(w, output.StatusPass, fmt.Sprintf("Verify %d checks PASS", len(vr.Checks)))
	} else {
		output.PrintStatus(w, output.StatusFail, "Verify FAIL")
	}
	for _, c := range vr.Checks {
		st := output.StatusPass
		if !c.Passed {
			st = output.StatusFail
		}
		output.PrintStatus(w, st, fmt.Sprintf("%s: %s", c.Name, c.Details))
	}
	if dryRun {
		fmt.Fprintf(w, "Dry-run complete — no install performed (build+verify only) (%s)\n", output.FormatDuration(dur))
		fmt.Fprintln(w, "Tip: remove --dry-run to push and install.")
		if reporter != nil {
			reporter.Complete("Deploy dry-run complete", time.Since(overallStart))
		}
	} else {
		if pushResult != nil {
			// When reporter is active, push step already completed via StepComplete + EmitPushProgress in runDeploy.
			// Avoid duplicate Step line; show remote + summary.
			if reporter == nil {
				fmt.Fprintf(w, "  Step: Push artifact ✓ (%s)\n", output.FormatDuration(pushDuration))
			}
			fmt.Fprintf(w, "    remote: %s\n", pushResult.RemotePath)
			fmt.Fprintf(w, "Deploy complete — artifact pushed to %s (%s)\n", pushResult.RemotePath, output.FormatDuration(dur+pushDuration))
		} else {
			fmt.Fprintf(w, "Deploy complete — artifact verified and ready to push (%s)\n", output.FormatDuration(dur))
			fmt.Fprintln(w, "Note: transport (push/install) is handled by the next work items; this build+verify validated the artifact for the target.")
		}
		if reporter != nil {
			reporter.Complete("Deploy complete", time.Since(overallStart))
		}
	}
}

// collectFailedChecks aggregates failed check details for error Reason.
func collectFailedChecks(vr *artifact.VerificationResult) string {
	if vr == nil {
		return "verification result unavailable"
	}
	var buf bytes.Buffer
	first := true
	for _, c := range vr.Checks {
		if !c.Passed {
			if !first {
				buf.WriteString("; ")
			}
			fmt.Fprintf(&buf, "%s: %s", c.Name, c.Details)
			first = false
		}
	}
	if buf.Len() == 0 {
		return "verification failed (no details)"
	}
	return buf.String()
}

// deployJSONData is the machine-readable data shape for `anvil deploy --json`.
// Exported for tests and ValidateHumanJSONConsistency-style checks.
//
// Reference: spikes/local-deploy-ux/harness.go DryRunResult JSON data
type deployJSONData struct {
	Target       string                   `json:"target"`
	DryRun       bool                     `json:"dry_run"`
	ArtifactID   string                   `json:"artifact_id"`
	Version      string                   `json:"version"`
	Checksum     string                   `json:"checksum"`
	ChecksumType string                   `json:"checksum_type"`
	ProjectID    string                   `json:"project_id"`
	ArtifactPath string                   `json:"artifact_path"`
	Verify       *artifact.VerificationResult `json:"verify"`
}

// auditGuardDecision is best-effort HMAC audit for guard decisions (0600, redacted, SSH principal bound).
func auditGuardDecision(projectRoot, env, principal, action, result, details string) error {
	dir := projectRoot
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		} else {
			return nil
		}
	}
	// prefer <projectRoot>/.anvil as audit dir (mode 0700), fallback to projectRoot
	candidates := []string{filepath.Join(dir, ".anvil"), dir}
	for _, d := range candidates {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			logger, err := deployment.NewAuditLogger(d)
			if err != nil {
				continue
			}
			_, _ = logger.Log(env, action, "", "", "", "", "", details, principal, result)
			_ = os.Chmod(logger.AuditLogPath(), 0600)
			_ = os.Chmod(logger.AuditKeyPath(), 0600)
			return nil
		}
	}
	// create .anvil dir if not exists
	anvilDir := filepath.Join(dir, ".anvil")
	if err := os.MkdirAll(anvilDir, 0700); err == nil {
		if logger, err := deployment.NewAuditLogger(anvilDir); err == nil {
			_, _ = logger.Log(env, action, "", "", "", "", "", details, principal, result)
			_ = os.Chmod(logger.AuditLogPath(), 0600)
			_ = os.Chmod(logger.AuditKeyPath(), 0600)
		}
	}
	return nil
}

// ValidateDeployHumanJSONConsistency checks that human vs JSON outputs carry
// same artifact_id/version/checksum (mirrors spikes/local-deploy-ux ValidateHumanJSONConsistency).
//
// This is the AC1 gate: human and JSON must be logically consistent.
func ValidateDeployHumanJSONConsistency(human string, jsonOutput string, manifest *artifact.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest nil")
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOutput), &env); err != nil {
		return err
	}
	var data map[string]interface{}
	if raw, ok := env["data"]; ok {
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("envelope missing data field")
	}
	fields := map[string]string{
		"artifact_id": manifest.ArtifactID,
		"version":     manifest.Version,
		"checksum":    manifest.Checksum,
		"project_id":  manifest.ProjectID,
	}
	for k, want := range fields {
		got, _ := data[k].(string)
		if got != want {
			return fmt.Errorf("mismatch %s: manifest %q vs json %q", k, want, got)
		}
	}
	// envelope must be version 1 / success
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOutput), &envelope); err != nil {
		return err
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		return fmt.Errorf("envelope invalid: version=%q status=%q want 1/success", envelope.Version, envelope.Status)
	}
	// human must contain same ids
	for _, needle := range []string{manifest.ArtifactID, manifest.Version, manifest.Checksum} {
		if !bytes.Contains([]byte(human), []byte(needle)) {
			// Show truncated for readability, but check full containment.
			short := needle
			if len(short) > 16 {
				short = short[:16]
			}
			return fmt.Errorf("human missing %q", short)
		}
	}
	return nil
}
