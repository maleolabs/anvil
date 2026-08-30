package runtime

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestRegistry_New verifies that NewRuntimeRegistry creates an empty registry.
//
// Reference: TS-P5-10
func TestRegistry_New(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")
	if r == nil {
		t.Fatal("NewRuntimeRegistry() returned nil")
	}
	if r.Len() != 0 {
		t.Errorf("expected empty registry, got %d entries", r.Len())
	}
}

// TestRegistry_RegisterAndGet verifies basic register and get operations.
//
// Reference: TS-P5-10 AC-1, AC-3
func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	entry := RuntimeEntry{
		ID:          RuntimeID("a1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "test-runtime",
		Environment: EnvProduction,
		InstallPath: "/opt/anvil/runtimes/test",
		Status:      StatusProvisioned,
	}

	if err := r.Register(entry); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	if r.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", r.Len())
	}

	got, err := r.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	if got.Name != entry.Name {
		t.Errorf("Get().Name = %q, want %q", got.Name, entry.Name)
	}
	if got.Environment != entry.Environment {
		t.Errorf("Get().Environment = %q, want %q", got.Environment, entry.Environment)
	}
	if got.InstallPath != entry.InstallPath {
		t.Errorf("Get().InstallPath = %q, want %q", got.InstallPath, entry.InstallPath)
	}
	if got.Status != entry.Status {
		t.Errorf("Get().Status = %q, want %q", got.Status, entry.Status)
	}
}

// TestRegistry_RegisterDuplicate verifies that registering a duplicate ID
// returns an error.
//
// Reference: TS-P5-10 AC-1
func TestRegistry_RegisterDuplicate(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	entry := RuntimeEntry{
		ID:          RuntimeID("a1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "first",
		Environment: EnvProduction,
		InstallPath: "/opt/anvil/runtimes/first",
		Status:      StatusProvisioned,
	}

	if err := r.Register(entry); err != nil {
		t.Fatalf("first Register() failed: %v", err)
	}

	dup := RuntimeEntry{
		ID:          entry.ID,
		Name:        "second",
		Environment: EnvStaging,
		InstallPath: "/opt/anvil/runtimes/second",
		Status:      StatusProvisioned,
	}

	err := r.Register(dup)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}

	if r.Len() != 1 {
		t.Errorf("expected 1 entry after duplicate registration, got %d", r.Len())
	}
}

// TestRegistry_Unregister verifies that unregistering an existing entry
// removes it from the registry.
//
// Reference: TS-P5-10 AC-2
func TestRegistry_Unregister(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	entry := RuntimeEntry{
		ID:          RuntimeID("b2c3d4e5-f6a7-4890-bcde-f12345678901"),
		Name:        "to-remove",
		Environment: EnvDevelopment,
		InstallPath: "/opt/anvil/runtimes/to-remove",
		Status:      StatusProvisioned,
	}

	if err := r.Register(entry); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	if err := r.Unregister(entry.ID); err != nil {
		t.Fatalf("Unregister() returned unexpected error: %v", err)
	}

	if r.Len() != 0 {
		t.Errorf("expected 0 entries after unregister, got %d", r.Len())
	}

	_, err := r.Get(entry.ID)
	if err == nil {
		t.Error("expected error getting unregistered entry, got nil")
	}
}

// TestRegistry_UnregisterUnknown verifies that unregistering an unknown ID
// returns an error.
//
// Reference: TS-P5-10 AC-2
func TestRegistry_UnregisterUnknown(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	err := r.Unregister(RuntimeID("unknown-id"))
	if err == nil {
		t.Fatal("expected error for unknown ID, got nil")
	}
}

// TestRegistry_GetUnknown verifies that getting an unknown ID returns an error.
//
// Reference: TS-P5-10 AC-3
func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	_, err := r.Get(RuntimeID("unknown-id"))
	if err == nil {
		t.Fatal("expected error for unknown ID, got nil")
	}
}

// TestRegistry_ListAllOrdering verifies that ListAll returns entries sorted
// by Name.
//
// Reference: TS-P5-10 AC-4
func TestRegistry_ListAllOrdering(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-runtimes.json")

	// Register entries in non-alphabetical order.
	entries := []RuntimeEntry{
		{
			ID:          RuntimeID("c0000000-0000-4000-8000-000000000001"),
			Name:        "charlie",
			Environment: EnvProduction,
			InstallPath: "/opt/anvil/runtimes/charlie",
			Status:      StatusProvisioned,
		},
		{
			ID:          RuntimeID("a0000000-0000-4000-8000-000000000002"),
			Name:        "alpha",
			Environment: EnvStaging,
			InstallPath: "/opt/anvil/runtimes/alpha",
			Status:      StatusProvisioned,
		},
		{
			ID:          RuntimeID("b0000000-0000-4000-8000-000000000003"),
			Name:        "bravo",
			Environment: EnvDevelopment,
			InstallPath: "/opt/anvil/runtimes/bravo",
			Status:      StatusProvisioned,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			t.Fatalf("Register(%q) failed: %v", e.Name, err)
		}
	}

	all := r.ListAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}

	// Verify alphabetical order.
	expectedNames := []string{"alpha", "bravo", "charlie"}
	for i, name := range expectedNames {
		if all[i].Name != name {
			t.Errorf("entry[%d].Name = %q, want %q", i, all[i].Name, name)
		}
	}
}

// TestRegistry_SaveLoad_RoundTrip verifies that entries can be saved to a
// file and loaded back.
//
// Reference: TS-P5-10 AC-5, AC-6
func TestRegistry_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtimes.json")

	r := NewRuntimeRegistry(path)

	entry := RuntimeEntry{
		ID:          RuntimeID("d4e5f6a7-b8c9-4090-cdef-123456789012"),
		Name:        "persist-test",
		Environment: EnvProduction,
		InstallPath: "/opt/anvil/runtimes/persist-test",
		Status:      StatusActive,
	}

	if err := r.Register(entry); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	if err := r.Save(); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Load into a new registry.
	r2 := NewRuntimeRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if r2.Len() != 1 {
		t.Fatalf("expected 1 entry after load, got %d", r2.Len())
	}

	got, err := r2.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get() after load returned error: %v", err)
	}

	if got.Name != entry.Name {
		t.Errorf("after load, Name = %q, want %q", got.Name, entry.Name)
	}
	if got.Status != entry.Status {
		t.Errorf("after load, Status = %q, want %q", got.Status, entry.Status)
	}
}

// TestRegistry_SaveLoad_MultipleEntries verifies that multiple entries are
// correctly preserved across save/load.
func TestRegistry_SaveLoad_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtimes.json")

	r := NewRuntimeRegistry(path)

	entries := []RuntimeEntry{
		{
			ID:          RuntimeID("e5f6a7b8-c9d0-40a0-def1-234567890123"),
			Name:        "web",
			Environment: EnvProduction,
			InstallPath: "/opt/anvil/runtimes/web",
			Status:      StatusActive,
		},
		{
			ID:          RuntimeID("f6a7b8c9-d0e1-40b0-ef12-345678901234"),
			Name:        "worker",
			Environment: EnvStaging,
			InstallPath: "/opt/anvil/runtimes/worker",
			Status:      StatusReady,
		},
		{
			ID:          RuntimeID("a7b8c9d0-e1f2-40c0-f123-456789012345"),
			Name:        "cron",
			Environment: EnvDevelopment,
			InstallPath: "/opt/anvil/runtimes/cron",
			Status:      StatusProvisioned,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			t.Fatalf("Register(%q) failed: %v", e.Name, err)
		}
	}

	if err := r.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	r2 := NewRuntimeRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if r2.Len() != 3 {
		t.Errorf("expected 3 entries after load, got %d", r2.Len())
	}

	// Verify all entries are present.
	for _, e := range entries {
		got, err := r2.Get(e.ID)
		if err != nil {
			t.Errorf("Get(%q) after load returned error: %v", e.ID, err)
			continue
		}
		if got.Name != e.Name {
			t.Errorf("after load, %q Name = %q, want %q", e.ID, got.Name, e.Name)
		}
	}
}

// TestRegistry_Load_FileNotFound verifies that Load returns an error when
// the file does not exist.
//
// Reference: TS-P5-10 AC-6
func TestRegistry_Load_FileNotFound(t *testing.T) {
	r := NewRuntimeRegistry("/nonexistent/path/runtimes.json")
	err := r.Load()
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestRegistry_ConcurrentAccess verifies that the registry is safe for
// concurrent access by multiple goroutines.
//
// Reference: TS-P5-10
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-concurrent.json")

	var wg sync.WaitGroup
	n := 20

	// Concurrently register entries.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			entry := RuntimeEntry{
				ID:          RuntimeID(fmt.Sprintf("id-%04d-0000-4000-8000-000000000000", idx)),
				Name:        fmt.Sprintf("runtime-%04d", idx),
				Environment: EnvProduction,
				InstallPath: "/opt/anvil/runtimes/test",
				Status:      StatusProvisioned,
			}
			_ = r.Register(entry)
		}(i)
	}
	wg.Wait()

	if r.Len() != n {
		t.Errorf("expected %d entries after concurrent register, got %d", n, r.Len())
	}

	// Concurrently read all entries.
	wg2 := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			_ = r.ListAll()
		}()
	}
	wg2.Wait()

	// Verify count is still correct.
	if r.Len() != n {
		t.Errorf("expected %d entries after concurrent reads, got %d", n, r.Len())
	}
}

// TestRegistry_EmptyList verifies that ListAll returns an empty slice for
// an empty registry.
func TestRegistry_EmptyList(t *testing.T) {
	r := NewRuntimeRegistry("/tmp/test-empty.json")
	all := r.ListAll()
	if all == nil {
		t.Fatal("ListAll() returned nil, expected empty slice")
	}
	if len(all) != 0 {
		t.Errorf("expected empty list, got %d entries", len(all))
	}
}

// TestDefaultRegistryPath verifies that DefaultRegistryPath returns a
// non-empty path using the default install root.
func TestDefaultRegistryPath(t *testing.T) {
	path := DefaultRegistryPath()
	if path == "" {
		t.Fatal("DefaultRegistryPath() returned empty string")
	}
	if path != DefaultInstallRoot+"/runtimes.json" {
		t.Errorf("DefaultRegistryPath() = %q, want %q", path, DefaultInstallRoot+"/runtimes.json")
	}
}

// TestRegistry_Save_NoDirectory verifies that Save returns an error when
// the parent directory does not exist.
func TestRegistry_Save_NoDirectory(t *testing.T) {
	r := NewRuntimeRegistry("/nonexistent/dir/runtimes.json")
	entry := RuntimeEntry{
		ID:          RuntimeID("a1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "test",
		Environment: EnvProduction,
		InstallPath: "/opt/anvil/runtimes/test",
		Status:      StatusProvisioned,
	}
	if err := r.Register(entry); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}
	if err := r.Save(); err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
}
