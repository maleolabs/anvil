package spkinstaller

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/spikes/installer-tooling/builders"
)

func TestSanitizeInstallerName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"MyApp", "MyApp"},
		{"my-app_2", "my-app_2"},
		{"  Hello World  ", "Hello World"},
		{"Bad!@#$Name", "BadName"},
		{"", "anvil"},
		{"   ", "anvil"},
		{"!@#", "anvil"},
	}
	for _, c := range cases {
		if got := builders.SanitizeInstallerName(c.in); got != c.want {
			t.Errorf("SanitizeInstallerName(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestRenderedFilename_ContainsSanitized(t *testing.T) {
	name := builders.SanitizeInstallerName("My Cool App")
	for _, b := range builders.All() {
		fn := builders.RenderedFilename(name, b.Extension())
		if !strings.Contains(fn, "My-Cool-App") && !strings.Contains(strings.ToLower(fn), "my-cool-app") {
			t.Errorf("%s filename %q missing sanitized name", b.ID(), fn)
		}
		if !strings.HasSuffix(fn, b.Extension()) {
			t.Errorf("%s filename %q missing ext %q", b.ID(), fn, b.Extension())
		}
	}
}

func TestIconVerification_WindowsRequiresICO(t *testing.T) {
	dir := t.TempDir()
	m := builders.CreateIconFixtures(dir)
	if ok, _ := builders.VerifyIcon(m["ico"], "windows"); !ok {
		t.Error("windows .ico should verify")
	}
	if ok, _ := builders.VerifyIcon(m["png"], "windows"); ok {
		t.Error("windows .png should NOT verify (expects .ico)")
	}
	if ok, _ := builders.VerifyIcon(m["png"], "linux"); !ok {
		t.Error("linux .png should verify")
	}
	if ok, _ := builders.VerifyIcon(m["ico"], "linux"); ok {
		t.Error("linux .ico should NOT verify (expects .png)")
	}
	if ok, _ := builders.VerifyIcon("", "windows"); ok {
		t.Error("empty icon should fail")
	}
}

func TestAllBuilders_BuildSucceeds(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	evDir := filepath.Join(t.TempDir(), "evidence")
	for _, b := range builders.All() {
		workDir := filepath.Join(t.TempDir(), "work-"+b.ID())
		iconPath := ""
		if b.OS() == "windows" {
			iconPath = builders.CreateIconFixtures(t.TempDir())["ico"]
		} else {
			iconPath = builders.CreateIconFixtures(t.TempDir())["png"]
		}
		res, err := b.Build(builders.BuildConfig{
			InstallerName: "TestApp",
			IconPath:      iconPath,
			PayloadSizeMB: 1,
			OutputDir:     outDir,
			WorkDir:       workDir,
		})
		if err != nil {
			t.Fatalf("%s Build error: %v", b.ID(), err)
		}
		if !res.Success {
			t.Fatalf("%s not success: %s", b.ID(), res.Error)
		}
		if !res.IconVerified {
			t.Errorf("%s icon not verified: %s", b.ID(), res.IconDetail)
		}
		if _, err := os.Stat(res.OutputPath); err != nil {
			t.Errorf("%s output missing: %v", b.ID(), err)
		}
		if res.SizeBytes <= res.PayloadBytes {
			t.Errorf("%s size %d <= payload %d", b.ID(), res.SizeBytes, res.PayloadBytes)
		}
		if res.OverheadBytes != b.OverheadBytes() {
			t.Errorf("%s overhead %d want %d", b.ID(), res.OverheadBytes, b.OverheadBytes())
		}
		if !strings.Contains(strings.ToLower(res.NameRendered), strings.ToLower("TestApp")) {
			t.Errorf("%s NameRendered %q missing TestApp", b.ID(), res.NameRendered)
		}
	}
	_ = evDir
}

func TestBuildOverheadOrdering(t *testing.T) {
	// Verify expected overhead ranking: makeself < deb < inno < nsis < wix < appimage (with synthetic values)
	// Note: deb 0.8MB < inno 1.2MB < nsis 1.6MB ; wix 4.2MB < appimage 12MB
	expectedOrder := []string{"makeself", "deb", "inno", "nsis", "wix", "appimage"}
	overheads := make(map[string]int64)
	for _, b := range builders.All() {
		overheads[b.ID()] = b.OverheadBytes()
	}
	// check thatmakeself smallest and appimage largest
	if overheads["makeself"] >= overheads["appimage"] {
		t.Errorf("makeself %d should be < appimage %d", overheads["makeself"], overheads["appimage"])
	}
	// check ordering is sorted as expected
	prev := int64(-1)
	for _, id := range expectedOrder {
		cur := overheads[id]
		if cur <= prev {
			t.Errorf("overhead order broken at %s: %d <= prev %d", id, cur, prev)
		}
		prev = cur
	}
}

func TestBuildDurationOrdering(t *testing.T) {
	// fastest -> slowest: makeself < deb < nsis < inno < appimage < wix
	expectedOrder := []string{"makeself", "deb", "nsis", "inno", "appimage", "wix"}
	durs := make(map[string]int64)
	for _, b := range builders.All() {
		durs[b.ID()] = b.SyntheticBuildDuration(50).Milliseconds()
	}
	prev := int64(-1)
	for _, id := range expectedOrder {
		cur := durs[id]
		if cur <= prev {
			t.Errorf("duration order broken at %s: %d <= prev %d", id, cur, prev)
		}
		prev = cur
	}
}

func TestHarness_FullRun(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	evDir := filepath.Join(t.TempDir(), "evidence")
	var log bytes.Buffer
	res, err := RunHarness(HarnessConfig{
		InstallerName: "HarnessApp",
		PayloadSizeMB: 1,
		OutputDir:     outDir,
		EvidenceDir:   evDir,
		RepoRoot:      ".",
		Logger:        &log,
	})
	if err != nil {
		t.Fatalf("RunHarness: %v", err)
	}
	if len(res.Results) != 6 {
		t.Fatalf("results len %d want 6", len(res.Results))
	}
	for _, r := range res.Results {
		if !r.Success {
			t.Errorf("%s not success", r.Tool)
		}
	}
	// evidence files exist
	for _, name := range []string{"matrix.csv", "matrix.md", "size-measurements.csv", "icon-tests.log", "signing-feasibility.md", "ux-eval.md", "recommendation.md"} {
		if _, err := os.Stat(filepath.Join(evDir, name)); err != nil {
			t.Errorf("evidence %s missing: %v", name, err)
		}
	}
	// per-builder logs
	for _, b := range builders.All() {
		if _, err := os.Stat(filepath.Join(evDir, "build-"+b.ID()+".log")); err != nil {
			t.Errorf("build log %s missing: %v", b.ID(), err)
		}
	}
	if log.Len() == 0 {
		t.Error("expected harness log output")
	}
}

func TestLoadInstallerName_Fallback(t *testing.T) {
	// from repo root should load "anvil" (project.name) when installer.name absent
	got := builders.LoadInstallerName("/tmp/nonexistent-xyz")
	if got != "anvil" {
		t.Errorf("LoadInstallerName fallback = %q want anvil", got)
	}
	// missing dir + anvil.yaml at root fallback to anvil
	got2 := builders.LoadInstallerName(".")
	if got2 == "" {
		t.Error("LoadInstallerName returned empty")
	}
}

func TestUXAndSigning_NotEmpty(t *testing.T) {
	for _, b := range builders.All() {
		ux := b.UX()
		if ux.SilentFlag == "" {
			t.Errorf("%s UX SilentFlag empty", b.ID())
		}
		if ux.DefaultLocation == "" {
			t.Errorf("%s UX DefaultLocation empty", b.ID())
		}
		sig := b.Signing()
		if sig.Method == "" || sig.VerifyCommand == "" {
			t.Errorf("%s Signing incomplete", b.ID())
		}
	}
}
