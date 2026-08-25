package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/server"
)

// TestServerProjectRegisterCommand_RegistersUnderServerProject verifies that:
//
//	anvil server project register
//
// is registered as a subcommand under "anvil server project".
func TestServerProjectRegisterCommand_RegistersUnderServerProject(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"server", "project", "register"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"project\", \"register\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"project\", \"register\"]) returned nil command")
	}
	if sub.Use != "register" {
		t.Errorf("command Use = %q, want %q", sub.Use, "register")
	}

	// Verify it's nested under project (parent is serverProjectCmd).
	if sub.Parent() == nil || sub.Parent().Use != "project" {
		t.Errorf("register command parent = %v, want project subcommand", sub.Parent())
	}
}

// TestServerProjectRegisterCommand_InteractiveRegistration verifies that:
//
//	anvil server project register --server-root <dir>
//
// in interactive mode (stdin prompts) creates the project registry file.
func TestServerProjectRegisterCommand_InteractiveRegistration(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Interactive prompt inputs (one per line):
	//   Project ID: test-project
	//   Install root: (default /srv/apps)
	//   Display name: (empty)
	//   Adapter: (empty)
	//   Confirm: y
	input := "test-project\n\n\n\ny\n"

	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(strings.NewReader(input))
	defer rootCmd.SetIn(oldIn)

	_, stdout, stderr, err := executeCommand("server", "project", "register", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "test-project registered at") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Verify the project file was created.
	store := server.NewRegistryStore(dir)
	if !store.Exists("test-project") {
		t.Error("project was not registered on disk")
	}
}

// TestServerProjectRegisterCommand_RejectsDuplicate verifies that registering
// a project ID that already exists returns an error.
func TestServerProjectRegisterCommand_RejectsDuplicate(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// First registration should succeed (non-interactive).
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "dup-project",
		"--install-root", "/srv/dup-project",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("first registration failed: %v\nstderr: %s", err, stderr)
	}

	// Second registration should fail with duplicate error.
	_, _, stderr2, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "dup-project",
		"--install-root", "/srv/dup-project",
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}

	if !contains(stderr2, "already registered") {
		t.Errorf("stderr should contain 'already registered', got: %s", stderr2)
	}
}

// TestServerProjectRegisterCommand_NoProjectRequired verifies that:
//
//	anvil server project register --server-root <dir>
//
// works without requiring a project context (no anvil.yaml, no project
// discovery, no current working directory constraints).
func TestServerProjectRegisterCommand_NoProjectRequired(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// This test ensures no RequireProject or cwd-based discovery is used.
	// We run from /tmp which has no anvil.yaml.
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "no-cwd-project",
		"--install-root", "/srv/no-cwd-project",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Should not mention project, anvil.yaml, or RequireProject.
	if contains(stderr, "anvil.yaml") {
		t.Errorf("stderr should not mention anvil.yaml, got: %s", stderr)
	}
	if contains(stderr, "RequireProject") {
		t.Errorf("stderr should not mention RequireProject, got: %s", stderr)
	}
}

// TestServerProjectRegisterCommand_NonInteractive verifies that:
//
//	anvil server project register --server-root <dir> --project-id <id>
//	  --install-root <path> --non-interactive
//
// registers the project without interactive prompts.
func TestServerProjectRegisterCommand_NonInteractive(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "my-app",
		"--install-root", "/srv/my-app",
		"--display-name", "My App",
		"--adapter", "laravel",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "my-app registered at") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Verify the project file was created.
	store := server.NewRegistryStore(dir)
	if !store.Exists("my-app") {
		t.Error("project was not registered on disk")
	}

	// Load and verify the saved values.
	cfg, err := store.Load("my-app")
	if err != nil {
		t.Fatalf("failed to load registered project: %v", err)
	}
	if cfg.Project.ID != "my-app" {
		t.Errorf("ID = %q, want %q", cfg.Project.ID, "my-app")
	}
	if cfg.Project.DisplayName != "My App" {
		t.Errorf("DisplayName = %q, want %q", cfg.Project.DisplayName, "My App")
	}
	if cfg.Project.InstallRoot != "/srv/my-app" {
		t.Errorf("InstallRoot = %q, want %q", cfg.Project.InstallRoot, "/srv/my-app")
	}
	if cfg.Project.Adapter != "laravel" {
		t.Errorf("Adapter = %q, want %q", cfg.Project.Adapter, "laravel")
	}

	// Registration performs no directory provisioning and writes no
	// ownership/shared-links keys (ADR-031 §3, TS-015-04-02).
	registryData, err := os.ReadFile(store.ProjectPath("my-app"))
	if err != nil {
		t.Fatalf("read registered project file: %v", err)
	}
	for _, demotedKey := range []string{"owner", "group", "shared_links"} {
		if strings.Contains(string(registryData), demotedKey) {
			t.Errorf("registered project file carries demoted key %q (ADR-031 §3)", demotedKey)
		}
	}
	// The install root directory must NOT be created by registration —
	// provisioning is deferred to external tools.
	if _, err := os.Stat("/srv/my-app"); !os.IsNotExist(err) {
		t.Errorf("registration must not provision the install root directory, stat err = %v", err)
	}
}

// TestServerProjectRegisterCommand_CanonicalStandardFlag verifies that
// the canonical project.standard key is written when --standard is
// passed (TS-019-02-01, ADR-032).
func TestServerProjectRegisterCommand_CanonicalStandardFlag(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "std-app",
		"--install-root", "/srv/std-app",
		"--standard", "laravel",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "std-app registered at") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Load and verify the canonical key was written.
	store := server.NewRegistryStore(dir)
	cfg, err := store.Load("std-app")
	if err != nil {
		t.Fatalf("failed to load registered project: %v", err)
	}
	if cfg.Project.Standard != "laravel" {
		t.Errorf("Standard = %q, want %q (canonical project.standard)", cfg.Project.Standard, "laravel")
	}
	if cfg.Project.Adapter != "" {
		t.Errorf("Adapter = %q, want empty when the canonical key is declared", cfg.Project.Adapter)
	}
	if got := cfg.Project.StandardName(); got != "laravel" {
		t.Errorf("StandardName() = %q, want %q", got, "laravel")
	}

	// The registry file must carry the canonical key.
	registryData, err := os.ReadFile(store.ProjectPath("std-app"))
	if err != nil {
		t.Fatalf("read registered project file: %v", err)
	}
	if !strings.Contains(string(registryData), "standard: laravel") {
		t.Errorf("registered project file does not carry the canonical standard key: %s", registryData)
	}
}

// TestServerProjectRegisterCommand_LegacyAdapterFlagStillWorks verifies
// backward compatibility: the legacy --adapter flag still registers a
// project whose project.adapter key is honored by resolution
// (TS-019-02-01 — adoption must not break existing declarations).
func TestServerProjectRegisterCommand_LegacyAdapterFlagStillWorks(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "legacy-app",
		"--install-root", "/srv/legacy-app",
		"--adapter", "node",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	store := server.NewRegistryStore(dir)
	cfg, err := store.Load("legacy-app")
	if err != nil {
		t.Fatalf("failed to load registered project: %v", err)
	}
	if cfg.Project.Adapter != "node" {
		t.Errorf("Adapter = %q, want %q", cfg.Project.Adapter, "node")
	}
	if got := cfg.Project.StandardName(); got != "node" {
		t.Errorf("StandardName() = %q, want %q (legacy project.adapter honored)", got, "node")
	}
}

// TestServerProjectRegisterCommand_BothStandardAndAdapterRejected
// verifies that declaring both --standard and --adapter is rejected —
// the rename policy is explicit, never a silent preference (ADR-032,
// TS-019-02-01).
func TestServerProjectRegisterCommand_BothStandardAndAdapterRejected(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "both-app",
		"--install-root", "/srv/both-app",
		"--standard", "laravel",
		"--adapter", "node",
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error when both --standard and --adapter are declared, got nil")
	}
	if !contains(stderr, "mutually exclusive") {
		t.Errorf("stderr should mention the mutual exclusion, got: %s", stderr)
	}

	// No project file may be written.
	store := server.NewRegistryStore(dir)
	if store.Exists("both-app") {
		t.Error("project must not be registered when the declaration is ambiguous")
	}
}

// TestServerProjectRegisterCommand_DemotedFlagsRejected verifies that the
// demoted ownership/shared-link flags are no longer part of the register
// command surface (ADR-031 §3, TS-015-04-02): passing them must fail.
func TestServerProjectRegisterCommand_DemotedFlagsRejected(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	for _, flag := range []string{"--owner", "--group", "--shared-link"} {
		_, _, _, err := executeCommand(
			"server", "project", "register",
			"--server-root", dir,
			"--project-id", "flag-test",
			"--install-root", "/srv/flag-test",
			flag, "x",
			"--non-interactive",
		)
		if err == nil {
			t.Errorf("register with %s must fail — the flag is demoted (ADR-031 §3)", flag)
		}
	}
}

// TestServerProjectRegisterCommand_MissingRequiredFlags verifies that:
//
//	anvil server project register --non-interactive
//
// (without required --project-id and --install-root) produces validation
// errors.
func TestServerProjectRegisterCommand_MissingRequiredFlags(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Missing both --project-id and --install-root.
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error for missing required flags, got nil")
	}

	// Should mention project.id is required.
	if !contains(stderr, "project.id is required") {
		t.Errorf("stderr should mention missing project.id, got: %s", stderr)
	}
}

// TestServerProjectRegisterCommand_NonInteractiveDuplicate verifies that:
//
//	anvil server project register --non-interactive
//
// rejects a duplicate project ID and does not mutate the existing file.
func TestServerProjectRegisterCommand_NonInteractiveDuplicate(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// First registration.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "unique-app",
		"--install-root", "/srv/unique-app",
		"--display-name", "Original",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Read the original file content before attempting to mutate.
	store := server.NewRegistryStore(dir)
	projectPath := store.ProjectPath("unique-app")
	originalContent, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("reading project file: %v", err)
	}

	// Second registration with different display name should fail.
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "unique-app",
		"--install-root", "/srv/unique-app",
		"--display-name", "Mutated",
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}

	if !contains(stderr, "already registered") {
		t.Errorf("stderr should contain 'already registered', got: %s", stderr)
	}

	// Verify the file was NOT mutated.
	currentContent, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("reading project file after failed registration: %v", err)
	}
	if string(currentContent) != string(originalContent) {
		t.Error("project file was mutated after duplicate registration attempt")
	}
}

// TestServerProjectRegisterCommand_ServerRootFlag verifies that:
//
//	anvil server project register --server-root <dir>
//
// creates the project file under the specified server root.
func TestServerProjectRegisterCommand_ServerRootFlag(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "root-test",
		"--install-root", "/srv/root-test",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify the project file exists at the expected path.
	expectedPath := filepath.Join(dir, "projects", "root-test.yaml")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("project file was not created at %s", expectedPath)
	}

	// Should not be at the default /etc/anvil path.
	defaultPath := filepath.Join("/etc/anvil", "projects", "root-test.yaml")
	if defaultPath != expectedPath {
		// Just sanity check — the file should exist under our dir, not /etc/anvil.
		if _, err := os.Stat(defaultPath); err == nil {
			t.Error("project file should not be created at default /etc/anvil path")
		}
	}

	// Stderr should contain a warning about non-default root.
	if !contains(stderr, "non-default server root") {
		t.Errorf("stderr should contain warning about non-default root, got: %s", stderr)
	}

	// Verify --server-root flag exists on this command (not on parent).
	registerCmd, _, err := rootCmd.Find([]string{"server", "project", "register"})
	if err != nil {
		t.Fatalf("failed to find register command: %v", err)
	}
	flag := registerCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the server project register subcommand")
	}
}

// TestServerProjectRegisterCommand_NotInitialized verifies that:
//
//	anvil server project register --server-root <dir>
//
// without running 'server init' first returns an error about the Runtime
// not being initialized.
func TestServerProjectRegisterCommand_NotInitialized(t *testing.T) {
	dir := t.TempDir()

	// Register without initializing — should fail.
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "no-init",
		"--install-root", "/srv/no-init",
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error when Runtime not initialized, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should mention Runtime not initialized, got: %s", stderr)
	}
}

// TestServerProjectRegisterCommand_AcceptDefaultInstallRoot verifies that
// interactive mode accepts the default install root (/srv/apps) when the
// user enters an empty value.
func TestServerProjectRegisterCommand_AcceptDefaultInstallRoot(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Interactive input: project-id, install-root (empty = default), then
	// skip all optional fields, and confirm.
	input := "default-root-test\n\n\n\ny\n"

	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(strings.NewReader(input))
	defer rootCmd.SetIn(oldIn)

	_, stdout, stderr, err := executeCommand("server", "project", "register", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "registered at") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Verify the project used /srv/apps as the default install root.
	store := server.NewRegistryStore(dir)
	cfg, err := store.Load("default-root-test")
	if err != nil {
		t.Fatalf("failed to load project: %v", err)
	}
	if cfg.Project.InstallRoot != "/srv/apps" {
		t.Errorf("InstallRoot = %q, want %q", cfg.Project.InstallRoot, "/srv/apps")
	}
}

// TestServerProjectRegisterCommand_InteractiveCancellation verifies that
// interactive mode cancels registration when the user answers "n" to the
// confirmation prompt.
func TestServerProjectRegisterCommand_InteractiveCancellation(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Interactive input: all fields, but answer "n" to confirmation.
	input := "cancel-test\n/srv/cancel-test\n\n\nn\n"

	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(strings.NewReader(input))
	defer rootCmd.SetIn(oldIn)

	_, stdout, stderr, err := executeCommand("server", "project", "register", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Registration cancelled") {
		t.Errorf("stdout should contain cancellation message, got: %s", stdout)
	}

	// Verify no project file was created.
	store := server.NewRegistryStore(dir)
	if store.Exists("cancel-test") {
		t.Error("project should not have been registered after cancellation")
	}
}
