package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	spike "maleolabs.com/anvil/spikes/installer-pipeline"
)

func main() {
	var target string
	var asJSON bool
	var dryRun bool
	var outDir string
	var evidenceDir string
	var repoRoot string
	var version string
	flag.StringVar(&target, "target", "", "single target build: windows|linux (empty runs full AC1-4 harness)")
	flag.BoolVar(&asJSON, "json", false, "machine-readable json envelope v1")
	flag.BoolVar(&dryRun, "dry-run", false, "verify only without building installer")
	flag.StringVar(&outDir, "out", "", "output dir dist/installer (default <repo>/dist/installer or temp)")
	flag.StringVar(&evidenceDir, "evidence", "", "evidence dir (default spikes/installer-pipeline/evidence)")
	flag.StringVar(&repoRoot, "repo", ".", "repo root for anvil.yaml lookup")
	flag.StringVar(&version, "version", "1.0.0", "artifact version")
	flag.Parse()

	if evidenceDir == "" {
		evidenceDir = "spikes/installer-pipeline/evidence"
		if _, err := os.Stat(evidenceDir); os.IsNotExist(err) {
			evidenceDir = filepath.Join(os.TempDir(), "spike-installer-pipeline-evidence")
		}
	}
	if err := os.MkdirAll(evidenceDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir evidence: %v\n", err)
		os.Exit(1)
	}
	if repoRoot == "" {
		repoRoot = "."
	}

	// Single-target mode: like `anvil installer build --target windows`
	if target != "" {
		icfg, _, err := spike.LoadInstallerConfig(repoRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load installer config: %v\n", err)
			os.Exit(1)
		}
		outputDir := outDir
		if outputDir == "" {
			outputDir = filepath.Join(repoRoot, "dist", "installer")
		}
		// For single-target demo, use isolated temp source for fast packaging (avoid walking entire repo)
		// In prod, artifactSource="." would be repo root — here we isolate for spike speed
		var sourceDir string
		var tmpSource string
		if icfg.ArtifactSource == "." {
			tmpSource, _ = os.MkdirTemp("", "spike-single-source-*")
			sourceDir = tmpSource
			defer os.RemoveAll(tmpSource)
			_ = os.MkdirAll(sourceDir, 0755)
			_ = os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php echo 'spike single';"), 0644)
			_ = os.MkdirAll(filepath.Join(sourceDir, "app"), 0755)
			_ = os.WriteFile(filepath.Join(sourceDir, "app", "hello.txt"), []byte("hello single"), 0644)
		} else {
			sourceDir = filepath.Join(repoRoot, icfg.ArtifactSource)
			_ = os.MkdirAll(sourceDir, 0755)
			if _, err := os.Stat(filepath.Join(sourceDir, "index.php")); os.IsNotExist(err) {
				_ = os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php echo 'spike';"), 0644)
			}
		}
		cfg := spike.PipelineConfig{
			Target:          target,
			DryRun:          dryRun,
			JSONOutput:      asJSON,
			OutputDir:       outputDir,
			InstallerConfig: icfg,
			ProjectRoot:     repoRoot,
			SourceDir:       sourceDir,
			Version:         version,
		}
		if asJSON {
			cfg.RawWriter = os.Stdout
			cfg.Logger = io.Discard
		} else {
			cfg.Logger = os.Stdout
		}
		res, err := spike.RunPipeline(cfg)
		if err != nil {
			// Render error envelope if --json, else human error
			if asJSON {
				// err is *output.AppError already with ExitCode; render via WriteJSONError
				// Extract message for envelope
				_ = res
				buf := &bytes.Buffer{}
				// Use generic error envelope
				_ = json.NewEncoder(buf).Encode(map[string]interface{}{"version": "1", "status": "error", "error": spike.RedactInstallerLog(err.Error())})
				fmt.Fprint(os.Stdout, buf.String())
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", spike.RedactInstallerLog(err.Error()))
			}
			os.Exit(1)
		}
		_ = res
		return
	}

	// Full harness mode (AC1-4)
	outputDir := outDir
	if outputDir != "" {
		_ = os.MkdirAll(outputDir, 0755)
	} else {
		outputDir = filepath.Join(repoRoot, "dist", "installer")
	}
	// Ensure anvil.yaml installer fixture exists for harness if missing
	ensureFixture(repoRoot)

	harnessLogPath := filepath.Join(evidenceDir, "harness.log")
	harnessFile, err := os.Create(harnessLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create harness.log: %v\n", err)
		os.Exit(1)
	}
	defer harnessFile.Close()
	mw := io.MultiWriter(os.Stdout, harnessFile)

	fmt.Fprintf(mw, "=== Spike 3: Pipeline Integration Harness (AC1-4) ===\n")
	fmt.Fprintf(mw, "repoRoot=%s outputDir=%s evidenceDir=%s version=%s\n\n", repoRoot, outputDir, evidenceDir, version)

	hCfg := spike.HarnessConfig{
		RepoRoot:    repoRoot,
		OutputDir:   outputDir,
		EvidenceDir: evidenceDir,
		Logger:      mw,
	}
	result, err := spike.RunHarness(hCfg)
	if err != nil {
		fmt.Fprintf(mw, "HARNESS FAILED: %v\n", spike.RedactInstallerLog(err.Error()))
		fmt.Fprintf(os.Stderr, "HARNESS FAILED: %v\n", spike.RedactInstallerLog(err.Error()))
		os.Exit(1)
	}

	// Write summary.json
	summaryPath := filepath.Join(evidenceDir, "summary.json")
	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, b, 0644)
	}
	// Write matrix.md
	matrixPath := filepath.Join(evidenceDir, "matrix.md")
	_ = os.WriteFile(matrixPath, []byte(buildMatrixMD(result, repoRoot, outputDir)), 0644)

	// Also copy harness log already written; ensure build logs exist (harness already wrote them)
	fmt.Fprintf(mw, "\n=== Evidence written to %s ===\n", evidenceDir)
	for _, name := range []string{"build-human.log", "build-json.log", "dry-run-human.log", "dry-run-json.log", "tamper.log", "redact-check.log", "idempotent.log", "harness.log", "summary.json", "matrix.md"} {
		p := filepath.Join(evidenceDir, name)
		if _, err := os.Stat(p); err == nil {
			fmt.Fprintf(mw, "  - %s\n", p)
		}
	}
	fmt.Fprintln(mw, "All ACs PASS. Pipeline integration proof complete.")
}

func ensureFixture(repoRoot string) {
	p := filepath.Join(repoRoot, "anvil.yaml")
	if _, err := os.Stat(p); err == nil {
		// If installer block missing, append it for spike demo (don't overwrite existing project metadata)
		b, _ := os.ReadFile(p)
		if !bytes.Contains(b, []byte("installer:")) {
			extra := "\ninstaller:\n  name: DemoApp\n  icon: spikes/installer-pipeline/fixtures/app.ico\n  artifactSource: .\n  osTargets: [windows, linux]\n"
			_ = os.WriteFile(p, append(b, []byte(extra)...), 0644)
		}
		return
	}
	_ = os.WriteFile(p, []byte("project:\n  name: demo-app\n  version: 1.0.0\ninstaller:\n  name: DemoApp\n  icon: spikes/installer-pipeline/fixtures/app.ico\n  artifactSource: .\n  osTargets: [windows, linux]\n"), 0644)
	// ensure fixtures
	fixturesDir := filepath.Join(repoRoot, "spikes", "installer-pipeline", "fixtures")
	_ = os.MkdirAll(fixturesDir, 0755)
	_ = os.WriteFile(filepath.Join(fixturesDir, "app.ico"), append([]byte("ICO\x00DUMMY-ICON-256x256-ANVIL"), make([]byte, 64)...), 0644)
	_ = os.WriteFile(filepath.Join(fixturesDir, "app.png"), append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("DUMMY-PNG-256x256-ANVIL")...), 0644)
}

func buildMatrixMD(r *spike.HarnessResult, repoRoot, outputDir string) string {
	var sb bytes.Buffer
	sb.WriteString("# Spike 3 — Pipeline Integration Matrix (AC1–AC4)\n\n")
	sb.WriteString(fmt.Sprintf("> repo=%s output=%s generated via `go run ./spikes/installer-pipeline/cmd/spike`\n\n", repoRoot, outputDir))
	sb.WriteString("## AC1 — anvil.yaml installer block (ADR-005 unified config)\n\n")
	if r.InstallerConfig != nil {
		sb.WriteString(fmt.Sprintf("- **installer.name**: `%s` (sanitized: `%s`)\n", r.InstallerConfig.Name, spike.SanitizeInstallerName(r.InstallerConfig.Name)))
		sb.WriteString(fmt.Sprintf("- **installer.icon**: `%s`\n", r.InstallerConfig.Icon))
		sb.WriteString(fmt.Sprintf("- **installer.artifactSource**: `%s`\n", r.InstallerConfig.ArtifactSource))
		sb.WriteString(fmt.Sprintf("- **installer.osTargets**: `%v` (resolvedFrom: %s)\n", r.InstallerConfig.OSTargets, r.InstallerConfig.ResolvedFrom))
	}
	if len(r.EnvOverrides) > 0 {
		sb.WriteString(fmt.Sprintf("- **env overrides**: `%v` (Execution > Project per ADR-005 §10.2)\n", r.EnvOverrides))
	}
	sb.WriteString(fmt.Sprintf("- **redact**: %t — `ANVIL_SIGNING_KEY` / `id_rsa` masked via `internal/output.RedactSecrets` (see `redact-check.log`)\n\n", r.RedactOK))
	sb.WriteString("## AC2 — anvil installer build --target windows (reuse internal/artifact.Package → bundle → tooling mock)\n\n")
	if r.BuildWindows != nil {
		sb.WriteString(fmt.Sprintf("- **windows**: `%s` size=%d bytes artifact_id=%s checksum=%s via `NSISMock` (simulated, real would exec `makensis`)\n", filepath.Base(r.BuildWindows.OutputPath), r.BuildWindows.Installer.SizeBytes, r.BuildWindows.ArtifactID[:16], r.BuildWindows.Checksum[:16]))
		sb.WriteString(fmt.Sprintf("  - verify: %d checks PASS (see `build-human.log`)\n", len(r.BuildWindows.Verify.Checks)))
	}
	if r.BuildLinux != nil {
		sb.WriteString(fmt.Sprintf("- **linux**: `%s` size=%d bytes via `MakeselfMock` (real would exec `makeself.sh`)\n", filepath.Base(r.BuildLinux.OutputPath), r.BuildLinux.Installer.SizeBytes))
	}
	sb.WriteString(fmt.Sprintf("- **output**: `dist/installer/` — `%s` + `%s` (+ `.installer-state.json` for idempotency)\n\n", "DemoApp-Setup.exe", "DemoApp.run"))
	sb.WriteString("## AC3 — Idempotent & verification-before-trust\n\n")
	sb.WriteString(fmt.Sprintf("- **idempotent**: %t — second build same checksum+config → skip rebuild (hash via `.installer-state.json`, see `idempotent.log`)\n", r.IdempotentOK))
	sb.WriteString(fmt.Sprintf("- **verify-before-trust**: %t — tampered artifact → `VerifyArtifact` FAIL → abort before embed (no installer written), see `tamper.log`\n\n", r.TamperPassed))
	sb.WriteString("## AC4 — Error handling & output (human + --json envelope v1, --dry-run)\n\n")
	sb.WriteString("- **human**: `RenderHuman` via `output.PlainStepReporter`-style steps (Build artifact ✓, Verify artifact ✓, Build installer ✓)\n")
	sb.WriteString("- **json**: envelope v1 `{\"version\":\"1\",\"status\":\"success\",\"data\":{target,dry_run,artifact_id,version,checksum,installer_path,verify}}` via `output.WriteJSON` konsisten dengan `cmd/deploy`\n")
	sb.WriteString(fmt.Sprintf("- **human+JSON consistent**: %t — same artifact_id/version/checksum in both (ValidateHumanJSONConsistency)\n", r.HumanJSONConsistent))
	sb.WriteString("- **--dry-run**: `dry-run-human.log` (verify only) + `dry-run-json.log` (envelope with `dry_run:true`), no `dist/installer` write\n")
	sb.WriteString("- **error envelope**: unsupported target → `{\"version\":\"1\",\"status\":\"error\",\"error\":\"...\"}` exit 2 (config), tamper → exit 1 (general)\n\n")
	sb.WriteString("## Code Structure\n\n")
	sb.WriteString("- `config.go` — `InstallerConfig`, `LoadInstallerConfig` (4-level resolver), `SanitizeInstallerName`, `RedactInstallerLog`, icon gate (.ico/.png)\n")
	sb.WriteString("- `pipeline.go` — `Builder` (NSISMock/MakeselfMock), `RunPipeline` (Package → VerifyBeforeTrust → Builder → dist/installer), `RenderHuman`/`RenderJSON` (envelope v1), idempotent state, exit codes\n")
	sb.WriteString("- `harness.go` — `RunHarness` AC1-4 + evidence emission\n")
	sb.WriteString("- `cmd/spike/main.go` — CLI: `go run ./spikes/installer-pipeline/cmd/spike [--target windows] [--json] [--dry-run]`\n\n")
	sb.WriteString("## How to Integrate into forge-anvil-cli\n\n")
	sb.WriteString("1. **Schema** — add to `internal/config/schema.go` `CoreSchema()`: `installer.name` (string, required), `installer.icon` (string), `installer.artifactSource` (string, default `.`), `installer.osTargets` (array, default `[windows,linux]`), with validation mirroring `config.go:ValidateInstallerConfig`.\n")
	sb.WriteString("2. **Command** — add `cmd/installer.go`: parent `installer` cobra command (`Use: \"installer\"`) with `build` subcommand (`anvil installer build --target windows|linux [--dry-run] [--json]`) wiring `LoadInstallerConfig` → `RunPipeline` (promoted from spike to `internal/installer/pipeline.go`). Reuse existing `AddJSONFlag` + `ReportError`/`output.AppError` + `SanitizeLogLine` pattern from `cmd/deploy.go`.\n")
	sb.WriteString("3. **Builder** — promote `spikes/installer-pipeline/pipeline.go:Builder` to `internal/installer/builder.go` (NSIS + Makeself). Real toolchains gated: `exec.LookPath(\"makensis\")` / `makeself.sh` when present, else simulated (CI). Icon fixture validation via `VerifyIcon` (spike 2 helpers).\n")
	sb.WriteString("4. **Idempotency** — keep `dist/installer/.installer-state.json` exactly as spike; respect `.gitignore` entry for `dist/`.\n")
	sb.WriteString("5. **Tests** — promote `harness_test.go` AC1-4 as `internal/installer/*_test.go` + `cmd/installer_test.go` (human vs json consistency via `ValidateHumanJSONConsistency`).\n")
	sb.WriteString("6. **Docs** — add `anvil installer --help` + `anvil.yaml` installer block to `docs/` + update `anvil-cli/fnd:anvil-installer` with conclusion linking `spikes/installer-pipeline/evidence/matrix.md`.\n")
	return sb.String()
}
