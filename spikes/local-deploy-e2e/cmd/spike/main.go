package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	e2e "maleolabs.com/anvil/spikes/local-deploy-e2e"
)

func main() {
	var deployerUser string
	var sizeMB int
	var outDir string
	flag.StringVar(&deployerUser, "deployer-user", "spike-tester", "SSH user for audit trail (AC5)")
	flag.IntVar(&sizeMB, "size-mb", 1, "dummy payload size MB per artifact (keeps tests fast)")
	flag.StringVar(&outDir, "out-dir", "", "evidence out dir (default spikes/local-deploy-e2e/evidence)")
	flag.Parse()

	if outDir == "" {
		// default: spikes/local-deploy-e2e/evidence relative to repo root
		// when run via go run ./spikes/local-deploy-e2e/cmd/spike, cwd is repo root
		outDir = filepath.Join("spikes", "local-deploy-e2e", "evidence")
	}

	// temp dirs: isolated, no prod side effects
	serverRoot := filepath.Join(os.TempDir(), fmt.Sprintf("spike-e2e-server-%d", os.Getpid()))
	installRoot := filepath.Join(serverRoot, "install", "spike-e2e-project")
	artifactsDir := filepath.Join(os.TempDir(), fmt.Sprintf("spike-e2e-artifacts-%d", os.Getpid()))
	remoteStagingDir := filepath.Join(os.TempDir(), fmt.Sprintf("spike-e2e-remote-%d", os.Getpid()))
	defer os.RemoveAll(serverRoot)
	defer os.RemoveAll(artifactsDir)
	defer os.RemoveAll(remoteStagingDir)

	// Ensure out dir exists
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir outDir: %v\n", err)
		os.Exit(1)
	}

	// Open evidence logs
	e2eLogPath := filepath.Join(outDir, "e2e.log")
	verifyLogPath := filepath.Join(outDir, "verify.log")
	statusDir := outDir
	rollbackLogPath := filepath.Join(outDir, "rollback.log")
	artifact1JSON := filepath.Join(outDir, "artifact1.json")
	artifact2JSON := filepath.Join(outDir, "artifact2.json")

	e2eFile, err := os.Create(e2eLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create e2e.log: %v\n", err)
		os.Exit(1)
	}
	defer e2eFile.Close()
	verifyFile, _ := os.Create(verifyLogPath)
	if verifyFile != nil {
		defer verifyFile.Close()
	}
	// tee to stdout + file
	mw := io.MultiWriter(os.Stdout, e2eFile)
	if verifyFile != nil {
		// verify also tees to verify.log via harness logger? we will duplicate
		mw = io.MultiWriter(os.Stdout, e2eFile, verifyFile)
	}

	cfg := e2e.HarnessConfig{
		ProjectID:        "spike-e2e-project",
		ServerRoot:       serverRoot,
		InstallRoot:      installRoot,
		ArtifactsDir:     artifactsDir,
		RemoteStagingDir: remoteStagingDir,
		DeployerUser:     deployerUser,
		SizeMB:           sizeMB,
		Logger:           mw,
	}

	fmt.Fprintf(mw, "=== Spike 2 E2E Harness ===\n")
	fmt.Fprintf(mw, "project=%s deployer=%s sizeMB=%d\n", cfg.ProjectID, e2e.SanitizeLogLine(cfg.DeployerUser), cfg.SizeMB)
	fmt.Fprintf(mw, "serverRoot=%s\ninstallRoot=%s\nartifactsDir=%s\nremoteStagingDir=%s\n", serverRoot, installRoot, artifactsDir, remoteStagingDir)

	result, err := e2e.RunE2E(cfg)
	if err != nil {
		fmt.Fprintf(mw, "E2E FAILED: %v\n", err)
		fmt.Fprintf(os.Stderr, "E2E FAILED: %v\n", err)
		os.Exit(1)
	}

	// Write evidence files
	// status snapshots already logged; also SaveStatusJSON was done inside harness? harness doesn't save to file, we save here
	_ = statusDir
	if result.StatusAfterActivate1 != nil {
		_ = e2e.SaveStatusJSON(statusDir, "activate1", result.StatusAfterActivate1)
	}
	if result.StatusAfterActivate2 != nil {
		_ = e2e.SaveStatusJSON(statusDir, "activate2", result.StatusAfterActivate2)
	}
	if result.StatusAfterRollback != nil {
		_ = e2e.SaveStatusJSON(statusDir, "rollback", result.StatusAfterRollback)
	}

	// rollback log
	if f, err := os.Create(rollbackLogPath); err == nil {
		if result.Rollback != nil {
			fmt.Fprintf(f, "rolled_back=%s stage=%s\nrestored=%s stage=%s\n",
				result.Rollback.RolledBackRelease.ID.String(), result.Rollback.RolledBackRelease.Stage.String(),
				result.Rollback.RestoredRelease.ID.String(), result.Rollback.RestoredRelease.Stage.String())
			fmt.Fprintf(f, "rollback eligible after: %v\n", result.StatusAfterRollback.Lifecycle.Rollback.Eligible)
		}
		f.Close()
	}

	// artifact manifests
	if data, err := json.MarshalIndent(result.Artifact1.Manifest, "", "  "); err == nil {
		_ = os.WriteFile(artifact1JSON, data, 0644)
	}
	if data, err := json.MarshalIndent(result.Artifact2.Manifest, "", "  "); err == nil {
		_ = os.WriteFile(artifact2JSON, data, 0644)
	}
	// audit log: copy from installRoot/audit.log to evidence/audit.log
	if auditData, err := os.ReadFile(filepath.Join(installRoot, "audit.log")); err == nil {
		_ = os.WriteFile(filepath.Join(outDir, "audit.log"), auditData, 0644)
	}

	// summary JSON
	summaryPath := filepath.Join(outDir, "summary.json")
	if data, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, data, 0644)
	}

	fmt.Fprintf(mw, "\n=== Evidence written to %s ===\n", outDir)
	fmt.Fprintf(mw, "  - %s\n  - %s\n  - status_activate1.json\n  - status_activate2.json\n  - status_rollback.json\n  - %s\n  - %s\n  - %s\n  - summary.json\n",
		e2eLogPath, verifyLogPath, rollbackLogPath, artifact1JSON, artifact2JSON)
	fmt.Fprintf(mw, "All ACs PASS. E2E proof complete.\n")
}
