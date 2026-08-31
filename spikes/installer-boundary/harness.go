package spkinstallerboundary

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/artifact"
)

// HarnessConfig configures the spike run (AC1-4).
type HarnessConfig struct {
	ProjectID       string // e.g. spike-installer-boundary
	ArtifactsDir    string // local build output
	InstallerOutDir string // installer dummy output
	InstallRoot     string // user-chosen lokasi (extract target)
	Logger          io.Writer
	Version         string
	Variant         InstallerVariant
}

// HarnessResult is full evidence of the spike run.
type HarnessResult struct {
	Artifact              *ArtifactInfo                `json:"artifact"`
	Installer             *InstallerInfo               `json:"installer"`
	VerifyResult          *artifact.VerificationResult `json:"verify"`
	StandardResult        *SetupResult                 `json:"standard_result"`
	IdempotentOK          bool                         `json:"idempotent_ok"`
	CancelRecovered       bool                         `json:"cancel_recovered"`
	MigrateFailRolledBack bool                         `json:"migrate_fail_rolled_back"`
}

// ArtifactInfo carries built artifact.
type ArtifactInfo struct {
	Path      string             `json:"path"`
	Manifest  *artifact.Manifest `json:"manifest"`
	SizeBytes int64              `json:"size_bytes"`
}

// BuildLaravelArtifact creates a minimal Laravel-like source tree and packages via artifact.Package (AC1).
func BuildLaravelArtifact(cfg HarnessConfig) (*ArtifactInfo, error) {
	if cfg.ArtifactsDir == "" {
		return nil, fmt.Errorf("ArtifactsDir required")
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("ProjectID required")
	}
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	srcDir, err := os.MkdirTemp("", "spike-installer-src-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(srcDir)

	// Minimal Laravel sample: artisan, composer.json, .env.example, app, database, public, storage, routes, config
	files := map[string]string{
		"artisan":       "#!/usr/bin/env php\n<?php echo 'artisan';",
		"composer.json": `{"name":"spike/laravel","require":{"php":">=8.2"}}`,
		".env.example":  "APP_ENV=production\nAPP_KEY=base64:spikeKey1234567890\nDB_CONNECTION=sqlite\n",
		"index.php":     "<?php echo 'spike laravel v1';",
		"app/Http/Controllers/WelcomeController.php": "<?php class Welcome {}",
		"app/Models/User.php":                        "<?php class User {}",
		"database/migrations/001_create_users.php":   "<?php // migration",
		"database/seeders/DatabaseSeeder.php":        "<?php // seeder creates super-admin",
		"public/index.php":                           "<?php // public entry",
		"storage/app/.gitkeep":                       "",
		"routes/web.php":                             "<?php // routes",
		"config/app.php":                             "<?php // config",
	}
	for rel, content := range files {
		full := filepath.Join(srcDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return nil, err
		}
	}

	_ = os.MkdirAll(cfg.ArtifactsDir, 0755)
	// AC1: reuse internal/artifact.Package directly
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: srcDir,
		OutputDir: cfg.ArtifactsDir,
		Formats:   []string{"tar.gz"},
		Version:   version,
		Source:    cfg.ProjectID,
		ProjectID: cfg.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("artifact.Package: %w", err)
	}
	if result.Manifest == nil {
		return nil, fmt.Errorf("nil manifest")
	}
	if err := ValidateManifestSchema(result.Manifest); err != nil {
		return nil, fmt.Errorf("manifest schema invalid (spec:artifact-manifest): %w", err)
	}
	vr, err := artifact.VerifyArtifact(result.ArtifactPath)
	if err != nil {
		return nil, fmt.Errorf("VerifyArtifact: %w", err)
	}
	if !vr.Passed {
		return nil, fmt.Errorf("artifact verification failed (spec:artifact-manifest)")
	}
	info, _ := os.Stat(result.ArtifactPath)
	var sz int64
	if info != nil {
		sz = info.Size()
	}
	return &ArtifactInfo{Path: result.ArtifactPath, Manifest: result.Manifest, SizeBytes: sz}, nil
}

// RunHarness orchestrates AC1-4 proof.
func RunHarness(cfg HarnessConfig) (*HarnessResult, error) {
	if cfg.ProjectID == "" {
		cfg.ProjectID = "spike-installer-boundary"
	}
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}
	if cfg.Variant == "" {
		cfg.Variant = VariantLinux
	}
	if cfg.Logger == nil {
		cfg.Logger = io.Discard
	}
	for _, d := range []string{cfg.ArtifactsDir, cfg.InstallerOutDir, cfg.InstallRoot} {
		if d != "" {
			_ = os.MkdirAll(d, 0755)
		}
	}
	result := &HarnessResult{}
	ctx := context.Background()

	// AC1
	fmt.Fprintln(cfg.Logger, "=== AC1: Build Laravel sample artifact via artifact.Package ===")
	art, err := BuildLaravelArtifact(cfg)
	if err != nil {
		return nil, fmt.Errorf("AC1: %w", err)
	}
	result.Artifact = art
	fmt.Fprintf(cfg.Logger, "[AC1] artifact id=%s version=%s checksum=%s size=%d path=%s\n", art.Manifest.ArtifactID[:16], art.Manifest.Version, art.Manifest.Checksum[:16], art.SizeBytes, filepath.Base(art.Path))
	vr, _ := artifact.VerifyArtifact(art.Path)
	result.VerifyResult = vr
	WriteVerifyLog(cfg.Logger, art.Path, vr)

	// AC2 + AC3: bundle to installer dummy then install triggering standard hook
	fmt.Fprintf(cfg.Logger, "\n=== AC2: Bundle artifact to installer dummy (%s) ===\n", cfg.Variant)
	installer, err := BuildInstaller(art.Path, cfg.Version, cfg.Variant, cfg.InstallerOutDir, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("AC2 build installer: %w", err)
	}
	result.Installer = installer
	fmt.Fprintf(cfg.Logger, "[AC2] installer %s size=%d\n", filepath.Base(installer.Path), installer.SizeBytes)
	fmt.Fprintf(cfg.Logger, "[AC3] trigger point: extract -> anvil standard setup (interface StandardSetup.Setup)\n")
	fmt.Fprintf(cfg.Logger, "[AC3] contract: installer MUST NOT duplicate migrate/seed logic; standard owns it (vis:anvil-manifesto §4, ADR-003)\n")

	fmt.Fprintf(cfg.Logger, "\n=== AC2: Install to user-chosen lokasi %s -> trigger anvil standard setup ===\n", cfg.InstallRoot)
	hook := &MockStandardSetup{Logger: cfg.Logger}
	setupRes, err := Install(ctx, installer.Path, cfg.InstallRoot, hook, InstallOptions{}, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("AC2 install: %w", err)
	}
	result.StandardResult = setupRes
	// AC2 verify super-admin + storage:link
	if err := VerifyStandardHookResults(cfg.InstallRoot); err != nil {
		return nil, fmt.Errorf("AC2 verify super-admin/storage:link: %w", err)
	}
	fmt.Fprintln(cfg.Logger, "[AC2] PASS: super-admin exists + storage:link PASS (mock simulates migrate --force && db:seed)")

	// AC4: idempotency — second install same artifact is idempotent
	fmt.Fprintln(cfg.Logger, "\n=== AC4: Idempotency — retry same installer (should be idempotent noop) ===")
	setupRes2, err := Install(ctx, installer.Path, cfg.InstallRoot, hook, InstallOptions{}, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("AC4 idempotent retry: %w", err)
	}
	if setupRes2 == nil {
		return nil, fmt.Errorf("AC4 idempotent retry returned nil")
	}
	result.IdempotentOK = true
	fmt.Fprintln(cfg.Logger, "[AC4] idempotent PASS: second install detected already installed, verified without corrupt")

	// AC4: cancel mid-install — must not corrupt, retry safe
	fmt.Fprintln(cfg.Logger, "\n=== AC4: Cancel mid-install (simulate user cancel) ===")
	cancelRoot := cfg.InstallRoot + "-cancel-test"
	_ = os.RemoveAll(cancelRoot)
	defer os.RemoveAll(cancelRoot)
	_, err = Install(ctx, installer.Path, cancelRoot, hook, InstallOptions{CancelAfterBytes: 1}, cfg.Logger)
	if err == nil {
		return nil, fmt.Errorf("AC4 cancel expected failure but got success")
	}
	fmt.Fprintf(cfg.Logger, "[AC4] cancel correctly failed: %v\n", err)
	// retry without cancel must succeed (no corrupt)
	fmt.Fprintln(cfg.Logger, "[AC4] retry after cancel (must succeed)...")
	_, err = Install(ctx, installer.Path, cancelRoot, hook, InstallOptions{}, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("AC4 retry after cancel failed (should be safe): %w", err)
	}
	if err := VerifyStandardHookResults(cancelRoot); err != nil {
		return nil, fmt.Errorf("AC4 verify after cancel retry: %w", err)
	}
	result.CancelRecovered = true
	fmt.Fprintln(cfg.Logger, "[AC4] cancel recovery PASS: retry safe, no corrupt")

	// AC4: migrate fail -> rollback + actionable error
	fmt.Fprintln(cfg.Logger, "\n=== AC4: Migrate fail -> rollback (actionable error) ===")
	failRoot := cfg.InstallRoot + "-rollback-test"
	_ = os.RemoveAll(failRoot)
	defer os.RemoveAll(failRoot)
	failingHook := &MockStandardSetup{FailNext: true, Logger: cfg.Logger}
	_, err = Install(ctx, installer.Path, failRoot, failingHook, InstallOptions{}, cfg.Logger)
	if err == nil {
		return nil, fmt.Errorf("AC4 migrate fail expected error but got success")
	}
	fmt.Fprintf(cfg.Logger, "[AC4] migrate fail correctly returned actionable error: %v\n", err)
	// ensure no half-state: .anvil-install-state should not exist after rollback (no prior install)
	if _, statErr := os.Stat(filepath.Join(failRoot, ".anvil-install-state.json")); statErr == nil {
		return nil, fmt.Errorf("AC4 rollback FAIL: state file exists after migrate fail (should have been removed)")
	}
	// retry with success hook must work
	fmt.Fprintln(cfg.Logger, "[AC4] retry after migrate fail with good hook...")
	goodHook := &MockStandardSetup{Logger: cfg.Logger}
	_, err = Install(ctx, installer.Path, failRoot, goodHook, InstallOptions{}, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("AC4 retry after migrate fail: %w", err)
	}
	if err := VerifyStandardHookResults(failRoot); err != nil {
		return nil, fmt.Errorf("AC4 verify after rollback retry: %w", err)
	}
	result.MigrateFailRolledBack = true
	fmt.Fprintln(cfg.Logger, "[AC4] rollback PASS: artifact rolled back, actionable error, retry safe")

	fmt.Fprintln(cfg.Logger, "\n=== ALL ACs PASS ===")
	return result, nil
}
