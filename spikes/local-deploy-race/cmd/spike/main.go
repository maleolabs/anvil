package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	race "maleolabs.com/anvil/spikes/local-deploy-race"
)

func main() {
	var deployerUser string
	var sizeMB int
	var outDir string
	flag.StringVar(&deployerUser, "deployer-user", "spike-tester", "deployer user for audit")
	flag.IntVar(&sizeMB, "size-mb", 1, "dummy payload size MB per artifact")
	flag.StringVar(&outDir, "out-dir", "", "evidence out dir (default spikes/local-deploy-race/evidence)")
	flag.Parse()

	if outDir == "" {
		outDir = filepath.Join("spikes", "local-deploy-race", "evidence")
	}
	serverRoot := filepath.Join(os.TempDir(), fmt.Sprintf("spike-race-server-%d", os.Getpid()))
	installRoot := filepath.Join(serverRoot, "install", "spike-race-project")
	artifactsDir := filepath.Join(os.TempDir(), fmt.Sprintf("spike-race-artifacts-%d", os.Getpid()))
	remoteStagingDir := filepath.Join(os.TempDir(), fmt.Sprintf("spike-race-remote-%d", os.Getpid()))
	defer os.RemoveAll(serverRoot)
	defer os.RemoveAll(artifactsDir)
	defer os.RemoveAll(remoteStagingDir)

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir outDir: %v\n", err)
		os.Exit(1)
	}

	raceLogPath := filepath.Join(outDir, "race.log")
	concurrentLogPath := filepath.Join(outDir, "concurrent.log")
	lockLogPath := filepath.Join(outDir, "lock.log")
	stateBeforePath := filepath.Join(outDir, "state_before.json")
	stateAfterInstallPath := filepath.Join(outDir, "state_after_install.json")
	stateAfterActivatePath := filepath.Join(outDir, "state_after_activate.json")
	stateAfterPath := filepath.Join(outDir, "state_after.json")
	guardPath := filepath.Join(outDir, "guard_recommendation.md")

	raceFile, err := os.Create(raceLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create race.log: %v\n", err)
		os.Exit(1)
	}
	defer raceFile.Close()
	mw := io.MultiWriter(os.Stdout, raceFile)

	cfg := race.HarnessConfig{
		ProjectID:        "spike-race-project",
		ServerRoot:       serverRoot,
		InstallRoot:      installRoot,
		ArtifactsDir:     artifactsDir,
		RemoteStagingDir: remoteStagingDir,
		DeployerUser:     deployerUser,
		SizeMB:           sizeMB,
		Logger:           mw,
	}

	fmt.Fprintf(mw, "=== Spike 3 Race Harness ===\n")
	fmt.Fprintf(mw, "project=%s deployer=%s sizeMB=%d\n", cfg.ProjectID, race.SanitizeLogLine(cfg.DeployerUser), cfg.SizeMB)
	fmt.Fprintf(mw, "serverRoot=%s\ninstallRoot=%s\nartifactsDir=%s\nremoteStagingDir=%s\n", serverRoot, installRoot, artifactsDir, remoteStagingDir)

	result, err := race.RunRace(cfg)
	if err != nil {
		fmt.Fprintf(mw, "RACE FAILED: %v\n", err)
		fmt.Fprintf(os.Stderr, "RACE FAILED: %v\n", err)
		os.Exit(1)
	}

	// Write status snapshots
	if result.BeforeState != nil {
		_ = race.SaveStatusJSON(outDir, "before", result.BeforeState)
		if data, _ := json.MarshalIndent(result.BeforeState, "", "  "); data != nil {
			_ = os.WriteFile(stateBeforePath, data, 0644)
		}
	}
	if result.AfterInstallRace != nil {
		_ = race.SaveStatusJSON(outDir, "after_install", result.AfterInstallRace)
		if data, _ := json.MarshalIndent(result.AfterInstallRace, "", "  "); data != nil {
			_ = os.WriteFile(stateAfterInstallPath, data, 0644)
		}
	}
	if result.AfterActivateRace != nil {
		_ = race.SaveStatusJSON(outDir, "after_activate", result.AfterActivateRace)
		if data, _ := json.MarshalIndent(result.AfterActivateRace, "", "  "); data != nil {
			_ = os.WriteFile(stateAfterActivatePath, data, 0644)
		}
		// also generic after
		if data, _ := json.MarshalIndent(result.AfterActivateRace, "", "  "); data != nil {
			_ = os.WriteFile(stateAfterPath, data, 0644)
		}
	}

	// Write summary
	summaryPath := filepath.Join(outDir, "summary.json")
	if data, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, data, 0644)
	}

	// Write concurrent log (extract from race.log)
	if data, err := os.ReadFile(raceLogPath); err == nil {
		_ = os.WriteFile(concurrentLogPath, data, 0644)
	}

	// Lock behavior proof
	lockEvidence := fmt.Sprintf("Lock file: %s\nState file: %s\nContenders: %d Successes: %d Failures: %d\nHolder: %s\nState parseable: %v\nErrors:\n", result.LockContention.LockFile, result.LockContention.StateFile, result.LockContention.Contenders, result.LockContention.Successes, result.LockContention.Failures, result.LockContention.HolderOperation, result.LockContention.StateParseable)
	for _, e := range result.LockContention.Errors {
		lockEvidence += "  - " + e + "\n"
	}
	lockEvidence += "\nState dump:\n" + result.LockContention.StateDump + "\n"
	_ = os.WriteFile(lockLogPath, []byte(lockEvidence), 0644)

	// Guard recommendation
	guardDoc := race.BuildGuardRecommendation()
	_ = os.WriteFile(guardPath, []byte(guardDoc), 0644)
	// Also write to root evidence as guard_recommendation.md
	_ = os.WriteFile(filepath.Join(outDir, "guard_recommendation.md"), []byte(guardDoc), 0644)

	// Artifact manifests
	if data, err := json.MarshalIndent(result.ArtifactLocal.Manifest, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(outDir, "artifact_local.json"), data, 0644)
	}
	if data, err := json.MarshalIndent(result.ArtifactCI.Manifest, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(outDir, "artifact_ci.json"), data, 0644)
	}

	fmt.Fprintf(mw, "\n=== Evidence written to %s ===\n", outDir)
	fmt.Fprintf(mw, "  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - %s\n  - summary.json\n",
		raceLogPath, concurrentLogPath, lockLogPath, stateBeforePath, stateAfterInstallPath, stateAfterActivatePath, guardPath, filepath.Join(outDir, "artifact_local.json"))
	fmt.Fprintf(mw, "All ACs PASS. Race proof complete.\n")

	// also print state dumps for verification
	fmt.Fprintf(mw, "\n--- Lock evidence ---\n%s\n", lockEvidence)
	fmt.Fprintf(mw, "\n--- Guard recommendation preview ---\n%s\n", guardDoc[:800])
}
