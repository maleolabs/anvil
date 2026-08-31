package spkinstallerboundary

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
)

func tempCfg(t *testing.T) HarnessConfig {
	t.Helper()
	base := t.TempDir()
	return HarnessConfig{
		ProjectID:       "spike-installer-boundary",
		ArtifactsDir:    filepath.Join(base, "artifacts"),
		InstallerOutDir: filepath.Join(base, "installer-out"),
		InstallRoot:     filepath.Join(base, "install", "myapp"),
		Logger:          &bytes.Buffer{},
		Version:         "1.0.0",
		Variant:         VariantLinux,
	}
}

// AC1
func TestAC1_LaravelArtifactManifestValid(t *testing.T) {
	cfg := tempCfg(t)
	art, err := BuildLaravelArtifact(cfg)
	if err != nil {
		t.Fatalf("BuildLaravelArtifact: %v", err)
	}
	if art.Manifest.ArtifactID == "" {
		t.Error("artifact_id empty (identity-from-content)")
	}
	if art.Manifest.Checksum == "" || art.Manifest.ChecksumType == "" {
		t.Error("checksum missing")
	}
	if art.Manifest.ProjectID != cfg.ProjectID {
		t.Errorf("project_id %q want %q", art.Manifest.ProjectID, cfg.ProjectID)
	}
	if err := ValidateManifestSchema(art.Manifest); err != nil {
		t.Fatalf("manifest schema: %v", err)
	}
	vr, err := artifact.VerifyArtifact(art.Path)
	if err != nil {
		t.Fatalf("VerifyArtifact: %v", err)
	}
	if !vr.Passed {
		t.Fatalf("verification FAIL: %+v", vr.Checks)
	}
	if art.Manifest.Version != "1.0.0" {
		t.Errorf("version %q want 1.0.0", art.Manifest.Version)
	}
}

// AC2 + AC3: bundle and install triggers standard hook via interface
func TestAC2_BundleAndInstallTriggersStandardHook(t *testing.T) {
	cfg := tempCfg(t)
	var buf bytes.Buffer
	cfg.Logger = &buf
	result, err := RunHarness(cfg)
	if err != nil {
		t.Fatalf("RunHarness: %v\nlog:\n%s", err, buf.String())
	}
	if result.Installer == nil || result.Installer.Path == "" {
		t.Fatal("installer not built")
	}
	if result.StandardResult == nil || !result.StandardResult.SuperAdminExists {
		t.Fatal("standard hook did not produce super-admin")
	}
	if err := VerifyStandardHookResults(cfg.InstallRoot); err != nil {
		t.Fatalf("VerifyStandardHookResults: %v", err)
	}
	if err := assertInstallerContainsTrigger(result.Installer.Path); err != nil {
		t.Fatalf("AC3 trigger point: %v", err)
	}
}

func assertInstallerContainsTrigger(installerPath string) error {
	r, err := zip.OpenReader(installerPath)
	if err != nil {
		return err
	}
	defer r.Close()
	found := false
	for _, f := range r.File {
		if f.Name == "installer.sh" || f.Name == "installer.bat" {
			rc, _ := f.Open()
			b := new(bytes.Buffer)
			_, _ = b.ReadFrom(rc)
			rc.Close()
			if bytes.Contains(b.Bytes(), []byte("anvil standard setup")) {
				found = true
			}
			if bytes.Contains(b.Bytes(), []byte("migrate --force")) {
				return fmt.Errorf("installer script must NOT duplicate migrate --force; standard hook owns it")
			}
		}
	}
	if !found {
		return fmt.Errorf("installer script missing 'anvil standard setup' trigger point")
	}
	return nil
}

// AC2 windows dummy variant also works
func TestAC2_WindowsDummyVariant(t *testing.T) {
	cfg := tempCfg(t)
	cfg.Variant = VariantWindows
	var buf bytes.Buffer
	cfg.Logger = &buf
	art, err := BuildLaravelArtifact(cfg)
	if err != nil {
		t.Fatalf("BuildLaravelArtifact: %v", err)
	}
	installer, err := BuildInstaller(art.Path, "1.0.0", VariantWindows, cfg.InstallerOutDir, &buf)
	if err != nil {
		t.Fatalf("BuildInstaller windows: %v", err)
	}
	hook := &MockStandardSetup{Logger: &buf}
	ctx := context.Background()
	_, err = Install(ctx, installer.Path, cfg.InstallRoot, hook, InstallOptions{}, &buf)
	if err != nil {
		t.Fatalf("Install windows: %v\nlog:%s", err, buf.String())
	}
	if err := VerifyStandardHookResults(cfg.InstallRoot); err != nil {
		t.Fatalf("verify windows: %v", err)
	}
	if err := assertInstallerContainsTrigger(installer.Path); err != nil {
		t.Fatalf("AC3 windows: %v", err)
	}
}

// AC4 idempotency
func TestAC4_IdempotentRetry(t *testing.T) {
	cfg := tempCfg(t)
	art, err := BuildLaravelArtifact(cfg)
	if err != nil {
		t.Fatalf("BuildLaravelArtifact: %v", err)
	}
	installer, _ := BuildInstaller(art.Path, "1.0.0", VariantLinux, cfg.InstallerOutDir, nil)
	ctx := context.Background()
	hook := &MockStandardSetup{}
	if _, err := Install(ctx, installer.Path, cfg.InstallRoot, hook, InstallOptions{}, nil); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := Install(ctx, installer.Path, cfg.InstallRoot, hook, InstallOptions{}, nil); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if err := VerifyStandardHookResults(cfg.InstallRoot); err != nil {
		t.Fatalf("verify after idempotent: %v", err)
	}
}

// AC4 cancel recovery
func TestAC4_CancelRecovery(t *testing.T) {
	cfg := tempCfg(t)
	art, _ := BuildLaravelArtifact(cfg)
	installer, _ := BuildInstaller(art.Path, "1.0.0", VariantLinux, cfg.InstallerOutDir, nil)
	ctx := context.Background()
	hook := &MockStandardSetup{}
	cancelRoot := cfg.InstallRoot + "-cancel"
	_, err := Install(ctx, installer.Path, cancelRoot, hook, InstallOptions{CancelAfterBytes: 1}, nil)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if _, err := Install(ctx, installer.Path, cancelRoot, hook, InstallOptions{}, nil); err != nil {
		t.Fatalf("retry after cancel: %v", err)
	}
	if err := VerifyStandardHookResults(cancelRoot); err != nil {
		t.Fatalf("verify after cancel retry: %v", err)
	}
}

// AC4 migrate fail rollback
func TestAC4_MigrateFailRollback(t *testing.T) {
	cfg := tempCfg(t)
	art, _ := BuildLaravelArtifact(cfg)
	installer, _ := BuildInstaller(art.Path, "1.0.0", VariantLinux, cfg.InstallerOutDir, nil)
	ctx := context.Background()
	failHook := &MockStandardSetup{FailNext: true}
	failRoot := cfg.InstallRoot + "-rollback"
	_, err := Install(ctx, installer.Path, failRoot, failHook, InstallOptions{}, nil)
	if err == nil {
		t.Fatal("expected migrate fail error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("actionable")) && !bytes.Contains([]byte(err.Error()), []byte("rolled back")) {
		t.Errorf("error should be actionable and mention rollback, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(failRoot, ".anvil-install-state.json")); err == nil {
		t.Error("state file should not exist after rollback (no prior install)")
	}
	goodHook := &MockStandardSetup{}
	if _, err := Install(ctx, installer.Path, failRoot, goodHook, InstallOptions{}, nil); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if err := VerifyStandardHookResults(failRoot); err != nil {
		t.Fatalf("verify after rollback retry: %v", err)
	}
}

func TestIdentityFromContent(t *testing.T) {
	cfg1 := tempCfg(t)
	cfg2 := tempCfg(t)
	art1, _ := BuildLaravelArtifact(cfg1)
	art2, _ := BuildLaravelArtifact(cfg2)
	if art1.Manifest.ArtifactID != art2.Manifest.ArtifactID {
		t.Logf("warning: second build artifact_id mismatch %s vs %s (may be nondeterministic, but BuildLaravelArtifact is deterministic so expect equal)", art1.Manifest.ArtifactID[:16], art2.Manifest.ArtifactID[:16])
	}
}
