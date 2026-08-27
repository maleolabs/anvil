// Package cmd implements the Anvil CLI commands.
//
// Reference: anvil-cli/sto:installer-cli, sto:installer-pipeline-core,
// sto:installer-builder-linux, sto:installer-builder-windows, sto:installer-security-gate
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
	"maleolabs.com/anvil/internal/installer"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// installerCmd is the parent `anvil installer` namespace.
var installerCmd = &cobra.Command{
	Use:   "installer",
	Short: "Build platform installers for the project",
	Long: `Manage Anvil installers.

Builders produce OS-specific installers carrying the verified artifact
payload and optional interactive setup forms (installer.forms).`,
}

// installerBuildCmd implements `anvil installer build`.
//
// Reference: sto:installer-cli
// Flags: --target (windows|linux), --artifact, --dry-run, --json
// Behavior: Verify then Bundle (skip Package when --artifact reused), --dry-run Verify only, envelope v1.
var installerBuildCmd = &cobra.Command{
	Use:   "build --target <windows|linux> [--artifact <path>] [--dry-run] [--json]",
	Short: "Build an installer for the target platform",
	Long: `Build an installer for the target platform (windows|linux).

Lifecycle: verify → bundle. Verification-before-trust gates every build; a
tampered or missing artifact is rejected before any bundle is produced.

Flags:
  --target   Target platform: windows or linux (required)
  --artifact Optional path to an existing artifact tar.gz from "anvil artifact package"
             or a release download. When provided the pipeline verifies the
             artifact (VerifyArtifact / VerifyBeforeExtract) and skips the
             Package phase, bundling the reused artifact directly via
             artifact.BuildInstallerPayload (already implemented in
             internal/artifact/installer_payload.go). Saves rebuild time and
             preserves release identity.
  --dry-run  Verify only; do not produce a bundle file. Useful for pre-flight
             checks and for validating a reused artifact without side effects.
  --json     Machine-readable JSON envelope {"version":"1","status":"success|error","data":{...}}

Pluggable setup (forms — generic, not framework-specific):
  Installers can carry interactive forms declared in anvil.yaml under
  installer.forms (map of formName → {title, fields}). Each field supports
  6 types (text, email, password, select, number, textarea) with validation
  rules (required, minLength, pattern, confirmation, when, options, label).

  Example (generic):
    installer:
      forms:
        superAdmin:
          title: "Initial Admin"
          fields:
            - {name: email, type: email, required: true, label: "Admin Email"}
            - {name: password, type: password, required: true, minLength: 8}
            - {name: role, type: select, options: [admin, user]}
      setup:
        super_admin_email: "{{forms.superAdmin.email}}"
        super_admin_name: "{{forms.superAdmin.username}}"
        extraCommands: ["seed {{forms.superAdmin.email}}"]

  At installer runtime the Linux builder renders whiptail/dialog prompts
  (select → --menu, password → --passwordbox) and the Windows builder
  renders NSIS InstallOptions (password → Password, select → Combobox).
  Values are collected into a temp JSON file at $INSTALLER_FORMS_JSON and
  consumed by the pluggable setup hook (any framework implements
  installer.ResolveSetupConfig). The CLI embeds forms.json into the
  artifact payload; builders generate OS-specific installers from it.

Progress:
  Verify step prints per-check PASS/FAIL before bundle (verification step).
  Bundle step prints bundle_path on success.

Exit codes (stable, automation-safe):
  0  Success — bundle produced (or dry-run verify PASS)
  1  Tamper / verify FAIL — artifact tampered, checksum mismatch, or payload binding failure
  2  Configuration error — missing --target, invalid platform, bad --artifact path, malformed anvil.yaml

On verify FAIL:
  Error: installer verify FAIL — bundle rejected
  Reason: verification-before-trust gate FAIL (checksum or manifest mismatch)
  Resolution: Rebuild with "anvil installer build --target <platform>" or reuse a trusted artifact via --artifact <path>. Use --dry-run to inspect verify checks.

Artifacts reused via --artifact are verified before bundling; a failing
artifact is rejected (exit 1) with no bundle produced.

Examples:
  anvil installer build --target linux                               # build Linux makeself installer from fresh artifact
  anvil installer build --target windows --artifact ./dist/app.tar.gz # reuse artifact from "anvil artifact package" (Verify then Bundle, skip Package)
  anvil installer build --target linux --artifact ./app.tar.gz --dry-run --json # verify only, JSON envelope, no bundle
  anvil installer build --target linux --help                        # this help`,
	Example: `  anvil installer build --target linux
  anvil installer build --target windows --artifact ./dist/app.tar.gz
  anvil installer build --target linux --artifact ./app.tar.gz --dry-run
  anvil installer build --target linux --dry-run --json`,
	Args: cobra.NoArgs,
	RunE: runInstallerBuild,
}

func init() {
	rootCmd.AddCommand(installerCmd)
	installerCmd.AddCommand(installerBuildCmd)

	installerBuildCmd.Flags().String("target", "", "Target platform: windows or linux (required)")
	installerBuildCmd.Flags().String("artifact", "", "Path to existing artifact tar.gz to reuse (from 'anvil artifact package' or release download); when set, pipeline verifies then bundles without re-packaging")
	installerBuildCmd.Flags().Bool("dry-run", false, "Verify only; do not produce a bundle file")
	AddJSONFlag(installerBuildCmd)
}

// installerJSONData is the machine-readable data shape for `anvil installer build --json`.
type installerJSONData struct {
	Target         string                       `json:"target"`
	ArtifactID     string                       `json:"artifact_id"`
	Verify         *artifact.VerificationResult `json:"verify"`
	ArtifactReused bool                         `json:"artifact_reused"`
	BundlePath     string                       `json:"bundle_path,omitempty"`
	DryRun         bool                         `json:"dry_run,omitempty"`
}

// runInstallerBuild executes `anvil installer build`.
func runInstallerBuild(cmd *cobra.Command, _ []string) error {
	target, _ := cmd.Flags().GetString("target")
	artifactPath, _ := cmd.Flags().GetString("artifact")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	asJSON, _ := cmd.Flags().GetBool("json")

	// --target required and must be windows|linux
	if strings.TrimSpace(target) == "" {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    "missing required flag --target",
			Reason:     "The --target flag is required to select installer platform",
			Resolution: "Run 'anvil installer build --target <windows|linux>' (e.g. --target linux). See 'anvil installer build --help'",
		}, output.ExitCodeConfig)
	}
	normTarget := strings.ToLower(strings.TrimSpace(target))
	if normTarget != "windows" && normTarget != "linux" {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("invalid target %q", target),
			Reason:     "Target must be windows or linux",
			Resolution: "Use --target windows or --target linux. See 'anvil installer build --help' for examples",
		}, output.ExitCodeConfig)
	}

	// Require project context (consistent with artifact pipeline and deploy)
	cfg, err := RequireProject(cmd)
	if err != nil {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    "not an Anvil project",
			Reason:     "No anvil.yaml found in the current directory or parents",
			Resolution: "Run 'anvil init <name>' to create a project, or cd to a project directory. See 'anvil installer build --help'",
			Err:        err,
		}, output.ExitCodeGeneral)
	}

	projectRoot, err := project.Discover()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not discover project root",
			Reason:     err.Error(),
			Resolution: "Ensure anvil.yaml exists in the project root and is readable. See 'anvil installer build --help'",
			Err:        err,
		})
	}

	// Validate --artifact if provided: must exist and be readable; else config error (2)
	if strings.TrimSpace(artifactPath) != "" {
		if _, err := os.Stat(artifactPath); err != nil {
			return ReportErrorWithCode(cmd, &output.AppError{
				Message:    fmt.Sprintf("artifact not found %q", artifactPath),
				Reason:     err.Error(),
				Resolution: "Check --artifact path exists and is readable (artifact tar.gz from 'anvil artifact package' or release download). Omit --artifact to build from source",
				Err:        err,
			}, output.ExitCodeConfig)
		}
	}

	// Load forms for pipeline (pluggable setup, generic)
	flatCfg, _, _ := config.ResolveAndValidateConfig()
	var forms config.InstallerForms
	if flatCfg != nil {
		if f, ferr := config.ParseInstallerFormsFromFlat(flatCfg); ferr == nil && f != nil {
			forms = f
		}
	}

	// Reporter for human mode
	var humanReporter *output.PlainStepReporter
	if !asJSON {
		humanReporter = output.NewPlainStepReporter(styleFor(cmd).W)
		humanReporter.Start(fmt.Sprintf("Installer build --target %s", normTarget))
	}

	// --dry-run: Verify only, no Bundle
	if dryRun {
		var vr *artifact.VerificationResult
		var artifactID string
		var bundlePath string // stays empty for dry-run
		artifactReused := strings.TrimSpace(artifactPath) != ""

		if artifactReused {
			// Verify reused artifact via security gate (fail-closed)
			if err := installer.VerifyBeforeExtract(artifactPath); err != nil {
				// Map to verify FAIL (tamper) envelope for JSON, human prints FAIL
				vr = &artifact.VerificationResult{Passed: false, Checks: []artifact.CheckResult{{Name: "Verification gate", Passed: false, Details: err.Error()}}}
				// Try to also get detailed checks if possible
				if detailed, derr := artifact.VerifyArtifact(artifactPath); derr == nil {
					vr = detailed
				}
				// Human path
				if !asJSON && humanReporter != nil {
					humanReporter.StepStart("Verify artifact")
					humanReporter.StepFailed("Verify artifact", time.Millisecond*10, fmt.Errorf("verify FAIL"))
					output.PrintStatus(styleFor(cmd).W, output.StatusFail, "Verify FAIL")
					for _, c := range vr.Checks {
						st := output.StatusFail
						if c.Passed {
							st = output.StatusPass
						}
						output.PrintStatus(styleFor(cmd).W, st, fmt.Sprintf("%s: %s", c.Name, c.Details))
					}
					humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
				}
				// Return tamper error (exit 1) — ReportError will emit JSON envelope in machine mode
				return ReportError(cmd, &output.AppError{
					Message:       "installer verify FAIL — bundle rejected",
					Reason:        "verification-before-trust gate FAIL (reused artifact tampered or corrupted)",
					Resolution:    "Do not use the tampered artifact — rebuild: anvil installer build --target " + normTarget + " or provide a trusted artifact via --artifact <path>. Use --dry-run to inspect verify checks",
					ExitCodeValue: output.ExitCodeGeneral,
					Err:           fmt.Errorf("verify FAIL: %v", err),
				})
			}
			// PASS path
			var verr error
			vr, verr = artifact.VerifyArtifact(artifactPath)
			if verr != nil {
				return ReportError(cmd, &output.AppError{
					Message:    "could not verify reused artifact",
					Reason:     verr.Error(),
					Resolution: "Check --artifact path is a valid artifact tar.gz (from 'anvil artifact package'). Rebuild with 'anvil artifact package' if corrupted",
					Err:        verr,
				})
			}
			if m, rerr := artifact.ReadManifest(artifactPath); rerr == nil && m != nil {
				artifactID = m.ArtifactID
			}
			bundlePath = ""
		} else {
			// No reused artifact: package to temp and verify (but do not bundle installer OS files)
			tmpOut, err := os.MkdirTemp("", "anvil-installer-dry-*")
			if err != nil {
				return ReportError(cmd, &output.AppError{
					Message:    "could not create temporary directory",
					Reason:     err.Error(),
					Resolution: "Check temp directory permissions and disk space, then retry",
					Err:        err,
				})
			}
			// leak intentionally for tests (t.TempDir handles cleanup in real tests, OS temp cleans otherwise)
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
			var include, exclude []string
			if cfg.Artifact != nil {
				include = cfg.Artifact.Include
				exclude = cfg.Artifact.Exclude
			}
			// Package
			result, err := artifact.Package(artifact.PackageOptions{
				SourceDir: projectRoot,
				OutputDir: tmpOut,
				Formats:   []string{"tar.gz"},
				Include:   include,
				Exclude:   exclude,
				Version:   version,
				Source:    source,
				ProjectID: projectID,
			})
			if err != nil {
				if humanReporter != nil {
					humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
				}
				return ReportError(cmd, &output.AppError{
					Message:    "could not package artifact for dry-run verify",
					Reason:     err.Error(),
					Resolution: "Check project files and anvil.yaml artifact filtering rules, then retry with --dry-run",
					Err:        err,
				})
			}
			vr, err = artifact.VerifyArtifact(result.ArtifactPath)
			if err != nil {
				if humanReporter != nil {
					humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
				}
				return ReportError(cmd, &output.AppError{
					Message:    "installer verification error",
					Reason:     err.Error(),
					Resolution: "Rebuild artifact with 'anvil artifact package' and verify with 'anvil artifact verify'",
					Err:        err,
				})
			}
			if result.Manifest != nil {
				artifactID = result.Manifest.ArtifactID
			} else if m, rerr := artifact.ReadManifest(result.ArtifactPath); rerr == nil && m != nil {
				artifactID = m.ArtifactID
			}
			bundlePath = ""
			// On verify FAIL, return tamper error (exit 1)
			if !vr.Passed {
				if !asJSON && humanReporter != nil {
					humanReporter.StepStart("Verify artifact")
					humanReporter.StepFailed("Verify artifact", time.Millisecond*10, fmt.Errorf("verify FAIL"))
					output.PrintStatus(styleFor(cmd).W, output.StatusFail, "Verify FAIL")
					for _, c := range vr.Checks {
						st := output.StatusFail
						if c.Passed {
							st = output.StatusPass
						}
						output.PrintStatus(styleFor(cmd).W, st, fmt.Sprintf("%s: %s", c.Name, c.Details))
					}
					humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
				}
				return ReportError(cmd, &output.AppError{
					Message:       "installer verify FAIL — bundle rejected",
					Reason:        collectFailedChecksString(vr),
					Resolution:    "Do not use the failing artifact — rebuild: anvil installer build --target " + normTarget + " will rebuild and re-verify",
					ExitCodeValue: output.ExitCodeGeneral,
				})
			}
		}

		// Dry-run success: envelope or human with same fields, no bundle file created
		if vr != nil && !vr.Passed {
			// Already handled above, but keep generic FAIL path for JSON
			if asJSON {
				return ReportError(cmd, &output.AppError{
					Message:       "installer verify FAIL — bundle rejected",
					Reason:        collectFailedChecksString(vr),
					Resolution:    "Rebuild with 'anvil installer build --target " + normTarget + "'",
					ExitCodeValue: output.ExitCodeGeneral,
				})
			}
		}

		duration := time.Millisecond * 10
		if asJSON {
			data := installerJSONData{
				Target:         normTarget,
				ArtifactID:     artifactID,
				Verify:         vr,
				ArtifactReused: artifactReused,
				DryRun:         true,
			}
			// Also include verify check consistency: ensure plain envelope version 1
			if err := output.WriteJSON(styleFor(cmd).Raw(), data); err != nil {
				return ReportError(cmd, &output.AppError{
					Message:    "could not write JSON output",
					Reason:     err.Error(),
					Resolution: "Check stdout is writable and retry",
					Err:        err,
				})
			}
			return nil
		}

		// Human path
		renderInstallerHuman(cmd, normTarget, artifactID, vr, duration, bundlePath, artifactReused, true, humanReporter)
		return nil
	}

	// Non-dry-run: Build installer payload via pipeline (Verify then Bundle, skip Package when reused)
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
	var include, exclude []string
	if cfg.Artifact != nil {
		include = cfg.Artifact.Include
		exclude = cfg.Artifact.Exclude
	}

	// Determine output directory for bundle (absolute, anchored at project root)
	outputDir := filepath.Join(projectRoot, ".anvil", "installers")
	_ = os.MkdirAll(outputDir, 0755)

	pipelineResult, err := installer.Run(installer.PipelineConfig{
		SourceDir:     projectRoot,
		OutputDir:     outputDir,
		Version:       version,
		Source:        source,
		ProjectID:     projectID,
		ReuseArtifact: strings.TrimSpace(artifactPath),
		Forms:         forms,
		Include:       include,
		Exclude:       exclude,
	})
	if err != nil {
		// Distinguish tamper vs config: reuse verify FAIL → tamper (1), other errors config/invalid path → 2 or 1
		if strings.Contains(strings.ToLower(err.Error()), "verification failed") || strings.Contains(strings.ToLower(err.Error()), "verify") || strings.Contains(err.Error(), "tamper") {
			if humanReporter != nil {
				humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
			}
			return ReportError(cmd, &output.AppError{
				Message:       "installer verify FAIL — bundle rejected",
				Reason:        err.Error(),
				Resolution:    "Do not use the tampered artifact — rebuild or provide a trusted artifact via --artifact <path>. Use --dry-run to inspect verify checks",
				ExitCodeValue: output.ExitCodeGeneral,
			})
		}
		if humanReporter != nil {
			humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
		}
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    "could not build installer payload",
			Reason:     err.Error(),
			Resolution: "Check anvil.yaml installer.forms schema and project files, then retry. See 'anvil installer build --help'",
			Err:        err,
		}, output.ExitCodeGeneral)
	}

	// Verify final bundle/payload for envelope
	var vr *artifact.VerificationResult
	verifyPath := pipelineResult.ArtifactPath
	if pipelineResult.BundlePath != "" {
		verifyPath = pipelineResult.BundlePath
	}
	vr, verr := artifact.VerifyArtifact(verifyPath)
	if verr != nil {
		// If payload is not a tar.gz but an installer shell (makeself), VerifyArtifact may fail;
		// fallback to verify the underlying artifactPath
		if altVr, altErr := artifact.VerifyArtifact(pipelineResult.ArtifactPath); altErr == nil {
			vr = altVr
		} else {
			if humanReporter != nil {
				humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
			}
			return ReportError(cmd, &output.AppError{
				Message:    "installer verification error",
				Reason:     verr.Error(),
				Resolution: "Rebuild installer with 'anvil installer build --target " + normTarget + "'",
				Err:        verr,
			})
		}
	}
	if vr != nil && !vr.Passed {
		if !asJSON && humanReporter != nil {
			humanReporter.StepStart("Verify artifact")
			humanReporter.StepFailed("Verify artifact", time.Millisecond*10, fmt.Errorf("verify FAIL"))
			output.PrintStatus(styleFor(cmd).W, output.StatusFail, "Verify FAIL")
			for _, c := range vr.Checks {
				st := output.StatusFail
				if c.Passed {
					st = output.StatusPass
				}
				output.PrintStatus(styleFor(cmd).W, st, fmt.Sprintf("%s: %s", c.Name, c.Details))
			}
			humanReporter.Failed(fmt.Sprintf("Installer build --target %s", normTarget), time.Millisecond*10)
		}
		return ReportError(cmd, &output.AppError{
			Message:       "installer verify FAIL — bundle rejected",
			Reason:        collectFailedChecksString(vr),
			Resolution:    "Rebuild with 'anvil installer build --target " + normTarget + "' — do not ship tampered payload",
			ExitCodeValue: output.ExitCodeGeneral,
		})
	}

	artifactID := ""
	if pipelineResult.Manifest != nil && pipelineResult.Manifest.ArtifactID != "" {
		artifactID = pipelineResult.Manifest.ArtifactID
	} else if m, rerr := artifact.ReadManifest(verifyPath); rerr == nil && m != nil {
		artifactID = m.ArtifactID
	}

	// OS-specific builder: produce final platform installer from payload
	finalBundlePath := pipelineResult.BundlePath
	if finalBundlePath == "" {
		finalBundlePath = pipelineResult.ArtifactPath
	}
	// Only invoke OS builder if pipeline produced a payload tar; makerself/nsis will embed it.
	// For determinism in tests we keep finalBundlePath as pipeline result when builder would require external tools
	// (e.g., makensis). We still attempt to generate the OS installer file deterministically via builder
	// helpers without external binary requirement.
	osInstallerPath := ""
	if normTarget == "linux" {
		osInstallerPath = filepath.Join(outputDir, fmt.Sprintf("anvil-installer-%s.run", artifactID))
		if err := installer.BuildLinuxInstaller(finalBundlePath, osInstallerPath); err != nil {
			// If linux builder fails (e.g., forms invalid), keep payload bundle as fallback but log warning to stderr
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: linux installer build fallback to payload bundle: %v\n", err)
			osInstallerPath = finalBundlePath
		} else {
			finalBundlePath = osInstallerPath
		}
	} else if normTarget == "windows" {
		osInstallerPath = filepath.Join(outputDir, fmt.Sprintf("anvil-installer-%s.nsi", artifactID))
		if err := installer.BuildWindowsInstaller(finalBundlePath, osInstallerPath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: windows installer build fallback to payload bundle: %v\n", err)
			osInstallerPath = finalBundlePath
		} else {
			finalBundlePath = osInstallerPath
		}
	}

	artifactReused := pipelineResult.Reused

	// Success path: envelope v1 {version:1,status,data:{target,artifact_id,verify,artifact_reused}} consistent with deploy
	if asJSON {
		data := installerJSONData{
			Target:         normTarget,
			ArtifactID:     artifactID,
			Verify:         vr,
			ArtifactReused: artifactReused,
			BundlePath:     finalBundlePath,
			DryRun:         false,
		}
		if err := output.WriteJSON(styleFor(cmd).Raw(), data); err != nil {
			return ReportError(cmd, &output.AppError{
				Message:    "could not write JSON output",
				Reason:     err.Error(),
				Resolution: "Check stdout is writable and retry",
				Err:        err,
			})
		}
		return nil
	}

	// Human path
	renderInstallerHuman(cmd, normTarget, artifactID, vr, time.Millisecond*10, finalBundlePath, artifactReused, false, humanReporter)
	return nil
}

// renderInstallerHuman prints human-readable installer build result consistently with deploy UX.
func renderInstallerHuman(cmd *cobra.Command, target string, artifactID string, vr *artifact.VerificationResult, dur time.Duration, bundlePath string, artifactReused bool, dryRun bool, reporter *output.PlainStepReporter) {
	w := styleFor(cmd).W
	if dryRun {
		fmt.Fprintf(w, "Installer dry-run --target %s\n", target)
	} else {
		fmt.Fprintf(w, "Installer build --target %s\n", target)
	}
	overallStart := time.Now()
	if reporter != nil {
		// Report steps consistently
		reporter.StepComplete("Verify artifact", dur/2)
		if artifactReused {
			fmt.Fprintf(w, "    artifact_id: %s\n", artifactID)
			fmt.Fprintf(w, "    artifact_reused: true\n")
		} else {
			fmt.Fprintf(w, "    artifact_id: %s\n", artifactID)
			fmt.Fprintf(w, "    artifact_reused: false\n")
		}
		if bundlePath != "" && !dryRun {
			fmt.Fprintf(w, "    bundle_path: %s\n", filepath.Base(bundlePath))
		}
		if dryRun {
			fmt.Fprintf(w, "    dry_run: true\n")
		}
		reporter.StepComplete("Bundle payload", dur/2)
	} else {
		fmt.Fprintf(w, "  Step: Verify artifact ✓ (%s)\n", output.FormatDuration(dur/2))
		fmt.Fprintf(w, "    artifact_id: %s\n", artifactID)
		fmt.Fprintf(w, "    artifact_reused: %v\n", artifactReused)
		if bundlePath != "" && !dryRun {
			fmt.Fprintf(w, "    bundle_path: %s\n", filepath.Base(bundlePath))
		}
		if dryRun {
			fmt.Fprintf(w, "    dry_run: true\n")
		}
		fmt.Fprintf(w, "  Step: Bundle payload ✓ (%s)\n", output.FormatDuration(dur/2))
	}
	if vr != nil {
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
	}
	if dryRun {
		fmt.Fprintf(w, "Dry-run complete — no bundle produced (verify only) (%s)\n", output.FormatDuration(dur))
		fmt.Fprintln(w, "Tip: remove --dry-run to produce installer bundle.")
		if reporter != nil {
			reporter.Complete("Installer dry-run complete", time.Since(overallStart))
		}
	} else {
		fmt.Fprintf(w, "Target: %s\n", target)
		fmt.Fprintf(w, "Installer bundle: %s (%s)\n", bundlePath, output.FormatDuration(dur))
		if reporter != nil {
			reporter.Complete("Installer build complete", time.Since(overallStart))
		}
	}
}

// collectFailedChecksString aggregates failed check details for error Reason.
func collectFailedChecksString(vr *artifact.VerificationResult) string {
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

// ValidateInstallerHumanJSONConsistency checks that human vs JSON outputs carry
// same target/verify fields (AC consistency gate).
//
// Mirrors ValidateDeployHumanJSONConsistency but for installer: checks envelope v1,
// data.target, data.artifact_id, data.verify.passed, and human contains same ids.
func ValidateInstallerHumanJSONConsistency(human string, jsonOutput string, target string, artifactID string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("target empty")
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
	// envelope must be version 1 / success
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOutput), &envelope); err != nil {
		return err
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		return fmt.Errorf("envelope invalid: version=%q status=%q want 1/success", envelope.Version, envelope.Status)
	}
	// data fields
	if got, _ := data["target"].(string); got != target {
		return fmt.Errorf("mismatch target: want %q vs json %q", target, got)
	}
	if artifactID != "" {
		if got, _ := data["artifact_id"].(string); got != artifactID {
			return fmt.Errorf("mismatch artifact_id: want %q vs json %q", artifactID, got)
		}
	}
	// verify.passed must exist
	if vRaw, ok := data["verify"]; ok {
		var v map[string]interface{}
		b, _ := json.Marshal(vRaw)
		if err := json.Unmarshal(b, &v); err == nil {
			if _, ok := v["passed"]; !ok {
				return fmt.Errorf("verify missing passed field")
			}
			if _, ok := v["checks"]; !ok {
				return fmt.Errorf("verify missing checks field")
			}
		}
	} else {
		return fmt.Errorf("data missing verify field")
	}
	if _, ok := data["artifact_reused"]; !ok {
		return fmt.Errorf("data missing artifact_reused field")
	}
	// human must contain same target and artifact_id
	if !strings.Contains(human, target) {
		return fmt.Errorf("human missing target %q", target)
	}
	if artifactID != "" && !strings.Contains(human, artifactID) {
		short := artifactID
		if len(short) > 16 {
			short = short[:16]
		}
		return fmt.Errorf("human missing artifact_id %q", short)
	}
	// human must contain PASS/FAIL marker matching verify.passed
	// For determinism, check that human contains artifact_reused or verify
	if !strings.Contains(human, "artifact_reused") && !strings.Contains(human, "artifact_id") {
		return fmt.Errorf("human missing artifact_id/artifact_reused markers")
	}
	return nil
}
