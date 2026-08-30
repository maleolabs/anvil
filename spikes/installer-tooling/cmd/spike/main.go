package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	spike "maleolabs.com/anvil/spikes/installer-tooling"
	"maleolabs.com/anvil/spikes/installer-tooling/builders"
)

func main() {
	sizeMB := flag.Int("size-mb", 5, "payload size per installer in MB (5 fast, 50 lab)")
	name := flag.String("name", "", "installer.name override (default from anvil.yaml)")
	iconICO := flag.String("icon-ico", "", "Windows .ico path (default fixtures)")
	iconPNG := flag.String("icon-png", "", "Linux .png path (default fixtures)")
	outDir := flag.String("out", "", "output dir for installers (default temp + evidence)")
	evidenceDir := flag.String("evidence", "", "evidence dir (default spikes/installer-tooling/evidence)")
	repoRoot := flag.String("repo", ".", "repo root for anvil.yaml lookup")
	flag.Parse()

	if *evidenceDir == "" {
		*evidenceDir = "spikes/installer-tooling/evidence"
		if _, err := os.Stat(*evidenceDir); os.IsNotExist(err) {
			*evidenceDir = filepath.Join(os.TempDir(), "spike-installer-evidence")
		}
	}
	if err := os.MkdirAll(*evidenceDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir evidence: %v\n", err)
		os.Exit(1)
	}

	outputDir := *outDir
	if outputDir == "" {
		outputDir = filepath.Join(*evidenceDir, "artifacts")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir output: %v\n", err)
		os.Exit(1)
	}

	installerName := *name
	if installerName == "" {
		installerName = builders.LoadInstallerName(*repoRoot)
	}

	fmt.Printf("=== Spike 2: Installer Tooling Evaluation ===\n")
	fmt.Printf("installer.name=%q payload=%dMB output=%s evidence=%s\n", installerName, *sizeMB, outputDir, *evidenceDir)
	if *iconICO != "" {
		fmt.Printf("icon-ico=%s\n", *iconICO)
	}
	if *iconPNG != "" {
		fmt.Printf("icon-png=%s\n", *iconPNG)
	}

	cfg := spike.HarnessConfig{
		InstallerName: installerName,
		IconPathICO:   *iconICO,
		IconPathPNG:   *iconPNG,
		PayloadSizeMB: *sizeMB,
		OutputDir:     outputDir,
		EvidenceDir:   *evidenceDir,
		RepoRoot:      *repoRoot,
		Logger:        os.Stdout,
	}

	result, err := spike.RunHarness(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== AC1 Build Results ===\n")
	for _, r := range result.Results {
		fmt.Printf("  %-10s %-8s %-30s size=%.2fMB overhead=%.2fMB build=%dms sim=%t icon=%t\n",
			r.Tool, r.OS, r.OutputFileName,
			float64(r.SizeBytes)/1024/1024, float64(r.OverheadBytes)/1024/1024,
			r.BuildDuration.Milliseconds(), r.Simulated, r.IconVerified)
	}
	fmt.Printf("\n=== AC2 Icon & Name Rendering ===\n")
	for _, it := range result.IconTests {
		mark := "✓"
		if !it.Verified {
			mark = "✗"
		}
		fmt.Printf("  %s %-10s icon=%s name=%s\n", mark, it.Tool, filepath.Base(it.IconPath), it.NameRendered)
	}
	fmt.Printf("\n=== AC3 Signing Feasibility ===\n")
	fmt.Printf("  See %s/signing-feasibility.md (Authenticode self-signed + GPG/deb)\n", *evidenceDir)
	fmt.Printf("\n=== AC4 UX Eval ===\n")
	fmt.Printf("  See %s/ux-eval.md (silent/GUI/location/shortcut/uninstall/privilege)\n", *evidenceDir)
	fmt.Printf("\n=== Recommendation ===\n")
	fmt.Printf("  See %s/recommendation.md — Winner: Windows NSIS, Linux Makeself+deb\n", *evidenceDir)
	fmt.Printf("\n=== Evidence Gate ===\n")
	for _, f := range []string{"matrix.csv", "matrix.md", "size-measurements.csv", "icon-tests.log", "signing-feasibility.md", "ux-eval.md", "recommendation.md"} {
		fmt.Printf("  - %s/%s\n", *evidenceDir, f)
	}
	for _, b := range builders.All() {
		fmt.Printf("  - %s/build-%s.log\n", *evidenceDir, b.ID())
	}
	fmt.Printf("\nHarness completed. Run `go test ./spikes/installer-tooling/... -v` for unit gates.\n")
}
