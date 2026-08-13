package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Test Env ─────────────────────────────────────────────────────────

// skillTestEnv isolates the skill commands' global state for one test:
// XDG_CONFIG_HOME → temp (record stores), HOME → temp (global scope +
// agent detection), and the compatibility matrix env for the standard
// path gates.
func skillTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	matrixPath, err := filepath.Abs(filepath.Join("..", "docs", "specification-corpus", "compatibility-matrix.json"))
	if err != nil {
		t.Fatalf("resolve compatibility matrix path: %v", err)
	}
	t.Setenv(registry.EnvCompatibilityMatrix, matrixPath)
}

// skillTestExitCode extracts the deterministic exit code from a command
// error (TS-P8-07).
func skillTestExitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return output.ExitCodeSuccess
	}
	var appErr *output.AppError
	if errors.As(err, &appErr) {
		return appErr.ExitCode()
	}
	return output.ExitCodeGeneral
}

// skillTestChdir changes the working directory for the duration of the
// test.
func skillTestChdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// skillTestReadSkillRecord reads the installed-skills record for a name
// under the isolated config dir.
func skillTestReadSkillRecord(t *testing.T, name string) registry.InstalledSkillRecord {
	t.Helper()
	store, err := skillStore()
	if err != nil {
		t.Fatalf("skillStore: %v", err)
	}
	rec, err := store.Get(name)
	if err != nil {
		t.Fatalf("read skill record %s: %v", name, err)
	}
	return rec
}

// ── Command Group Registration ───────────────────────────────────────

// TestSkillDomainGroup_Development verifies the skill group is listed
// under the Development product domain in rootDomainGroups (the same
// grouping as standard/adapter — ST-008-001: the top-level help shows
// every command group), and not under System.
func TestSkillDomainGroup_Development(t *testing.T) {
	var development, system *domainGroup
	for i := range rootDomainGroups {
		switch rootDomainGroups[i].Name {
		case "Development":
			development = &rootDomainGroups[i]
		case "System":
			system = &rootDomainGroups[i]
		}
	}
	if development == nil {
		t.Fatal("Development domain group must exist")
	}
	if system == nil {
		t.Fatal("System domain group must exist")
	}

	if !containsString(development.Commands, "skill") {
		t.Errorf("skill must be grouped under Development, got: %v", development.Commands)
	}
	if containsString(system.Commands, "skill") {
		t.Errorf("skill must not be grouped under System, got: %v", system.Commands)
	}
}

// TestSkillDomainGroup_HelpOutput verifies the rendered top-level help
// places the skill command inside the Development section (the bare
// "anvil" and "anvil --help" surfaces — ST-008-001). The help block is
// scanned line by line using the section headers as boundaries, so the
// test is not brittle to section reordering (mirrors
// TestAdapterDomainGroup_HelpOutput).
func TestSkillDomainGroup_HelpOutput(t *testing.T) {
	_, stdout, _, err := executeCommand()
	if err != nil {
		t.Fatalf("bare 'anvil' help must succeed: %v", err)
	}

	// Restrict scanning to the domain-help block.
	block := stdout
	if start := strings.Index(stdout, "Product Domains:"); start >= 0 {
		block = stdout[start:]
	}
	if end := strings.Index(block, `Use "anvil [command] --help"`); end >= 0 {
		block = block[:end]
	}

	var devEntries, sysEntries []string
	section := ""
	for _, line := range strings.Split(block, "\n") {
		switch {
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    "):
			// Two-space indented: a domain section header.
			section = strings.TrimSpace(line)
		case strings.HasPrefix(line, "    "):
			// Four-space indented: a command entry of the current section.
			switch section {
			case "Development":
				devEntries = append(devEntries, strings.TrimSpace(line))
			case "System":
				sysEntries = append(sysEntries, strings.TrimSpace(line))
			}
		}
	}

	if !containsCommandEntry(devEntries, "skill") {
		t.Errorf("Development section must list the skill command, got entries: %v", devEntries)
	}
	if containsCommandEntry(sysEntries, "skill") {
		t.Errorf("System section must not list the skill command, got entries: %v", sysEntries)
	}
}

// TestSkillCommand_Registered verifies the skill group and its four
// subcommands are registered and listed in the group help.
func TestSkillCommand_Registered(t *testing.T) {
	for _, sub := range []string{"list", "install", "update", "uninstall"} {
		if _, _, err := rootCmd.Find([]string{"skill", sub}); err != nil {
			t.Fatalf("skill %s command not found: %v", sub, err)
		}
	}
	_, helpOut, _, err := executeCommand("skill", "--help")
	if err != nil {
		t.Fatalf("skill --help failed: %v", err)
	}
	for _, sub := range []string{"list", "install", "update", "uninstall"} {
		if !strings.Contains(helpOut, sub) {
			t.Errorf("skill group help does not list the %s subcommand:\n%s", sub, helpOut)
		}
	}
	// The help documents that --force is destructive.
	if !strings.Contains(helpOut, "destructive") {
		t.Errorf("skill group help does not document that --force is destructive:\n%s", helpOut)
	}
}

// ── Core Install ─────────────────────────────────────────────────────

// TestSkillInstall_Core_Global installs the embedded core skill
// end-to-end: the content lands at the agent target with the provenance
// header, the record pins the CLI version as the skill version, and the
// resolution records the core source.
func TestSkillInstall_Core_Global(t *testing.T) {
	skillTestEnv(t)
	// A detected agent for the --agent default.
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stdout, stderr, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	if !strings.Contains(stdout, "Installed skill: anvil-overview") {
		t.Errorf("stdout missing success line:\n%s", stdout)
	}

	// Lone claude-code install: native copy at ~/.claude/skills/<name>.
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	md, err := os.ReadFile(filepath.Join(native, "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(md), "# source: core "+CliVersion) {
		t.Errorf("installed SKILL.md lacks the provenance header (source: core %s):\n%s", CliVersion, md)
	}
	if _, err := os.Stat(filepath.Join(native, ".anvil-install")); err != nil {
		t.Errorf("ownership marker missing: %v", err)
	}

	rec := skillTestReadSkillRecord(t, "anvil-overview")
	if rec.Source != registry.SkillSourceCore {
		t.Errorf("record source = %q, want %q", rec.Source, registry.SkillSourceCore)
	}
	if rec.Version != CliVersion {
		t.Errorf("record version = %q, want the CLI version %q (core skills are lockstep)", rec.Version, CliVersion)
	}
	if rec.Resolution.Kind != registry.SkillResolutionKindCore {
		t.Errorf("record resolution kind = %q, want %q", rec.Resolution.Kind, registry.SkillResolutionKindCore)
	}
	if len(rec.Targets) == 0 || rec.Targets[0].Path != native {
		t.Errorf("record targets = %+v, want the native target %s", rec.Targets, native)
	}
	if !rec.InstalledAt.Equal(rec.UpdatedAt) {
		t.Errorf("fresh record installedAt %v != updatedAt %v", rec.InstalledAt, rec.UpdatedAt)
	}
}

// TestSkillInstall_Core_GlobalJSON verifies the --json envelope: stdout
// is exactly one JSON envelope (no StepReporter pollution), carrying the
// identity, source, version, and targets.
func TestSkillInstall_Core_GlobalJSON(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stdout, stderr, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--json")
	if err != nil {
		t.Fatalf("install failed: %v (stderr: %q)", err, stderr)
	}
	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Data    struct {
			Name    string `json:"name"`
			Source  string `json:"source"`
			Version string `json:"version"`
			Scope   string `json:"scope"`
			Targets []struct {
				Agent string `json:"agent"`
				Scope string `json:"scope"`
				Path  string `json:"path"`
			} `json:"targets"`
			RecordPath string `json:"record_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not a JSON envelope (progress polluted it?): %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		t.Errorf("envelope = %+v, want version 1 / success", envelope)
	}
	if envelope.Data.Name != "anvil-overview" || envelope.Data.Source != "core" || envelope.Data.Version != CliVersion {
		t.Errorf("data identity = %+v, want name anvil-overview / source core / version %s", envelope.Data, CliVersion)
	}
	if len(envelope.Data.Targets) == 0 || envelope.Data.Targets[0].Path == "" {
		t.Errorf("data targets = %+v, want recorded target paths", envelope.Data.Targets)
	}
	if envelope.Data.RecordPath == "" {
		t.Error("data record_path is empty")
	}
	// The StepReporter runs to stderr — it must never reach stdout.
	if strings.Contains(stdout, "Step:") || strings.Contains(stdout, "Installing") {
		t.Errorf("stdout carries StepReporter progress (envelope polluted):\n%s", stdout)
	}
}

// TestSkillInstall_Core_RepoScope_RequiresProject verifies the repo
// scope precondition: without an Anvil project, the install fails with
// exit 4 and an actionable message.
func TestSkillInstall_Core_RepoScope_RequiresProject(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	skillTestChdir(t, work)

	_, _, stderr, err := executeCommand("skill", "install", "anvil-overview")
	if err == nil {
		t.Fatal("repo install without a project: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodePrecondition {
		t.Errorf("exit code = %d, want %d (precondition)", code, output.ExitCodePrecondition)
	}
	if !strings.Contains(stderr, "anvil init") {
		t.Errorf("stderr not actionable (no anvil init hint):\n%s", stderr)
	}
}

// TestSkillInstall_Core_NoAgentDetected verifies the auto-detect
// precondition: with no agent config folders, install fails with exit 4.
func TestSkillInstall_Core_NoAgentDetected(t *testing.T) {
	skillTestEnv(t)

	_, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err == nil {
		t.Fatal("auto-detect on an empty machine: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodePrecondition {
		t.Errorf("exit code = %d, want %d (precondition)", code, output.ExitCodePrecondition)
	}
}

// TestSkillInstall_Core_ConflictAndForce verifies the conflict gate
// (exit 2, actionable) and the destructive --force escape.
func TestSkillInstall_Core_ConflictAndForce(t *testing.T) {
	skillTestEnv(t)
	// A user's same-name skill occupies the master location.
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(master, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(master, "user.txt"), []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode")
	if err == nil {
		t.Fatal("conflict not blocked")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeConfig {
		t.Errorf("exit code = %d, want %d (conflict)", code, output.ExitCodeConfig)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("conflict error not actionable (no --force hint):\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(master, "user.txt")); err != nil {
		t.Fatalf("blocked install must not touch the occupant: %v", err)
	}

	// --force replaces the occupant.
	_, _, _, err = executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode", "--force")
	if err != nil {
		t.Fatalf("--force did not escape the conflict: %v", err)
	}
	if _, err := os.Stat(filepath.Join(master, "user.txt")); !os.IsNotExist(err) {
		t.Errorf("--force left the user's file behind (user.txt still present)")
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Errorf("--force install did not write the skill content: %v", err)
	}
}

// TestSkillInstall_Core_AlreadyInstalled verifies idempotency: a second
// install of the same version re-validates, reports "already installed",
// and preserves the record (exit 0).
func TestSkillInstall_Core_AlreadyInstalled(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err != nil {
		t.Fatal(err)
	}
	first := skillTestReadSkillRecord(t, "anvil-overview")

	_, stdout, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err != nil {
		t.Fatalf("re-install failed: %v", err)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("re-install does not report 'already installed':\n%s", stdout)
	}
	second := skillTestReadSkillRecord(t, "anvil-overview")
	if !first.InstalledAt.Equal(second.InstalledAt) {
		t.Errorf("re-install changed installedAt: %v → %v", first.InstalledAt, second.InstalledAt)
	}
}

// TestSkillInstall_Core_VersionConflict verifies that installing a skill
// recorded at a different version is rejected (exit 2) with the update
// hint — install never changes versions.
func TestSkillInstall_Core_VersionConflict(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-record the skill at a different version (as an older CLI would).
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	target := filepath.Join(t.TempDir(), "anvil-overview")
	if _, _, err := store.Record("anvil-overview", registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            "anvil-overview",
		Version:       "9.9.9",
		Source:        registry.SkillSourceCore,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindCore, Source: "embedded"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: target,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err == nil {
		t.Fatal("version conflict not rejected")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeConfig {
		t.Errorf("exit code = %d, want %d (version conflict)", code, output.ExitCodeConfig)
	}
	if !strings.Contains(stderr, "skill update") {
		t.Errorf("version-conflict error not actionable (no update hint):\n%s", stderr)
	}
	// P2: the rejection is a PREFLIGHT — zero side effects: no target
	// content was materialized for the conflicting install.
	for _, dir := range []string{
		filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview"),
		filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview"),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("version-conflict install wrote content to %s (must be a preflight rejection)", dir)
		}
	}
}

// TestSkillInstall_Core_ReinstallRefreshesTargets verifies the re-install
// of the same version with a different --agent refreshes the target set
// like an update (P3): targets dropped by the narrower agent set are
// removed from disk — never orphaned — and the record stays consistent.
func TestSkillInstall_Core_ReinstallRefreshesTargets(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Wide install: master + native symlink for claude.
	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "all"); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(native, "SKILL.md")); err != nil {
		t.Fatalf("native symlink not materialized: %v", err)
	}

	// Narrow re-install: opencode only — the claude native target is
	// dropped and must be removed.
	_, stdout, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode")
	if err != nil {
		t.Fatalf("re-install failed: %v", err)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("re-install does not report 'already installed':\n%s", stdout)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Errorf("dropped native target %s was orphaned by the re-install", native)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Errorf("kept master %s missing after the re-install: %v", master, err)
	}

	rec := skillTestReadSkillRecord(t, "anvil-overview")
	if len(rec.Targets) != 2 || rec.Targets[0].Agent != "all" || rec.Targets[1].Agent != "opencode" {
		t.Errorf("record targets = %+v, want [all, opencode] only (no orphaned agent targets)", rec.Targets)
	}
	for _, tt := range rec.Targets {
		if tt.Path == native {
			t.Errorf("record still references the dropped native target %s", native)
		}
	}
}

// ── List ─────────────────────────────────────────────────────────────

// TestSkillList_AvailableThenInstalled tracks the status transition of
// the core skill across install.
func TestSkillList_AvailableThenInstalled(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "available") {
		t.Errorf("fresh list does not show the core skill as available:\n%s", stdout)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global"); err != nil {
		t.Fatal(err)
	}
	_, stdout, _, err = executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "installed") || !strings.Contains(stdout, ".claude/skills/anvil-overview") {
		t.Errorf("list after install lacks installed status / target path:\n%s", stdout)
	}
}

// TestSkillList_JSON exposes name, source, version, status, and target
// paths in the envelope (DoD).
func TestSkillList_JSON(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global"); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			CLIVersion string `json:"cli_version"`
			Skills     []struct {
				Name        string `json:"name"`
				Source      string `json:"source"`
				Version     string `json:"version"`
				Status      string `json:"status"`
				InstalledAt string `json:"installed_at"`
				Targets     []struct {
					Agent string `json:"agent"`
					Scope string `json:"scope"`
					Path  string `json:"path"`
				} `json:"targets"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q", envelope.Status)
	}
	if envelope.Data.CLIVersion != CliVersion {
		t.Errorf("cli_version = %q, want %q", envelope.Data.CLIVersion, CliVersion)
	}
	var found bool
	for _, s := range envelope.Data.Skills {
		if s.Name != "anvil-overview" {
			continue
		}
		found = true
		if s.Source != "core" || s.Version != CliVersion || s.Status != "installed" {
			t.Errorf("core entry = %+v, want source core / version %s / status installed", s, CliVersion)
		}
		if len(s.Targets) == 0 || s.Targets[0].Path == "" {
			t.Errorf("core entry targets = %+v, want the recorded target path", s.Targets)
		}
	}
	if !found {
		t.Error("list --json lacks the anvil-overview entry")
	}
}

// TestSkillList_StaleCoreAfterVersionSkew verifies the core stale status
// at the command level: a core skill recorded by an older CLI (version
// skew vs the current CLI version) is listed as stale with an update
// hint.
func TestSkillList_StaleCoreAfterVersionSkew(t *testing.T) {
	skillTestEnv(t)

	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	target := filepath.Join(t.TempDir(), "anvil-overview")
	if _, _, err := store.Record("anvil-overview", registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            "anvil-overview",
		Version:       "0.1.0", // older CLI
		Source:        registry.SkillSourceCore,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindCore, Source: "embedded"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: target,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "stale") {
		t.Errorf("core version skew not listed as stale:\n%s", stdout)
	}
	if !strings.Contains(stdout, "skill update") {
		t.Errorf("stale entry lacks the update hint:\n%s", stdout)
	}

	_, stdout, _, err = executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Skills []struct {
				Name    string   `json:"name"`
				Status  string   `json:"status"`
				Hints   []string `json:"hints"`
				Version string   `json:"version"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("list --json is not a JSON envelope: %v", err)
	}
	found := false
	for _, s := range envelope.Data.Skills {
		if s.Name != "anvil-overview" {
			continue
		}
		found = true
		if s.Status != "stale" || s.Version != "0.1.0" {
			t.Errorf("core entry = %+v, want status stale / version 0.1.0", s)
		}
		if len(s.Hints) == 0 {
			t.Error("stale core entry carries no hints")
		}
	}
	if !found {
		t.Error("list --json lacks the stale core entry")
	}
}

// TestSkillList_StaleStandardDeprecated verifies a standard-sourced skill
// whose source standard is deprecated is listed as stale.
func TestSkillList_StaleStandardDeprecated(t *testing.T) {
	skillTestEnv(t)

	// Installed standard record (deprecated) + a skill record sourced
	// from it.
	skillTestWriteStandardRecord(t, "anvil-standard-laravel", "1.2.3",
		"https://example.com/releases/1.2.3/release.tar.gz", registry.LifecycleStateDeprecated)

	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	target := filepath.Join(t.TempDir(), "overview")
	if _, _, err := store.Record("overview", registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            "overview",
		Version:       "1.0.0",
		Source:        "anvil-standard-laravel",
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindDistribution, Source: "https://example.com/releases/1.2.3/anvil-skill-overview-1-0-0"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: target,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "stale") || !strings.Contains(stdout, "deprecated") {
		t.Errorf("deprecated-source skill not listed as stale with a hint:\n%s", stdout)
	}
}

// TestSkillList_StaleStandardMissingSource verifies a standard-sourced
// skill whose source standard record is missing is listed as stale.
func TestSkillList_StaleStandardMissingSource(t *testing.T) {
	skillTestEnv(t)

	// NO installed-standard record for the source.
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	target := filepath.Join(t.TempDir(), "overview")
	if _, _, err := store.Record("overview", registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            "overview",
		Version:       "1.0.0",
		Source:        "anvil-standard-laravel",
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindDistribution, Source: "https://example.com/releases/1.2.3/anvil-skill-overview-1-0-0"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: target,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "stale") || !strings.Contains(stdout, "uninstall") {
		t.Errorf("missing-source skill not listed as stale with a hint:\n%s", stdout)
	}
}

// ── Core Update ──────────────────────────────────────────────────────

// TestSkillUpdate_Core_RefreshesAndPrunes verifies update = full target
// refresh: stale files (present in the old content, absent in the new)
// are pruned — never left behind — and the record keeps its installedAt
// while updatedAt moves.
func TestSkillUpdate_Core_RefreshesAndPrunes(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode"); err != nil {
		t.Fatal(err)
	}
	before := skillTestReadSkillRecord(t, "anvil-overview")
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")

	// Simulate drift in the installed content: a stale file from an old
	// version and a modified SKILL.md.
	stale := filepath.Join(master, "stale.txt")
	if err := os.WriteFile(stale, []byte("old version file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(master, "SKILL.md"), []byte("---\nname: anvil-overview\ndescription: tampered\n---\n# tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update without flags: scope + agents resolve from the record.
	_, stdout, _, err := executeCommand("skill", "update", "anvil-overview")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(stdout, "Updated skill: anvil-overview") {
		t.Errorf("stdout missing update line:\n%s", stdout)
	}

	// The stale file is pruned and SKILL.md is the refreshed embedded
	// content with provenance.
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file %s was not pruned by the update", stale)
	}
	md, err := os.ReadFile(filepath.Join(master, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "tampered") {
		t.Errorf("SKILL.md was not refreshed by the update:\n%s", md)
	}
	if !strings.Contains(string(md), "# source: core "+CliVersion) {
		t.Errorf("refreshed SKILL.md lacks the provenance header:\n%s", md)
	}

	after := skillTestReadSkillRecord(t, "anvil-overview")
	if !before.InstalledAt.Equal(after.InstalledAt) {
		t.Errorf("update changed installedAt: %v → %v (must be preserved)", before.InstalledAt, after.InstalledAt)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) && !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("update did not move updatedAt: %v → %v", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestSkillUpdate_Core_NotInstalled verifies update of an unrecorded
// skill fails with exit 3 (not found) — updates are explicit re-adoption.
func TestSkillUpdate_Core_NotInstalled(t *testing.T) {
	skillTestEnv(t)

	_, _, stderr, err := executeCommand("skill", "update", "anvil-overview")
	if err == nil {
		t.Fatal("update of an unrecorded skill: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeRuntime {
		t.Errorf("exit code = %d, want %d (not found)", code, output.ExitCodeRuntime)
	}
	if !strings.Contains(stderr, "install") {
		t.Errorf("not-found error not actionable (no install hint):\n%s", stderr)
	}
}

// ── Uninstall ────────────────────────────────────────────────────────

// TestSkillUninstall_RemovesContentAndRecord verifies uninstall removes
// every target (containment-checked) and the record.
func TestSkillUninstall_RemovesContentAndRecord(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Two agents: master (.agents/skills) + native symlink/copy.
	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "all"); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("master not materialized: %v", err)
	}

	_, stdout, _, err := executeCommand("skill", "uninstall", "anvil-overview")
	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if !strings.Contains(stdout, "Uninstalled skill") {
		t.Errorf("stdout missing uninstall line:\n%s", stdout)
	}
	if _, err := os.Stat(master); !os.IsNotExist(err) {
		t.Errorf("master %s still exists after uninstall", master)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Errorf("native target %s still exists after uninstall", native)
	}
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("anvil-overview"); !errors.Is(err, registry.ErrSkillRecordNotFound) {
		t.Errorf("record still exists after uninstall (err = %v)", err)
	}
}

// TestSkillUninstall_NotInstalled_Graceful verifies uninstalling an
// unrecorded skill is graceful (exit 0, nothing to do).
func TestSkillUninstall_NotInstalled_Graceful(t *testing.T) {
	skillTestEnv(t)

	_, stdout, _, err := executeCommand("skill", "uninstall", "anvil-overview")
	if err != nil {
		t.Fatalf("uninstall of an unrecorded skill: %v", err)
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeSuccess {
		t.Errorf("exit code = %d, want %d (graceful)", code, output.ExitCodeSuccess)
	}
	if !strings.Contains(stdout, "not installed") {
		t.Errorf("stdout lacks the graceful message:\n%s", stdout)
	}
}

// TestSkillUninstall_ContainmentRejects verifies the containment check: a
// hand-edited record target outside the scope base is refused before any
// RemoveAll.
func TestSkillUninstall_ContainmentRejects(t *testing.T) {
	skillTestEnv(t)

	// Record a target OUTSIDE the (temp) home: /tmp/<rand>/anvil-overview.
	outside := filepath.Join(t.TempDir(), "anvil-overview")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	if _, _, err := store.Record("anvil-overview", registry.InstalledSkillRecord{
		FormatVersion: registry.InstalledSkillRecordFormatVersion,
		ID:            "anvil-overview",
		Version:       CliVersion,
		Source:        registry.SkillSourceCore,
		Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindCore, Source: "embedded"},
		InstalledAt:   ts,
		UpdatedAt:     ts,
		Targets: []registry.InstalledSkillTarget{{
			Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: outside,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, _, stderr, err := executeCommand("skill", "uninstall", "anvil-overview")
	if err == nil {
		t.Fatal("uninstall of an out-of-base target: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (containment rejection)", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "refusing") {
		t.Errorf("containment error not explicit:\n%s", stderr)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("out-of-base target was removed despite the containment rejection: %v", err)
	}
}

// TestSkillUninstall_ScopeFilter verifies --scope filters the removed
// targets and keeps the record (with remaining targets) when targets
// remain.
func TestSkillUninstall_ScopeFilter(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode"); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")

	// Filter with a non-matching scope: nothing matches → exit 3.
	_, _, _, err := executeCommand("skill", "uninstall", "anvil-overview", "--scope", "repo")
	if err == nil {
		t.Fatal("filter with no match: expected error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeRuntime {
		t.Errorf("exit code = %d, want %d (no matching target)", code, output.ExitCodeRuntime)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("non-matching filter removed content: %v", err)
	}

	// Matching filter removes the target and the record.
	if _, _, _, err := executeCommand("skill", "uninstall", "anvil-overview", "--scope", "global"); err != nil {
		t.Fatalf("filtered uninstall failed: %v", err)
	}
	if _, err := os.Stat(master); !os.IsNotExist(err) {
		t.Errorf("master still exists after filtered uninstall")
	}
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("anvil-overview"); !errors.Is(err, registry.ErrSkillRecordNotFound) {
		t.Errorf("record still exists after uninstall (err = %v)", err)
	}
}

// TestSkillUpdate_Core_AgentExpansion verifies the target-kind
// transition on update: a lone-native install (real copy directory)
// expanded to --agent all becomes a master copy plus a native symlink —
// the recorded copy is pre-cleaned so the writer can publish the
// symlink.
func TestSkillUpdate_Core_AgentExpansion(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global"); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	if info, err := os.Lstat(native); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a lone-native real directory, got symlink=%v err=%v", info != nil && info.Mode()&os.ModeSymlink != 0, err)
	}

	if _, _, _, err := executeCommand("skill", "update", "anvil-overview", "--agent", "all"); err != nil {
		t.Fatalf("update --agent all failed: %v", err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("master not created by the expansion: %v", err)
	}
	info, err := os.Lstat(native)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("native target is not a symlink after the expansion (kind transition failed)")
	}
}

// TestSkillUpdate_Core_AgentReduction verifies the reverse transition:
// --agent all → --agent claude-code drops the master and converts the
// native symlink back into a lone real copy.
func TestSkillUpdate_Core_AgentReduction(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "all"); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("master not materialized: %v", err)
	}

	if _, _, _, err := executeCommand("skill", "update", "anvil-overview", "--agent", "claude-code"); err != nil {
		t.Fatalf("update --agent claude-code failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("master was not removed by the reduction (dropped target)")
	}
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	info, err := os.Lstat(native)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("native target is still a symlink after the reduction (kind transition failed)")
	}
	rec := skillTestReadSkillRecord(t, "anvil-overview")
	if len(rec.Targets) != 1 || rec.Targets[0].Agent != "claude-code" {
		t.Errorf("record targets = %+v, want only the claude-code target", rec.Targets)
	}
}

// TestSkillUninstall_PartialAgentKeepsSharedMaster verifies the
// MED-1/F-1 fix: a partial-agent uninstall (e.g. --agent opencode on an
// --agent all install) never removes the master copy still referenced by
// other agents — their symlinks must not dangle. A full uninstall then
// cleans everything.
func TestSkillUninstall_PartialAgentKeepsSharedMaster(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "all"); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")

	// Partial uninstall for a reader agent (opencode): its only path is
	// the shared master, referenced by other agents — nothing may be
	// removed, the master must stay intact, and the command explains the
	// shared-copy situation with a non-zero exit (the requested end state
	// is not achievable via a filter).
	_, _, stderr, err := executeCommand("skill", "uninstall", "anvil-overview", "--agent", "opencode")
	if err == nil {
		t.Fatal("partial uninstall of a shared master: expected an explanatory error")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	if !strings.Contains(stderr, "shared") {
		t.Errorf("partial-uninstall error not actionable (no shared-master explanation):\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("partial uninstall removed the shared master: %v", err)
	}
	if _, err := os.Stat(native); err != nil {
		t.Fatalf("partial uninstall left the claude symlink dangling (native gone): %v", err)
	}
	rec := skillTestReadSkillRecord(t, "anvil-overview")
	if len(rec.Targets) != 9 {
		t.Errorf("record targets = %d entries after a partial uninstall, want 9 (all agents intact)", len(rec.Targets))
	}

	// A partial uninstall of an agent with its OWN native target
	// (claude-code) removes that native target while the shared master
	// survives — the other reader agents keep working.
	if _, _, _, err := executeCommand("skill", "uninstall", "anvil-overview", "--agent", "claude-code"); err != nil {
		t.Fatalf("partial uninstall of the claude native target failed: %v", err)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Errorf("claude native target %s still exists after --agent claude-code uninstall", native)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("claude partial uninstall removed the shared master: %v", err)
	}
	rec = skillTestReadSkillRecord(t, "anvil-overview")
	if len(rec.Targets) != 8 {
		t.Errorf("record targets = %d entries after the claude partial uninstall, want 8", len(rec.Targets))
	}

	// Full uninstall removes everything.
	if _, _, _, err := executeCommand("skill", "uninstall", "anvil-overview"); err != nil {
		t.Fatalf("full uninstall failed: %v", err)
	}
	for _, p := range []string{master, native} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("full uninstall left %s behind", p)
		}
	}
	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("anvil-overview"); !errors.Is(err, registry.ErrSkillRecordNotFound) {
		t.Errorf("record still exists after full uninstall (err = %v)", err)
	}
}

// TestSkillUpdate_Core_ShapeTransitions verifies the LOW-2 fix: an
// update whose new content changes the SHAPE of a tree entry (a file
// where the old tree had a directory, and a directory where the old tree
// had a file) pre-cleans the conflicting entries — the update succeeds
// and the target tree matches the new content.
func TestSkillUpdate_Core_ShapeTransitions(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode"); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")

	// Drift into conflicting shapes:
	//   - SKILL.md (a file in the new content) becomes a DIRECTORY;
	//   - references (a directory in the new content) becomes a FILE.
	if err := os.RemoveAll(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(master, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(master, "SKILL.md", "junk.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(master, "references")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(master, "references"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := executeCommand("skill", "update", "anvil-overview"); err != nil {
		t.Fatalf("update across shape transitions failed: %v", err)
	}

	// SKILL.md is a file again (dir → file) and references is a directory
	// again with its content (file → dir). The junk.txt under the former
	// SKILL.md directory cannot survive: the conflicting directory was
	// removed in full before the write.
	info, err := os.Lstat(filepath.Join(master, "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md missing after the update: %v", err)
	}
	if info.IsDir() {
		t.Error("SKILL.md is still a directory after the update (dir → file transition failed)")
	}
	md, err := os.ReadFile(filepath.Join(master, "references", "REFERENCE.md"))
	if err != nil {
		t.Fatalf("references/REFERENCE.md missing after the update (file → dir transition failed): %v", err)
	}
	if !strings.Contains(string(md), "Anvil CLI Reference") {
		t.Errorf("references/REFERENCE.md content not refreshed:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md", "junk.txt")); err == nil {
		t.Error("conflicting directory under SKILL.md was not removed")
	}
}

// TestSkillList_JSONCorruptRecords verifies the corrupt_records field in
// the list --json envelope (MIN-1/F-5): an unreadable installed-skill
// record surfaces with its path and reason — never silently dropped.
func TestSkillList_JSONCorruptRecords(t *testing.T) {
	skillTestEnv(t)

	store, err := skillStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "anvil-overview.json"), []byte("this is not a record"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stdout, _, err := executeCommand("skill", "list", "--json")
	if err != nil {
		t.Fatalf("list with a corrupt record should still succeed: %v", err)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			CorruptRecords []struct {
				Path  string `json:"path"`
				Error string `json:"error"`
			} `json:"corrupt_records"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("list --json is not a JSON envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want success", envelope.Status)
	}
	if len(envelope.Data.CorruptRecords) != 1 {
		t.Fatalf("corrupt_records = %+v, want exactly 1 entry", envelope.Data.CorruptRecords)
	}
	cr := envelope.Data.CorruptRecords[0]
	if !strings.Contains(cr.Path, "anvil-overview.json") || cr.Error == "" {
		t.Errorf("corrupt record entry = %+v, want the record path and an error reason", cr)
	}
}

// TestSkillUpdate_ExitCodePrecondition verifies the update command's exit
// 4 paths (documented in the help): no selectable agent on --agent
// auto-detect, and a repo-scope base that cannot be resolved.
func TestSkillUpdate_ExitCodePrecondition(t *testing.T) {
	t.Run("auto-detect-no-agent", func(t *testing.T) {
		skillTestEnv(t) // no agent config folders anywhere

		// A recorded skill is required for update; write one directly.
		store, err := skillStore()
		if err != nil {
			t.Fatal(err)
		}
		ts := time.Now()
		if _, _, err := store.Record("anvil-overview", registry.InstalledSkillRecord{
			FormatVersion: registry.InstalledSkillRecordFormatVersion,
			ID:            "anvil-overview",
			Version:       CliVersion,
			Source:        registry.SkillSourceCore,
			Resolution:    registry.Resolution{Kind: registry.SkillResolutionKindCore, Source: "embedded"},
			InstalledAt:   ts,
			UpdatedAt:     ts,
			Targets: []registry.InstalledSkillTarget{{
				Agent: "opencode", Scope: registry.SkillScopeGlobal, Path: filepath.Join(t.TempDir(), "anvil-overview"),
			}},
		}); err != nil {
			t.Fatal(err)
		}

		_, _, stderr, err := executeCommand("skill", "update", "anvil-overview", "--agent", "auto")
		if err == nil {
			t.Fatal("update with no selectable agent: expected error")
		}
		if code := skillTestExitCode(t, err); code != output.ExitCodePrecondition {
			t.Errorf("exit code = %d, want %d (precondition)", code, output.ExitCodePrecondition)
		}
		if !strings.Contains(stderr, "--agent") {
			t.Errorf("precondition error not actionable (no --agent hint):\n%s", stderr)
		}
	})

	t.Run("repo-scope-without-project", func(t *testing.T) {
		skillTestEnv(t)
		if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode"); err != nil {
			t.Fatal(err)
		}

		// Update re-targeted to repo scope outside any Anvil project.
		work := t.TempDir()
		skillTestChdir(t, work)
		_, _, stderr, err := executeCommand("skill", "update", "anvil-overview", "--scope", "repo")
		if err == nil {
			t.Fatal("repo-scope update without a project: expected error")
		}
		if code := skillTestExitCode(t, err); code != output.ExitCodePrecondition {
			t.Errorf("exit code = %d, want %d (precondition)", code, output.ExitCodePrecondition)
		}
		if !strings.Contains(stderr, "anvil init") {
			t.Errorf("precondition error not actionable (no anvil init hint):\n%s", stderr)
		}
	})
}

// TestSkillUpdate_Core_DroppedUserTargetNotRemoved verifies the security
// LOW fix (fix-round 2): a dropped target the user replaced with their
// own content (no Anvil ownership marker) is NEVER removed by the
// target-set refresh — it is skipped with a warning, because removing it
// would delete user content.
func TestSkillUpdate_Core_DroppedUserTargetNotRemoved(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Wide install: master + claude native symlink.
	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "all"); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(os.Getenv("HOME"), ".claude", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(native, "SKILL.md")); err != nil {
		t.Fatalf("native symlink not materialized: %v", err)
	}

	// The user REPLACES the native symlink with their own real directory
	// containing their own content (the Anvil marker is gone).
	if err := os.Remove(native); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(native, "user-notes.md")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Narrow update: --agent opencode drops the claude native target.
	_, _, stderr, err := executeCommand("skill", "update", "anvil-overview", "--agent", "opencode")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("user content at the dropped target was removed — ownership must never be overridden: %v", err)
	}
	if !strings.Contains(stderr, "NOT removed") || !strings.Contains(stderr, "not an Anvil-managed install") {
		t.Errorf("dropped-target warning missing from stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "uninstall") {
		t.Errorf("dropped-target warning lacks the uninstall hint:\n%s", stderr)
	}
}

// TestSkillInstall_Core_MasterAndSymlinkLayout verifies the multi-agent
// layout: the master copy under .agents/skills and a native symlink for
// Claude Code (ADR-037 D6), and the record carries one target per
// agent/scope/path.
func TestSkillInstall_Core_MasterAndSymlinkLayout(t *testing.T) {
	skillTestEnv(t)

	_, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "claude-code,opencode")
	if err == nil {
		// Comma lists are not a supported --agent syntax; this asserts
		// the parse rejects them with an actionable error.
		t.Fatal("comma agent list accepted")
	}
	if !strings.Contains(err.Error(), "supported values") {
		t.Errorf("agent parse error not actionable: %v", err)
	}

	_, _, _, err = executeCommand("skill", "install", "anvil-overview", "--scope", "global", "--agent", "opencode")
	if err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("master not materialized: %v", err)
	}
	rec := skillTestReadSkillRecord(t, "anvil-overview")
	if len(rec.Targets) != 2 {
		t.Fatalf("record targets = %+v, want the master ('all') and the opencode target", rec.Targets)
	}
	// A reader-agent install carries the master target under the agent's
	// own ID, so the record attributes the content per agent.
	if rec.Targets[0].Agent != "all" || rec.Targets[1].Agent != "opencode" {
		t.Errorf("record targets = %+v, want [all, opencode]", rec.Targets)
	}
	if rec.Targets[0].Path != master || rec.Targets[1].Path != master {
		t.Errorf("record target paths = %+v, want both pointing at the master %s", rec.Targets, master)
	}
}
