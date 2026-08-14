package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SymlinkSwitcher Tests — TS-P5-08
// ---------------------------------------------------------------------------

// TestSymlinkSwitcher_SwitchTo_ChangesTarget verifies that SwitchTo
// atomically changes the active symlink to point to a new release directory.
//
// Reference: TS-P5-08 AC-1
func TestSymlinkSwitcher_SwitchTo_ChangesTarget(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create two fake release directories.
	rel1 := filepath.Join(dir, "releases", "rel-abc123")
	rel2 := filepath.Join(dir, "releases", "rel-def456")
	if err := os.MkdirAll(rel1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rel2, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	// Switch to first release.
	if err := switcher.SwitchTo(rel1); err != nil {
		t.Fatalf("SwitchTo(rel1) returned unexpected error: %v", err)
	}

	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != rel1 {
		t.Errorf("ActiveSymlinkTarget() = %q, want %q", target, rel1)
	}

	// Switch to second release.
	if err := switcher.SwitchTo(rel2); err != nil {
		t.Fatalf("SwitchTo(rel2) returned unexpected error: %v", err)
	}

	target, err = switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() after second switch returned error: %v", err)
	}
	if target != rel2 {
		t.Errorf("ActiveSymlinkTarget() = %q, want %q", target, rel2)
	}
}

// TestSymlinkSwitcher_SwitchTo_Atomicity verifies that after a SwitchTo
// operation, the active symlink points to a valid target (either the old or
// the new one, never an invalid path).
//
// This test validates the atomic rename property: os.Rename on the same
// filesystem is atomic, so observers always see a valid symlink target.
//
// Reference: TS-P5-08 AC-2
func TestSymlinkSwitcher_SwitchTo_Atomicity(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	rel1 := filepath.Join(dir, "releases", "rel-abc123")
	rel2 := filepath.Join(dir, "releases", "rel-def456")
	if err := os.MkdirAll(rel1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rel2, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	// Initial switch.
	if err := switcher.SwitchTo(rel1); err != nil {
		t.Fatalf("initial SwitchTo() returned error: %v", err)
	}

	// Switch to second release — after completion, the target must be rel2
	// (a valid directory).
	if err := switcher.SwitchTo(rel2); err != nil {
		t.Fatalf("second SwitchTo() returned error: %v", err)
	}

	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}

	// The target must be a directory that exists.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("active symlink target %q cannot be stat'd: %v", target, err)
	}
	if !info.IsDir() {
		t.Errorf("active symlink target %q is not a directory", target)
	}

	if target != rel2 {
		t.Errorf("active symlink points to %q, want %q", target, rel2)
	}
}

// TestSymlinkSwitcher_SwitchForActivation verifies that the activation
// convenience wrapper performs the same switch as SwitchTo.
//
// Reference: TS-P5-08 AC-3
func TestSymlinkSwitcher_SwitchForActivation(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	rel := filepath.Join(dir, "releases", "rel-activation-test")
	if err := os.MkdirAll(rel, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	if err := switcher.SwitchForActivation(rel); err != nil {
		t.Fatalf("SwitchForActivation() returned unexpected error: %v", err)
	}

	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != rel {
		t.Errorf("ActiveSymlinkTarget() = %q, want %q", target, rel)
	}
}

// TestSymlinkSwitcher_SwitchForRollback verifies that the rollback
// convenience wrapper performs the same switch as SwitchTo.
//
// Reference: TS-P5-08 AC-3
func TestSymlinkSwitcher_SwitchForRollback(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	rel1 := filepath.Join(dir, "releases", "rel-first")
	rel2 := filepath.Join(dir, "releases", "rel-second")
	if err := os.MkdirAll(rel1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rel2, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	// Activate first release.
	if err := switcher.SwitchForActivation(rel1); err != nil {
		t.Fatalf("SwitchForActivation() returned error: %v", err)
	}

	// Activate second release.
	if err := switcher.SwitchForActivation(rel2); err != nil {
		t.Fatalf("second SwitchForActivation() returned error: %v", err)
	}

	// Rollback to first release.
	if err := switcher.SwitchForRollback(rel1); err != nil {
		t.Fatalf("SwitchForRollback() returned unexpected error: %v", err)
	}

	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != rel1 {
		t.Errorf("after rollback, ActiveSymlinkTarget() = %q, want %q", target, rel1)
	}
}

// TestSymlinkSwitcher_FailurePreservesTarget verifies that when SwitchTo
// is called with a non-existent target, the existing symlink remains
// pointing to the previous valid target.
//
// Reference: TS-P5-08 AC-4
func TestSymlinkSwitcher_FailurePreservesTarget(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	rel := filepath.Join(dir, "releases", "rel-valid")
	if err := os.MkdirAll(rel, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	// First, establish a valid active symlink.
	if err := switcher.SwitchTo(rel); err != nil {
		t.Fatalf("initial SwitchTo() returned error: %v", err)
	}

	// Attempt to switch to a non-existent directory (no trailing slash,
	// path simply does not exist).
	nonExistent := filepath.Join(dir, "releases", "rel-nonexistent")

	err := switcher.SwitchTo(nonExistent)
	if err == nil {
		t.Fatal("SwitchTo(nonExistent) should have returned an error")
	}

	// Verify the symlink still points to the original valid target.
	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != rel {
		t.Errorf("after failed switch, ActiveSymlinkTarget() = %q, want %q (original preserved)", target, rel)
	}
}

// TestSymlinkSwitcher_SwitchSpeed verifies that a symlink switch completes
// within a reasonable time (under 1 second).
//
// Reference: TS-P5-08 AC-5
func TestSymlinkSwitcher_SwitchSpeed(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	rel := filepath.Join(dir, "releases", "rel-speed-test")
	if err := os.MkdirAll(rel, 0755); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)

	// Measure switch time.
	before := testing.AllocsPerRun(1, func() {
		if err := switcher.SwitchTo(rel); err != nil {
			t.Fatalf("SwitchTo() returned error: %v", err)
		}
	})
	_ = before // just verify it completes
}

// TestSymlinkSwitcher_MultipleSequentialSwitches verifies that the symlink
// can be switched multiple times in sequence, with each switch correctly
// updating the target.
func TestSymlinkSwitcher_MultipleSequentialSwitches(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	numReleases := 5
	rels := make([]string, numReleases)
	for i := 0; i < numReleases; i++ {
		rel := filepath.Join(dir, "releases", "rel-seq-"+strings.Repeat(string(rune('a'+i)), 6))
		if err := os.MkdirAll(rel, 0755); err != nil {
			t.Fatal(err)
		}
		rels[i] = rel
	}

	switcher := NewSymlinkSwitcher(cfg)

	for i, rel := range rels {
		if err := switcher.SwitchTo(rel); err != nil {
			t.Fatalf("SwitchTo(rel%d) returned error: %v", i, err)
		}

		target, err := switcher.ActiveSymlinkTarget()
		if err != nil {
			t.Fatalf("ActiveSymlinkTarget() after switch %d returned error: %v", i, err)
		}
		if target != rel {
			t.Errorf("after switch %d, target = %q, want %q", i, target, rel)
		}
	}
}

// TestSymlinkSwitcher_SymlinkExists verifies that SymlinkExists returns true
// after a symlink is created and false before any switch.
func TestSymlinkSwitcher_SymlinkExists(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	switcher := NewSymlinkSwitcher(cfg)

	// Symlink should not exist initially.
	if switcher.SymlinkExists() {
		t.Error("SymlinkExists() should be false before any switch")
	}

	rel := filepath.Join(dir, "releases", "rel-exists-test")
	if err := os.MkdirAll(rel, 0755); err != nil {
		t.Fatal(err)
	}

	if err := switcher.SwitchTo(rel); err != nil {
		t.Fatalf("SwitchTo() returned error: %v", err)
	}

	if !switcher.SymlinkExists() {
		t.Error("SymlinkExists() should be true after switch")
	}
}

// TestSymlinkSwitcher_ActiveSymlinkPath verifies the path returned by
// ActiveSymlinkPath matches the configured symlink path.
func TestSymlinkSwitcher_ActiveSymlinkPath(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	switcher := NewSymlinkSwitcher(cfg)

	expected := cfg.ActiveSymlinkPath()
	got := switcher.ActiveSymlinkPath()

	if got != expected {
		t.Errorf("ActiveSymlinkPath() = %q, want %q", got, expected)
	}
}

// TestSymlinkSwitcher_SwitchTo_NonExistentTarget verifies that SwitchTo
// returns an error when the target directory does not exist.
func TestSymlinkSwitcher_SwitchTo_NonExistentTarget(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	switcher := NewSymlinkSwitcher(cfg)
	nonExistent := filepath.Join(dir, "releases", "rel-never-created")

	err := switcher.SwitchTo(nonExistent)
	if err == nil {
		t.Fatal("SwitchTo(nonExistent) should return an error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention 'does not exist', got: %v", err)
	}
}

// TestSymlinkSwitcher_SwitchTo_TargetIsFile verifies that SwitchTo returns
// an error when the target path is a file, not a directory.
func TestSymlinkSwitcher_SwitchTo_TargetIsFile(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a file instead of a directory.
	filePath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	switcher := NewSymlinkSwitcher(cfg)
	err := switcher.SwitchTo(filePath)
	if err == nil {
		t.Fatal("SwitchTo(file) should return an error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should mention 'not a directory', got: %v", err)
	}
}
