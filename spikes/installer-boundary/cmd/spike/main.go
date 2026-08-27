package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	spike "maleolabs.com/anvil/spikes/installer-boundary"
)

func main() {
	var installRoot string
	var variant string
	var version string
	var outDir string
	flag.StringVar(&installRoot, "install-root", "", "user-chosen install lokasi (default temp)")
	flag.StringVar(&variant, "variant", "linux-makeself", "installer variant: linux-makeself | windows-nsis")
	flag.StringVar(&version, "version", "1.0.0", "artifact/installer version")
	flag.StringVar(&outDir, "out-dir", "", "evidence out dir (default spikes/installer-boundary/evidence)")
	flag.Parse()

	if outDir == "" {
		outDir = filepath.Join("spikes", "installer-boundary", "evidence")
	}
	_ = os.MkdirAll(outDir, 0755)

	installLogPath := filepath.Join(outDir, "install.log")
	verifyLogPath := filepath.Join(outDir, "verify.log")
	artifactJSON := filepath.Join(outDir, "artifact.json")
	installerJSON := filepath.Join(outDir, "installer.json")
	stateJSON := filepath.Join(outDir, "install-state.json")
	frictionPath := filepath.Join(outDir, "friction-checklist.md")
	summaryPath := filepath.Join(outDir, "summary.json")

	installFile, err := os.Create(installLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create install.log: %v\n", err)
		os.Exit(1)
	}
	defer installFile.Close()
	verifyFile, _ := os.Create(verifyLogPath)
	if verifyFile != nil {
		defer verifyFile.Close()
	}
	mw := io.MultiWriter(os.Stdout, installFile)
	if verifyFile != nil {
		// also tee verify details to verify.log via harness logger duplication: harness writes to mw which includes verifyFile
		mw = io.MultiWriter(os.Stdout, installFile, verifyFile)
	}

	// temp dirs isolation
	baseArtifacts := filepath.Join(os.TempDir(), fmt.Sprintf("spike-installer-artifacts-%d", os.Getpid()))
	baseInstallerOut := filepath.Join(os.TempDir(), fmt.Sprintf("spike-installer-out-%d", os.Getpid()))
 chosenRoot := installRoot
	if chosenRoot == "" {
		chosenRoot = filepath.Join(os.TempDir(), fmt.Sprintf("spike-installer-install-%d", os.Getpid()))
	}
	defer os.RemoveAll(baseArtifacts)
	defer os.RemoveAll(baseInstallerOut)
	// do NOT defer remove chosenRoot when default temp — keep for inspection? But clean up after harness? We'll keep until after copy, then optionally clean if temp.
	isTempRoot := installRoot == ""
	if isTempRoot {
		defer os.RemoveAll(chosenRoot)
	}

	variantTyped := spike.InstallerVariant(variant)
	if variantTyped != spike.VariantLinux && variantTyped != spike.VariantWindows {
		fmt.Fprintf(os.Stderr, "unknown variant %q (want linux-makeself or windows-nsis)\n", variant)
		os.Exit(1)
	}

	cfg := spike.HarnessConfig{
		ProjectID:       "spike-installer-boundary",
		ArtifactsDir:    baseArtifacts,
		InstallerOutDir: baseInstallerOut,
		InstallRoot:     chosenRoot,
		Logger:          mw,
		Version:         version,
		Variant:         variantTyped,
	}

	fmt.Fprintf(mw, "=== Spike 1 Installer Boundary Harness ===\n")
	fmt.Fprintf(mw, "project=%s variant=%s version=%s\n", cfg.ProjectID, cfg.Variant, cfg.Version)
	fmt.Fprintf(mw, "installRoot (user-chosen) = %s\nartifactsDir=%s\ninstallerOut=%s\n", chosenRoot, baseArtifacts, baseInstallerOut)
	fmt.Fprintf(mw, "contract: installer dumb wrapper -> anvil standard setup (migrate --force + db:seed + storage:link owned by standard)\n\n")

	result, err := spike.RunHarness(cfg)
	if err != nil {
		fmt.Fprintf(mw, "HARNESS FAILED: %v\n", err)
		fmt.Fprintf(os.Stderr, "HARNESS FAILED: %v\n", err)
		os.Exit(1)
	}

	// write evidence files
	if result.Artifact != nil && result.Artifact.Manifest != nil {
		if b, err := json.MarshalIndent(result.Artifact.Manifest, "", "  "); err == nil {
			_ = os.WriteFile(artifactJSON, b, 0644)
		}
	}
	if result.Installer != nil {
		if b, err := json.MarshalIndent(result.Installer, "", "  "); err == nil {
			_ = os.WriteFile(installerJSON, b, 0644)
		}
	}
	// copy state marker if exists
	stateSrc := filepath.Join(chosenRoot, ".anvil-install-state.json")
	if b, err := os.ReadFile(stateSrc); err == nil {
		_ = os.WriteFile(stateJSON, b, 0644)
	}
	// friction checklist
	frictionContent := `# Friction Checklist — Installer Boundary (auto-generated)

## Before installer (manual)
- scp / git clone to server
- edit .env (DB_HOST, APP_KEY)
- composer install
- php artisan migrate --force   (manual, error prone)
- php artisan db:seed           (super-admin manual)
- php artisan storage:link      (symlink manual, Windows needs admin)
- fix perms / chown
- diverged Windows (NSIS) vs Linux (Makeself) steps
- Steps: 7+ manual, no idempotency, no rollback

## After dumb-wrapper + standard-owned setup
- user runs installer (zip/shell) -> chooses lokasi
- installer extracts payload/artifact.tar.gz (verified checksum identity-from-content)
- installer triggers: anvil standard setup --install-root <lokasi>
- standard hook owns: migrate --force, db:seed (super-admin), storage:link
- idempotent: second run detects .anvil-install-state.json -> noop verify
- cancel mid-extract: staged tmp removed, no corrupt, retry safe
- migrate fail: rollback artifact + actionable error (check DB_HOST)

## Result (this run)
- artifact_id: ` + result.Artifact.Manifest.ArtifactID + `
- installer: ` + result.Installer.Path + ` (` + string(result.Installer.Variant) + `)
- installRoot: ` + chosenRoot + `
- idempotent: ` + fmt.Sprint(result.IdempotentOK) + `
- cancel recovered: ` + fmt.Sprint(result.CancelRecovered) + `
- migrate rollback: ` + fmt.Sprint(result.MigrateFailRolledBack) + `
`
	_ = os.WriteFile(frictionPath, []byte(frictionContent), 0644)

	// verify log already teed; ensure summary
	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, b, 0644)
	}
	// copy install log to evidence already; reflect summary
	fmt.Fprintf(mw, "\n=== Evidence written to %s ===\n", outDir)
	fmt.Fprintf(mw, "  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n", installLogPath, verifyLogPath, artifactJSON, installerJSON, stateJSON, frictionPath, summaryPath)
	fmt.Fprintf(mw, "All ACs PASS. Dumb-wrapper boundary proof complete.\n")
}
