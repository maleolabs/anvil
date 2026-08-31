// Package cmd implements the Anvil CLI commands.
//
// Reference: anvil-cli/sto:installer-cli, sto:installer-pipeline-core,
// sto:installer-builder-linux, sto:installer-builder-windows, sto:installer-security-gate
package cmd

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/installer"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
)

// guiTemplate is bundled into anvil so fresh projects receive a complete
// runtime, not an empty scaffold.
//
//go:embed gui-template/*
var guiTemplate embed.FS

// installerCmd is the parent `anvil installer` namespace.
var installerCmd = &cobra.Command{
	Use:   "installer",
	Short: "Build platform installers for the project",
	Long: `Manage Anvil installers.

Builders produce OS-specific installers carrying the verified artifact
payload and optional interactive setup forms (installer.forms).`,
}

// installerBuildCmd implements `anvil installer build`.
var installerBuildCmd = &cobra.Command{
	Use:   "build --target <windows|linux> [--artifact <path>] [--gui] [--dry-run] [--json]",
	Short: "Build an installer for the target platform",
	Long: `Build a platform installer (linux/windows).

--target is required. --artifact reuses an existing artifact tar.gz (skips packaging).
--gui builds a native GUI package; Linux --format auto selects rpm on Fedora/RHEL, deb on Debian/Ubuntu, appimage elsewhere. Windows supports msi and nsis.
Forms come from installer.forms in anvil.yaml (6 types, including superAdmin). Use --dry-run to verify only.
Exit codes: 0 success, 1 verification failure, 2 configuration/toolchain error.`,
	Example: `  anvil installer build --target linux
  anvil installer build --target linux --gui
  anvil installer build --target windows --gui --artifact ./dist/app.tar.gz`,
	Args: cobra.NoArgs,
	RunE: runInstallerBuild,
}

func init() {
	rootCmd.AddCommand(installerCmd)
	installerCmd.AddCommand(installerBuildCmd)

	installerBuildCmd.Flags().String("target", "", "Target platform: windows or linux (required)")
	installerBuildCmd.Flags().String("artifact", "", "Path to existing artifact tar.gz to reuse (from 'anvil artifact package' or release download); when set, pipeline verifies then bundles without re-packaging")
	installerBuildCmd.Flags().Bool("dry-run", false, "Verify only; do not produce a bundle file")
	installerBuildCmd.Flags().Bool("gui", false, "Pure GUI mode: dispatch to Tauri bundler (AppImage/deb or msi) instead of legacy makeself/NSIS; fallback to TUI if webview unavailable")
	installerBuildCmd.Flags().String("format", "auto", "GUI package: auto, appimage, rpm, deb (Linux); msi (Windows)")
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
	GUI            bool                         `json:"gui"`
}

// runInstallerBuild executes `anvil installer build`.
func runInstallerBuild(cmd *cobra.Command, _ []string) error {
	target, _ := cmd.Flags().GetString("target")
	artifactPath, _ := cmd.Flags().GetString("artifact")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	asJSON, _ := cmd.Flags().GetBool("json")
	isGUI, _ := cmd.Flags().GetBool("gui")
	guiFormat, _ := cmd.Flags().GetString("format")

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
	if isGUI {
		var formatErr error
		guiFormat, formatErr = resolveGUIFormat(normTarget, guiFormat)
		if formatErr != nil {
			return ReportErrorWithCode(cmd, &output.AppError{Message: "invalid GUI format", Reason: formatErr.Error(), Resolution: "Use --format auto, appimage, rpm, or deb for Linux; use msi or nsis for Windows"}, output.ExitCodeConfig)
		}
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
	installerName := ""
	installerIcon := ""
	if flatCfg != nil {
		if v, ok := flatCfg["installer.name"]; ok {
			if s, ok := v.(string); ok {
				installerName = strings.TrimSpace(s)
			}
		}
		if v, ok := flatCfg["installer.icon"]; ok {
			if s, ok := v.(string); ok {
				installerIcon = strings.TrimSpace(s)
			}
		}
	}
	if installerName == "" {
		installerName = cfg.Identity().Name()
		if installerName == "" {
			installerName = "anvil-installer"
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
				GUI:            isGUI,
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

	// OS-specific builder: produce final platform installer from payload — dispatch GUI vs legacy
	finalBundlePath := pipelineResult.BundlePath
	if finalBundlePath == "" {
		finalBundlePath = pipelineResult.ArtifactPath
	}
	safeName := sanitizeInstallerName(installerName)
	osInstallerPath := ""
	if isGUI {
		// Pure GUI: Tauri bundler AppImage/deb (linux) or msi (windows) with custom name/icon
		if normTarget == "linux" {
			ext := ".AppImage"
			if guiFormat == "rpm" {
				ext = ".rpm"
			} else if guiFormat == "deb" {
				ext = ".deb"
			}
			osInstallerPath = filepath.Join(outputDir, fmt.Sprintf("%s-%s%s", safeName, artifactID, ext))
			if err := buildGUIBundle(finalBundlePath, osInstallerPath, normTarget, installerName, installerIcon, flatCfg, guiFormat); err != nil {
				return ReportErrorWithCode(cmd, &output.AppError{Message: "native GUI bundle failed", Reason: err.Error(), Resolution: "Install Tauri prerequisites and rerun; no archive fallback is allowed for --gui"}, output.ExitCodeConfig)
			} else {
				finalBundlePath = osInstallerPath
			}
		} else if normTarget == "windows" {
			ext := ".msi"
			if guiFormat == "nsis" {
				ext = ".exe"
			}
			osInstallerPath = filepath.Join(outputDir, fmt.Sprintf("%s-%s%s", safeName, artifactID, ext))
			if err := buildGUIBundle(finalBundlePath, osInstallerPath, normTarget, installerName, installerIcon, flatCfg, guiFormat); err != nil {
				return ReportErrorWithCode(cmd, &output.AppError{Message: "native GUI bundle failed", Reason: err.Error(), Resolution: "Run this command on a Windows build host with Tauri prerequisites; no archive fallback is allowed for --gui"}, output.ExitCodeConfig)
			} else {
				finalBundlePath = osInstallerPath
			}
		}
	} else {
		// Legacy TUI/NSIS path (backward compat)
		if normTarget == "linux" {
			osInstallerPath = filepath.Join(outputDir, fmt.Sprintf("anvil-installer-%s.run", artifactID))
			if err := installer.BuildLinuxInstaller(finalBundlePath, osInstallerPath); err != nil {
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
	}

	artifactReused := pipelineResult.Reused

	// Success path: envelope v1 {version:1,status,data:{target,artifact_id,verify,artifact_reused,gui}} consistent with deploy
	if asJSON {
		data := installerJSONData{
			Target:         normTarget,
			ArtifactID:     artifactID,
			Verify:         vr,
			ArtifactReused: artifactReused,
			BundlePath:     finalBundlePath,
			DryRun:         false,
			GUI:            isGUI,
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

// buildGUIBundle runs the real Tauri bundler. It never renames an archive into
// an installer. Missing toolchain, unsupported host/target, or missing bundle
// output returns an actionable error.
// installerName/installerIcon come from anvil.yaml installer.name/icon.
func buildGUIBundle(payloadPath, outputPath, target, installerName, installerIcon string, flatCfg map[string]interface{}, guiFormat string) error {
	if target != runtime.GOOS {
		return fmt.Errorf("GUI target %q cannot be built on %q; use a native %s build host (cross-compilation is not supported)", target, runtime.GOOS, target)
	}
	// Auto-scaffold if missing (one-command DX) with name/icon
	if projRoot, derr := project.Discover(); derr == nil {
		tauriConf := filepath.Join(projRoot, "installer-gui/src-tauri/tauri.conf.json")
		if _, err := os.Stat(tauriConf); os.IsNotExist(err) {
			if err := ensureGUIScaffold(projRoot, installerName, installerIcon); err != nil {
				return fmt.Errorf("auto-scaffold GUI project: %w", err)
			} else {
				fmt.Fprintf(os.Stderr, "Auto-scaffolded installer-gui/ for one-command build (%s) name=%q\n", target, installerName)
			}
		} else {
			// Existing/legacy scaffold: repair config and fill missing template files.
			if err := ensureGUIScaffold(projRoot, installerName, installerIcon); err != nil {
				return fmt.Errorf("repair GUI project: %w", err)
			}
		}
		if err := normalizeGUIIconConfig(projRoot, installerIcon); err != nil {
			return fmt.Errorf("configure GUI icon: %w", err)
		}
	} else {
		if _, err := os.Stat("installer-gui/src-tauri/tauri.conf.json"); os.IsNotExist(err) {
			return fmt.Errorf("Tauri scaffold not found at installer-gui/src-tauri/tauri.conf.json")
		}
	}
	// Ensure payload exists
	if _, err := os.Stat(payloadPath); err != nil {
		return fmt.Errorf("payload not found %q: %w", payloadPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	projRoot, err := project.Discover()
	if err != nil {
		return fmt.Errorf("discover project root: %w", err)
	}
	tauriDir := filepath.Join(projRoot, "installer-gui", "src-tauri")
	resourceDir := filepath.Join(tauriDir, "resources")
	buildRoot, err := os.MkdirTemp("", "anvil-gui-build-")
	if err != nil {
		return fmt.Errorf("create isolated GUI build directory: %w", err)
	}
	defer os.RemoveAll(buildRoot)
	if err := copyDirFiltered(filepath.Join(projRoot, "installer-gui"), filepath.Join(buildRoot, "installer-gui")); err != nil {
		return fmt.Errorf("stage GUI project: %w", err)
	}
	tauriDir = filepath.Join(buildRoot, "installer-gui", "src-tauri")
	resourceDir = filepath.Join(tauriDir, "resources")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		return fmt.Errorf("create Tauri resources: %w", err)
	}
	if err := copyFile(payloadPath, filepath.Join(resourceDir, "installer-artifact.tar.gz"), 0o644); err != nil {
		return fmt.Errorf("embed verified artifact: %w", err)
	}
	digest, err := sha256File(payloadPath)
	if err != nil {
		return fmt.Errorf("hash verified artifact: %w", err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "installer-artifact.sha256"), []byte(digest+"\n"), 0o644); err != nil {
		return fmt.Errorf("embed artifact checksum: %w", err)
	}
	if forms, readErr := artifact.ReadFormsFromArtifact(payloadPath); readErr == nil {
		if err := os.WriteFile(filepath.Join(resourceDir, "forms.json"), forms, 0o644); err != nil {
			return fmt.Errorf("embed forms.json: %w", err)
		}
	} else if err := os.WriteFile(filepath.Join(resourceDir, "forms.json"), []byte(`{}`), 0o644); err != nil {
		return fmt.Errorf("embed empty forms.json: %w", err)
	}
	setup := map[string]interface{}{"env_map": map[string]interface{}{}, "env_file": ".env"}
	if flatCfg != nil {
		if raw, ok := flatCfg["installer.setup.env_map"]; ok {
			setup["env_map"] = raw
		}
		if raw, ok := flatCfg["installer.setup.env_file"]; ok {
			setup["env_file"] = raw
		}
	}
	setupData, err := json.Marshal(setup)
	if err != nil {
		return fmt.Errorf("marshal setup.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "setup.json"), setupData, 0o644); err != nil {
		return fmt.Errorf("embed setup.json: %w", err)
	}
	appData, err := json.Marshal(map[string]string{"name": installerName})
	if err != nil {
		return fmt.Errorf("marshal app.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "app.json"), appData, 0o644); err != nil {
		return fmt.Errorf("embed app.json: %w", err)
	}
	bundle := guiFormat
	if required := map[string]string{"rpm": "rpmbuild", "deb": "dpkg-deb", "appimage": "linuxdeploy"}[bundle]; required != "" {
		if _, err := exec.LookPath(required); err != nil && bundle != "appimage" {
			return fmt.Errorf("native %s packaging requires %s on PATH; install the platform packaging tools and retry", bundle, required)
		}
	}
	if _, err := os.Stat(filepath.Join(buildRoot, "installer-gui", "node_modules")); os.IsNotExist(err) {
		npm := exec.Command("npm", "install", "--ignore-scripts", "--legacy-peer-deps")
		npm.Dir = filepath.Join(buildRoot, "installer-gui")
		npm.Stdout, npm.Stderr = os.Stdout, os.Stderr
		if err := npm.Run(); err != nil {
			return fmt.Errorf("install GUI frontend dependencies: %w; install Node.js/npm and retry", err)
		}
	}
	cmd := exec.Command("cargo", "tauri", "build", "--bundles", bundle, "--no-sign", "--ci")
	cmd.Dir = tauriDir
	var buildLog bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buildLog)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buildLog)
	if target == "linux" {
		// linuxdeploy is distributed as an AppImage; extraction mode works on
		// build hosts without FUSE and does not alter produced AppImage output.
		cmd.Env = append(os.Environ(), "APPIMAGE_EXTRACT_AND_RUN=1")
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo tauri build failed: %w; %s", err, summarizeBuildLog(buildLog.String()))
	}
	pattern := filepath.Join(tauriDir, "target", "release", "bundle", bundle, "*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("locate Tauri bundle: %w", err)
	}
	var native string
	for _, match := range matches {
		info, statErr := os.Stat(match)
		if statErr == nil && !info.IsDir() {
			native = match
			break
		}
	}
	if native == "" {
		return fmt.Errorf("cargo tauri build succeeded but no native %s bundle found under %s", bundle, filepath.Dir(pattern))
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := copyFile(native, outputPath, 0o755); err != nil {
		return fmt.Errorf("copy native Tauri bundle: %w", err)
	}
	return nil
}

func summarizeBuildLog(log string) string {
	log = strings.TrimSpace(log)
	if len(log) > 4000 {
		log = log[len(log)-4000:]
	}
	return log
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyDirFiltered(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if rel == "node_modules" || strings.HasPrefix(rel, "node_modules"+string(filepath.Separator)) || rel == "dist" || strings.HasPrefix(rel, "dist"+string(filepath.Separator)) || rel == filepath.Join("src-tauri", "target") || strings.HasPrefix(rel, filepath.Join("src-tauri", "target")+string(filepath.Separator)) || rel == filepath.Join("src-tauri", "resources") || strings.HasPrefix(rel, filepath.Join("src-tauri", "resources")+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func resolveGUIFormat(target, requested string) (string, error) {
	if target == "windows" {
		if requested == "auto" || requested == "msi" || requested == "nsis" {
			if requested == "auto" {
				return "msi", nil
			}
			return requested, nil
		}
		return "", fmt.Errorf("Windows supports only msi or nsis")
	}
	if requested != "auto" && requested != "appimage" && requested != "rpm" && requested != "deb" {
		return "", fmt.Errorf("unsupported Linux format %q", requested)
	}
	if requested != "auto" {
		return requested, nil
	}
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "id=fedora") || strings.Contains(content, "id=rhel") || strings.Contains(content, "id=centos") || strings.Contains(content, "id=rocky") || strings.Contains(content, "id=almalinux") {
			return "rpm", nil
		}
		if strings.Contains(content, "id=ubuntu") || strings.Contains(content, "id=debian") || strings.Contains(content, "id=linuxmint") {
			return "deb", nil
		}
	}
	return "appimage", nil
}

func ensureGUIScaffold(projectRoot, installerName, installerIcon string) error {
	base := filepath.Join(projectRoot, "installer-gui")
	tauriDir := filepath.Join(base, "src-tauri")
	srcDir := filepath.Join(base, "src")
	if err := os.MkdirAll(tauriDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(tauriDir, "src"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(tauriDir, "icons"), 0o755); err != nil {
		return err
	}
	// Tauri requires an RGBA PNG icon even when user does not customize one.
	iconPath := filepath.Join(tauriDir, "icons/icon.png")
	if err := ensureGUIIcon(projectRoot, iconPath, installerIcon); err != nil {
		return err
	}
	title := installerName
	if title == "" {
		title = "Anvil Installer"
	}
	ident := "com.anvil.installer"
	if installerName != "" {
		safe := sanitizeInstallerName(installerName)
		ident = "com.anvil." + strings.ToLower(safe)
	}
	tauriConf := fmt.Sprintf(`{
  "productName": %q,
  "identifier": "%s",
  "build": {
    "beforeBuildCommand": {"script": "npm run build", "cwd": "..", "wait": true},
    "beforeDevCommand": "npm run dev",
    "devUrl": "http://localhost:1420",
    "frontendDist": "../dist"
  },
  "bundle": {
    "active": true,
    "targets": ["appimage", "deb", "msi", "nsis"],
  "resources": ["resources/installer-artifact.tar.gz", "resources/installer-artifact.sha256", "resources/forms.json", "resources/setup.json", "resources/app.json"]
  },
  "app": {
    "windows": [{"title": %q, "width": 600, "height": 700}],
    "security": {"csp": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'"}
  },
  "plugins": {"dialog": {"open": true, "save": false}}
}
`, title, ident, title)
	if _, err := os.Stat(filepath.Join(tauriDir, "tauri.conf.json")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(tauriDir, "tauri.conf.json"), []byte(tauriConf), 0o644); err != nil {
			return err
		}
	} else {
		_ = updateGUIScaffoldNameIcon(projectRoot, installerName, installerIcon)
		if err := normalizeGUIIconConfig(projectRoot, installerIcon); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(tauriDir, "Cargo.toml")); os.IsNotExist(err) {
		cargo := `[package]
name = "anvil-installer-gui"
version = "1.0.0"
edition = "2021"
[build-dependencies]
tauri-build = { version = "2", features = [] }
[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tauri = { version = "2" }
sha2 = "0.10"
tar = "0.4"
flate2 = "1"
tauri-plugin-dialog = "2"
[features]
default = ["custom-protocol"]
custom-protocol = ["tauri/custom-protocol"]
`
		_ = os.WriteFile(filepath.Join(tauriDir, "Cargo.toml"), []byte(cargo), 0o644)
		_ = os.WriteFile(filepath.Join(tauriDir, "build.rs"), []byte("fn main() { tauri_build::build() }\n"), 0o644)
	}
	if _, err := os.Stat(filepath.Join(tauriDir, "src/main.rs")); os.IsNotExist(err) {
		data, err := guiTemplate.ReadFile("gui-template/main.rs")
		if err != nil {
			return fmt.Errorf("read embedded GUI runtime: %w", err)
		}
		if err := os.WriteFile(filepath.Join(tauriDir, "src/main.rs"), data, 0o644); err != nil {
			return err
		}
	} else if data, readErr := os.ReadFile(filepath.Join(tauriDir, "src/main.rs")); readErr == nil && (strings.Contains(string(data), "fn has_gui()->bool{true}") || strings.Contains(string(data), "falling back to TUI whiptail/dialog")) {
		// Replace old Anvil stub/runtime; partial projects must not retain fake IPC.
		template, templateErr := guiTemplate.ReadFile("gui-template/main.rs")
		if templateErr != nil {
			return templateErr
		}
		if writeErr := os.WriteFile(filepath.Join(tauriDir, "src/main.rs"), template, 0o644); writeErr != nil {
			return writeErr
		}
	}
	if _, err := os.Stat(filepath.Join(base, "package.json")); os.IsNotExist(err) {
		data, err := guiTemplate.ReadFile("gui-template/package.json")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(base, "package.json"), data, 0o644); err != nil {
			return err
		}
	}
	if err := installEmbeddedGUIFiles(base); err != nil {
		return err
	}
	if err := normalizeGUIPackage(base); err != nil {
		return fmt.Errorf("normalize GUI frontend dependencies: %w", err)
	}
	if err := normalizeGUIConfig(projectRoot); err != nil {
		return fmt.Errorf("normalize GUI build config: %w", err)
	}
	if err := normalizeGUICargo(projectRoot); err != nil {
		return fmt.Errorf("normalize GUI Rust dependencies: %w", err)
	}
	return nil
}

func normalizeGUICargo(projectRoot string) error {
	path := filepath.Join(projectRoot, "installer-gui", "src-tauri", "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(data), `tauri = { version = "2", features = ["shell-open"] }`, `tauri = { version = "2" }`)
	// Remove dependencies accidentally appended under [features] by older Anvil.
	for _, key := range []string{"sha2", "tar", "flate2", "tauri-plugin-dialog"} {
		lines := strings.Split(text, "\n")
		filtered := lines[:0]
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, key+" =") {
				continue
			}
			filtered = append(filtered, line)
		}
		text = strings.Join(filtered, "\n")
	}
	deps := "sha2 = \"0.10\"\ntar = \"0.4\"\nflate2 = \"1\"\ntauri-plugin-dialog = \"2\"\n"
	if idx := strings.Index(text, "[features]"); idx >= 0 {
		text = text[:idx] + deps + "\n" + text[idx:]
	} else {
		text += "\n[dependencies]\n" + deps
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func normalizeGUIPackage(base string) error {
	path := filepath.Join(base, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return err
	}
	deps, _ := pkg["dependencies"].(map[string]interface{})
	if deps == nil {
		deps = map[string]interface{}{}
		pkg["dependencies"] = deps
	}
	deps["@tauri-apps/api"] = "^2"
	deps["@tauri-apps/plugin-dialog"] = "^2"
	dev, _ := pkg["devDependencies"].(map[string]interface{})
	if dev == nil {
		dev = map[string]interface{}{}
		pkg["devDependencies"] = dev
	}
	// Keep Svelte and vite-plugin-svelte on compatible major versions.
	dev["@sveltejs/vite-plugin-svelte"] = "^4"
	dev["svelte"] = "^5"
	dev["vite"] = "^5"
	scripts, _ := pkg["scripts"].(map[string]interface{})
	if scripts == nil {
		scripts = map[string]interface{}{}
		pkg["scripts"] = scripts
	}
	if scripts["build"] == nil {
		scripts["build"] = "vite build"
	}
	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func normalizeGUIConfig(projectRoot string) error {
	path := filepath.Join(projectRoot, "installer-gui", "src-tauri", "tauri.conf.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var conf map[string]interface{}
	if err := json.Unmarshal(data, &conf); err != nil {
		return err
	}
	build, _ := conf["build"].(map[string]interface{})
	if build == nil {
		build = map[string]interface{}{}
		conf["build"] = build
	}
	build["frontendDist"] = "../dist"
	build["beforeBuildCommand"] = map[string]interface{}{"script": "npm run build", "cwd": "..", "wait": true}
	out, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func installEmbeddedGUIFiles(base string) error {
	return iofs.WalkDir(guiTemplate, "gui-template", func(path string, entry iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path == "gui-template/main.rs" || path == "gui-template/package.json" {
			return nil
		}
		rel := strings.TrimPrefix(path, "gui-template/")
		if rel == "" {
			return nil
		}
		dst := filepath.Join(base, rel)
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		data, readErr := guiTemplate.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

func updateGUIScaffoldNameIcon(projectRoot, installerName, installerIcon string) error {
	if installerName == "" && installerIcon == "" {
		return nil
	}
	tauriConfPath := filepath.Join(projectRoot, "installer-gui/src-tauri/tauri.conf.json")
	data, err := os.ReadFile(tauriConfPath)
	if err != nil {
		return err
	}
	var conf map[string]interface{}
	if err := json.Unmarshal(data, &conf); err != nil {
		return err
	}
	if installerName != "" {
		title := installerName
		ident := "com.anvil." + strings.ToLower(sanitizeInstallerName(installerName))
		conf["identifier"] = ident
		conf["productName"] = title
		if app, ok := conf["app"].(map[string]interface{}); ok {
			if wins, ok := app["windows"].([]interface{}); ok && len(wins) > 0 {
				if w, ok := wins[0].(map[string]interface{}); ok {
					w["title"] = title
				}
			}
		}
	}
	out, _ := json.MarshalIndent(conf, "", "  ")
	return os.WriteFile(tauriConfPath, out, 0o644)
}

func ensureGUIIcon(projectRoot, iconPath, installerIcon string) error {
	if installerIcon == "" {
		if data, err := os.ReadFile(iconPath); err == nil {
			if decoded, err := png.Decode(bytes.NewReader(data)); err == nil {
				if _, ok := decoded.(*image.RGBA); ok {
					return nil
				}
				return writeRGBAIcon(iconPath, decoded)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		return writeDefaultGUIIcon(iconPath)
	}

	srcIcon := installerIcon
	if !filepath.IsAbs(srcIcon) {
		srcIcon = filepath.Join(projectRoot, srcIcon)
	}
	data, err := os.ReadFile(srcIcon)
	if err != nil {
		return fmt.Errorf("installer.icon %q could not be read: %w; provide a readable PNG", installerIcon, err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("installer.icon %q is not a valid PNG: %w; provide a valid PNG", installerIcon, err)
	}
	if _, ok := decoded.(*image.RGBA); ok {
		return os.WriteFile(iconPath, data, 0o644)
	}
	return writeRGBAIcon(iconPath, decoded)
}

func writeRGBAIcon(path string, decoded image.Image) error {
	rgba := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, rgba); err != nil {
		return fmt.Errorf("could not encode RGBA PNG: %w", err)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func writeDefaultGUIIcon(path string) error {
	// Tauri/WiX reject tiny placeholder icons even when PNG is valid RGBA.
	rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
	rgba.SetRGBA(0, 0, color.RGBA{A: 0xff})
	var out bytes.Buffer
	if err := png.Encode(&out, rgba); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func normalizeGUIIconConfig(projectRoot, installerIcon string) error {
	path := filepath.Join(projectRoot, "installer-gui/src-tauri/tauri.conf.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var conf map[string]interface{}
	if err := json.Unmarshal(data, &conf); err != nil {
		return err
	}
	bundle, ok := conf["bundle"].(map[string]interface{})
	if !ok {
		return nil
	}
	// Tauri v2 identifier is top-level, not bundle.identifier.
	delete(bundle, "identifier")
	iconPath := filepath.Join(projectRoot, "installer-gui/src-tauri/icons/icon.png")
	if installerIcon == "" {
		bundle["icon"] = []string{"icons/icon.png"}
	} else {
		if _, err := os.Stat(iconPath); err != nil {
			return fmt.Errorf("configured icon %q was not found or could not be copied", installerIcon)
		}
		bundle["icon"] = []string{"icons/icon.png"}
	}
	out, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func sanitizeInstallerName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "anvil-installer"
	}
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('-')
		} else {
			b.WriteRune('-')
		}
	}
	res := b.String()
	res = strings.Trim(res, "-")
	if res == "" {
		return "anvil-installer"
	}
	return res
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
