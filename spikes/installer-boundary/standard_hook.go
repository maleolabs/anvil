package spkinstallerboundary

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// StandardSetup is the trigger-point contract (AC3):
// After dumb-wrapper extraction, the installer invokes the embedded anvil runtime
// via `anvil standard setup` — represented here as this Go interface, not shell duplication.
//
// Contract: the installer MUST delegate Laravel-owned setup to this hook.
// The hook owns: php artisan migrate --force, db:seed (super-admin), storage:link
// The installer owns: bundling (.tar.gz + manifest), extract to user-chosen location, invoke hook.
//
// In production this is implemented by the Laravel standard adapter (anvil-standard-laravel)
// exposing ActivationCommands in manifest + `anvil standard setup` execution.
// In spike we simulate via MockStandardSetup (documented below).
type StandardSetup interface {
	Setup(ctx context.Context, installRoot string) (*SetupResult, error)
}

// SetupResult describes what the standard hook did (AC2 verification).
type SetupResult struct {
	Migrated         bool `json:"migrated"`
	Seeded           bool `json:"seeded"`
	StorageLinked    bool `json:"storage_linked"`
	SuperAdminExists bool `json:"super_admin_exists"`
	ArtifactID       string `json:"artifact_id,omitempty"`
}

// MockStandardSetup simulates `php artisan migrate --force && php artisan db:seed` + storage:link.
// It is intentionally dumb-filesystem: no real PHP/DB required. The proof validates boundary,
// not Laravel runtime.
//
// Behaviour:
//   - Creates <installRoot>/database/database.sqlite (migrate)
//   - Creates <installRoot>/storage/app/seeded.json containing super-admin marker (seed)
//   - Creates symlink-or-marker <installRoot>/public/storage -> ../storage/app/public (storage:link)
//   - Idempotent: repeated calls with same installRoot succeed and leave markers intact.
//   - Failure injection: if FailNext is true, next Setup returns error simulating migrate fail.
//     Caller can verify installer rollback handling (AC4).
type MockStandardSetup struct {
	FailNext bool
	FailMsg  string
	Logger   io.Writer
}

// Setup implements StandardSetup.
func (m *MockStandardSetup) Setup(ctx context.Context, installRoot string) (*SetupResult, error) {
	if installRoot == "" {
		return nil, fmt.Errorf("installRoot empty")
	}
	if m.FailNext {
		m.FailNext = false
		msg := m.FailMsg
		if msg == "" {
			msg = "simulated migrate failure: SQLSTATE[HY000] connection failed"
		}
		if m.Logger != nil {
			fmt.Fprintf(m.Logger, "[standard-hook] FAIL: %s\n", msg)
		}
		return nil, fmt.Errorf("migrate --force failed: %s (actionable: check DB_HOST in .env, ensure database reachable)", msg)
	}
	// migrate --force: ensure database dir + sqlite file
	dbDir := filepath.Join(installRoot, "database")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir database: %w", err)
	}
	dbPath := filepath.Join(dbDir, "database.sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00 migrated at spike\n"), 0644); err != nil {
			return nil, fmt.Errorf("migrate write db: %w", err)
		}
	}
	// also touch a migrations marker
	migMarker := filepath.Join(dbDir, ".migrated")
	_ = os.WriteFile(migMarker, []byte("migrated"), 0644)

	// db:seed — super-admin marker
	seedDir := filepath.Join(installRoot, "storage", "app")
	if err := os.MkdirAll(seedDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir storage/app: %w", err)
	}
	seededPath := filepath.Join(seedDir, "seeded.json")
	seedContent := fmt.Sprintf(`{"super_admin":{"email":"admin@example.com","seeded":true},"artifact":"%s"}`, filepath.Base(installRoot))
	if err := os.WriteFile(seededPath, []byte(seedContent), 0644); err != nil {
		return nil, fmt.Errorf("seed write: %w", err)
	}
	// ensure public storage dir exists for symlink target
	publicTarget := filepath.Join(seedDir, "public")
	_ = os.MkdirAll(publicTarget, 0755)

	// storage:link — create public/storage link (or marker if symlink unsupported)
	publicDir := filepath.Join(installRoot, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir public: %w", err)
	}
	linkPath := filepath.Join(publicDir, "storage")
	// remove existing (idempotent)
	_ = os.Remove(linkPath)
	target := "../storage/app/public"
	// ensure target dir exists so symlink resolves
	_ = os.MkdirAll(filepath.Join(installRoot, "storage", "app", "public"), 0755)
	if err := os.Symlink(target, linkPath); err != nil {
		// fallback on Windows/filesystem that disallows symlink: write marker file
		_ = os.WriteFile(linkPath+".link", []byte(target), 0644)
		if m.Logger != nil {
			fmt.Fprintf(m.Logger, "[standard-hook] storage:link fallback marker (symlink failed: %v)\n", err)
		}
	} else {
		if m.Logger != nil {
			fmt.Fprintf(m.Logger, "[standard-hook] storage:link -> %s\n", target)
		}
	}

	if m.Logger != nil {
		fmt.Fprintf(m.Logger, "[standard-hook] migrate --force PASS, db:seed PASS (super-admin admin@example.com), storage:link PASS\n")
	}
	return &SetupResult{
		Migrated:         true,
		Seeded:           true,
		StorageLinked:    true,
		SuperAdminExists: true,
	}, nil
}

// VerifyStandardHookResults checks AC2 post-conditions: super-admin + storage:link.
func VerifyStandardHookResults(installRoot string) error {
	dbPath := filepath.Join(installRoot, "database", "database.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("migrate verify FAIL: database.sqlite missing (%w)", err)
	}
	seededPath := filepath.Join(installRoot, "storage", "app", "seeded.json")
	data, err := os.ReadFile(seededPath)
	if err != nil {
		return fmt.Errorf("seed verify FAIL: seeded.json missing (%w)", err)
	}
	if !contains(string(data), "super_admin") {
		return fmt.Errorf("seed verify FAIL: super_admin marker missing in seeded.json")
	}
	linkPath := filepath.Join(installRoot, "public", "storage")
	if _, err := os.Lstat(linkPath); err != nil {
		// check fallback marker
		if _, err2 := os.Stat(linkPath + ".link"); err2 != nil {
			return fmt.Errorf("storage:link verify FAIL: public/storage missing (%v, fallback also missing %v)", err, err2)
		}
	}
	return nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
