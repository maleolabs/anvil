package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"maleolabs.com/anvil/internal/agenttarget"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// ── Model unit tests (pure Update/View — no terminal required) ──────

// keyMsg builds a tea.KeyMsg for the given key type.
func keyMsg(t tea.KeyType) tea.Msg { return tea.KeyMsg{Type: t} }

// testModelOptions builds a minimal three-stage option set.
func testModelOptions() (skills, agents, scope []skillFormOption) {
	skills = []skillFormOption{
		{ID: "anvil-overview", Label: "anvil-overview"},
		{ID: "anvil-lifecycle", Label: "anvil-lifecycle"},
	}
	agents = []skillFormOption{
		{ID: "claude-code", Label: "Claude Code"},
		{ID: "opencode", Label: "OpenCode"},
	}
	scope = scopeOptions(agenttarget.ScopeRepo)
	return skills, agents, scope
}

// TestInstallFormModel_SelectSkillsAgentsScope verifies the happy path:
// space toggles, enter submits, and the model completes with the right
// selections after all three stages.
func TestInstallFormModel_SelectSkillsAgentsScope(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)

	// Skills: select anvil-lifecycle (down + space), submit.
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeySpace))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	// Agents: select OpenCode (down + space), submit.
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeySpace))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	// Scope: move to global — the radio follows the cursor — submit.
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	fm := m.(installFormModel)
	if !fm.completed() {
		t.Fatalf("model must complete after all three stages, stage=%v", fm.stage)
	}
	names, agentsSel, scopeSel := fm.selections()
	if len(names) != 1 || names[0] != "anvil-lifecycle" {
		t.Errorf("selected skills = %v, want [anvil-lifecycle]", names)
	}
	if len(agentsSel) != 1 || agentsSel[0].ID != "opencode" {
		t.Errorf("selected agents = %v, want [opencode]", agentsSel)
	}
	if scopeSel != agenttarget.ScopeGlobal {
		t.Errorf("scope = %v, want global", scopeSel)
	}
	if fm.aborted != abortNone {
		t.Errorf("aborted = %v, want none", fm.aborted)
	}
}

// TestInstallFormModel_EmptySelectionBlocked verifies that submitting a
// stage with no selection is blocked with an inline message — the form
// never resolves an empty selection silently.
func TestInstallFormModel_EmptySelectionBlocked(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)

	m, _ = m.Update(keyMsg(tea.KeyEnter))
	fm := m.(installFormModel)
	if fm.stage != stageSkills {
		t.Fatalf("empty skills submit must not advance the stage, stage=%v", fm.stage)
	}
	if !strings.Contains(fm.message, "select at least one skill") {
		t.Errorf("message = %q, want a select-at-least-one hint", fm.message)
	}

	// Select a skill, advance, then submit agents with nothing selected.
	m, _ = m.Update(keyMsg(tea.KeySpace))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	fm = m.(installFormModel)
	if fm.stage != stageAgents {
		t.Fatalf("empty agents submit must not advance the stage, stage=%v", fm.stage)
	}
	if !strings.Contains(fm.message, "select at least one agent") {
		t.Errorf("message = %q, want a select-at-least-one hint", fm.message)
	}
}

// TestInstallFormModel_CtrlCAborts verifies Ctrl-C quits the model with
// the cancelled abort marker (terminal restore is bubbletea's job).
func TestInstallFormModel_CtrlCAborts(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)
	m, cmd := m.Update(keyMsg(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("Ctrl-C must return a quit command")
	}
	fm := m.(installFormModel)
	if fm.aborted != abortCtrlC {
		t.Errorf("aborted = %v, want abortCtrlC", fm.aborted)
	}
}

// TestInstallFormModel_EscapeAborts verifies Esc quits the model with
// the escape abort marker.
func TestInstallFormModel_EscapeAborts(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)
	m, cmd := m.Update(keyMsg(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a quit command")
	}
	fm := m.(installFormModel)
	if fm.aborted != abortEscape {
		t.Errorf("aborted = %v, want abortEscape", fm.aborted)
	}
}

// TestInstallFormModel_NavigationWraps verifies cursor wrap-around at
// the top and bottom of a stage.
func TestInstallFormModel_NavigationWraps(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)

	m, _ = m.Update(keyMsg(tea.KeyUp)) // from 0 wraps to last
	if fm := m.(installFormModel); fm.cursor != len(skills)-1 {
		t.Errorf("cursor after up = %d, want %d", fm.cursor, len(skills)-1)
	}
	m, _ = m.Update(keyMsg(tea.KeyDown)) // wraps back to 0
	if fm := m.(installFormModel); fm.cursor != 0 {
		t.Errorf("cursor after down = %d, want 0", fm.cursor)
	}
}

// TestInstallFormModel_ScopeRadioSingleSelect verifies the scope stage
// is a single-select radio that FOLLOWS the cursor: moving the cursor
// (arrow up/down) selects the row — no space toggle needed — and only
// one option is ever selected.
func TestInstallFormModel_ScopeRadioSingleSelect(t *testing.T) {
	skills, agents, scope := testModelOptions()
	var m tea.Model = newInstallFormModel(skills, agents, scope)
	// Advance to the scope stage.
	m, _ = m.Update(keyMsg(tea.KeySpace))
	m, _ = m.Update(keyMsg(tea.KeyEnter))
	m, _ = m.Update(keyMsg(tea.KeySpace))
	m, _ = m.Update(keyMsg(tea.KeyEnter))

	// Arrow down: the selection follows the cursor (no space needed).
	m, _ = m.Update(keyMsg(tea.KeyDown))
	fm := m.(installFormModel)
	if fm.scope != agenttarget.ScopeGlobal {
		t.Errorf("scope = %v, want global after arrow down", fm.scope)
	}
	if !fm.options[stageScope][1].Selected || fm.options[stageScope][0].Selected {
		t.Errorf("radio must select exactly one option: %+v", fm.options[stageScope])
	}

	// Arrow up wraps back to repo and re-selects it.
	m, _ = m.Update(keyMsg(tea.KeyUp))
	fm = m.(installFormModel)
	if fm.scope != agenttarget.ScopeRepo {
		t.Errorf("scope = %v, want repo after arrow up", fm.scope)
	}
	if !fm.options[stageScope][0].Selected || fm.options[stageScope][1].Selected {
		t.Errorf("radio must follow the cursor: %+v", fm.options[stageScope])
	}

	// Space on the scope stage still selects the cursor row (harmless).
	m, _ = m.Update(keyMsg(tea.KeyDown))
	m, _ = m.Update(keyMsg(tea.KeySpace))
	fm = m.(installFormModel)
	if fm.scope != agenttarget.ScopeGlobal {
		t.Errorf("scope = %v, want global after down + space", fm.scope)
	}
}

// TestInstallFormModel_AlreadyInstalledNote verifies the option builder
// annotates installed skills (regression: note distribution).
func TestInstallFormModel_AlreadyInstalledNote(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Install one core skill so its record exists.
	_, _, stderr, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global")
	if err != nil {
		t.Fatalf("seed install failed: %v (stderr: %q)", err, stderr)
	}

	names, _, err := discoverBatchSkills()
	if err != nil {
		t.Fatal(err)
	}
	opts := interactiveSkillOptions(names)
	var withNote, withoutNote int
	for _, o := range opts {
		if o.ID == "anvil-overview" {
			if !strings.Contains(o.Note, "already installed") {
				t.Errorf("anvil-overview note = %q, want an already-installed note", o.Note)
			}
			withNote++
		} else if strings.Contains(o.Note, "already installed") {
			t.Errorf("%s must not carry an installed note (not installed), got %q", o.ID, o.Note)
		} else {
			withoutNote++
		}
	}
	if withNote != 1 || withoutNote != len(names)-1 {
		t.Errorf("note distribution wrong: with=%d without=%d (total %d)", withNote, withoutNote, len(names))
	}
}

// TestInstallFormModel_FrameLinesFitNarrowTerminal guards the frame
// layout against wrapped rows: every rendered View line must fit a
// 60-column terminal (regression: long scope notes and already-installed
// notes pushed rows past the width).
func TestInstallFormModel_FrameLinesFitNarrowTerminal(t *testing.T) {
	skillTestEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := executeCommand("skill", "install", "anvil-overview", "--scope", "global"); err != nil {
		t.Fatalf("seed install failed: %v", err)
	}

	names, _, err := discoverBatchSkills()
	if err != nil {
		t.Fatal(err)
	}
	m := newInstallFormModel(
		interactiveSkillOptions(names), interactiveAgentOptions(), scopeOptions(agenttarget.ScopeRepo),
	)
	// The View carries ANSI styles (lipgloss); measure the VISIBLE width
	// of each line (the frame glyphs ◉ ○ ▸ ◆ ─ are single-width).
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, stage := range []skillFormStage{stageSkills, stageAgents, stageScope} {
		m.stage = stage
		for i, line := range strings.Split(m.View(), "\n") {
			visible := ansi.ReplaceAllString(line, "")
			if utf8.RuneCountInString(visible) > 60 {
				t.Errorf("stage %v line %d exceeds 60 columns (%d): %q", stage, i+1, utf8.RuneCountInString(visible), visible)
			}
		}
	}
}

// TestInstallFormModel_AgentDefaultsDetected verifies the agent option
// builder pre-selects auto-detected agents (the --agent default) and
// that no detection leaves every agent unselected.
func TestInstallFormModel_AgentDefaultsDetected(t *testing.T) {
	skillTestEnv(t)
	// No agent config folders: nothing detected, nothing pre-selected.
	opts := interactiveAgentOptions()
	if len(opts) == 0 {
		t.Fatal("selectable set must be non-empty")
	}
	for _, o := range opts {
		if o.Selected {
			t.Errorf("agent %s pre-selected without detection", o.ID)
		}
	}
	if len(opts) != len(agenttarget.SelectableAgents()) {
		t.Errorf("option count = %d, want %d (Roo excluded)", len(opts), len(agenttarget.SelectableAgents()))
	}

	// A .claude folder marks claude-code as detected.
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts = interactiveAgentOptions()
	claudeSeen := false
	for _, o := range opts {
		if o.ID == "claude-code" && o.Selected {
			claudeSeen = true
		}
		if o.ID != "claude-code" && o.Selected {
			t.Errorf("agent %s must not be pre-selected (only claude-code detected)", o.ID)
		}
	}
	if !claudeSeen {
		t.Error("claude-code must be pre-selected when ~/.claude exists")
	}
}

// ── Program-level tests (bubbletea key parsing from a byte stream) ───

// runFormProgram runs a bubbletea program against a scripted input
// stream, exercising the real key decoder (space, enter, ANSI arrow
// sequences, ctrl-c, bare esc) without a PTY.
func runFormProgram(t *testing.T, m tea.Model, input string) (tea.Model, string, error) {
	t.Helper()
	var buf bytes.Buffer
	p := tea.NewProgram(m,
		tea.WithInput(strings.NewReader(input)),
		tea.WithOutput(&buf),
		tea.WithoutSignalHandler(),
	)
	result, err := p.Run()
	return result, buf.String(), err
}

// TestSkillInstallProgram_BufferInput_Completes drives the full flow
// through bubbletea's key decoder and verifies the selections.
func TestSkillInstallProgram_BufferInput_Completes(t *testing.T) {
	skills, agents, scope := testModelOptions()
	// space (select first skill) enter; space (select first agent) enter;
	// ESC [ B (down to global — the scope radio follows the cursor) enter.
	input := " \r \r\x1b[B\r"
	model, out, err := runFormProgram(t, newInstallFormModel(skills, agents, scope), input)
	if err != nil {
		t.Fatalf("program run failed: %v", err)
	}
	fm := model.(installFormModel)
	if !fm.completed() {
		t.Fatalf("program must complete, stage=%v (aborted=%v)", fm.stage, fm.aborted)
	}
	names, agentsSel, scopeSel := fm.selections()
	if len(names) != 1 || names[0] != "anvil-overview" {
		t.Errorf("selected skills = %v, want [anvil-overview]", names)
	}
	if len(agentsSel) != 1 || agentsSel[0].ID != "claude-code" {
		t.Errorf("selected agents = %v, want [claude-code]", agentsSel)
	}
	if scopeSel != agenttarget.ScopeGlobal {
		t.Errorf("scope = %v, want global", scopeSel)
	}
	// The rendered frame content is asserted on the model's View()
	// directly (TestInstallFormModel_*); the buffer here only proves the
	// program ran without errors (bubbletea's renderer flushes on its own
	// framerate ticker, so a fast run may not flush before Run returns).
	_ = out
}

// TestSkillInstallProgram_BufferInput_CtrlCAborts verifies ctrl-c
// (0x03) quits the program with the cancelled marker.
func TestSkillInstallProgram_BufferInput_CtrlCAborts(t *testing.T) {
	skills, agents, scope := testModelOptions()
	model, _, err := runFormProgram(t, newInstallFormModel(skills, agents, scope), "\x03")
	if err != nil {
		t.Fatalf("program run failed: %v", err)
	}
	fm := model.(installFormModel)
	if fm.aborted != abortCtrlC {
		t.Errorf("aborted = %v, want abortCtrlC", fm.aborted)
	}
	if fm.completed() {
		t.Error("aborted program must not be completed")
	}
}

// TestSkillInstallProgram_BufferInput_EscAborts verifies a bare escape
// quits the program with the escape marker.
func TestSkillInstallProgram_BufferInput_EscAborts(t *testing.T) {
	skills, agents, scope := testModelOptions()
	model, _, err := runFormProgram(t, newInstallFormModel(skills, agents, scope), "\x1b")
	if err != nil {
		t.Fatalf("program run failed: %v", err)
	}
	fm := model.(installFormModel)
	if fm.aborted != abortEscape {
		t.Errorf("aborted = %v, want abortEscape", fm.aborted)
	}
}

// ── TTY gate + fallback ─────────────────────────────────────────────

// skillTestNonTTYStdin pins rootCmd's stdin to a non-terminal reader for
// the duration of the test. This prevents the no-args interactive gate
// from reaching os.Stdin: when a developer runs `go test ./cmd` from an
// interactive terminal, os.Stdin IS a TTY and the form would open — and
// block forever (M2).
func skillTestNonTTYStdin(t *testing.T) {
	t.Helper()
	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(bytes.NewReader(nil))
	t.Cleanup(func() { rootCmd.SetIn(oldIn) })
}

// TestStdinIsTerminal_PipeAndBufferAreNotTerminal verifies the TTY gate:
// a pipe fd and a buffer are never treated as a terminal, so the form
// cannot open on non-TTY stdin.
func TestStdinIsTerminal_PipeAndBufferAreNotTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	cmd := &cobra.Command{}
	cmd.SetIn(r)
	if stdinIsTerminal(cmd) {
		t.Error("a pipe fd must not be detected as a terminal")
	}

	cmd.SetIn(bytes.NewBufferString(""))
	if stdinIsTerminal(cmd) {
		t.Error("a buffer must not be detected as a terminal")
	}

	cmd.SetIn(nil)
	// InOrStdin falls back to os.Stdin. In the test process this is
	// normally a pipe/file — never a terminal — but a developer running
	// `go test` from an interactive terminal has a TTY stdin, so the
	// assertion is skipped in that environment (M2).
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("os.Stdin is a terminal in this environment; the non-TTY fallback assertion does not apply")
	}
	if stdinIsTerminal(cmd) {
		t.Error("os.Stdin in the test process must not be detected as a terminal")
	}
}

// TestSkillInstall_NoArgs_NonTTY_ActionableError verifies exit criterion
// 2: `anvil skill install` without a name on non-TTY stdin fails with an
// actionable error pointing at install <name> / --all / flags — and never
// renders the form.
func TestSkillInstall_NoArgs_NonTTY_ActionableError(t *testing.T) {
	skillTestEnv(t)
	skillTestNonTTYStdin(t)
	_, stdout, stderr, err := executeCommand("skill", "install")
	if err == nil {
		t.Fatal("no-args install on non-TTY stdin must fail")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d (general)", code, output.ExitCodeGeneral)
	}
	for _, want := range []string{
		"requires an interactive terminal",
		"anvil skill install <name>",
		"anvil skill install --all",
		"--agent/--scope/--force",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr must point at %q, got:\n%s", want, stderr)
		}
	}
	if strings.Contains(stdout, "stage 1 of 3") {
		t.Errorf("the form must never render on non-TTY stdin:\n%s", stdout)
	}
}

// TestSkillInstall_NoArgs_JSON_NeverInteractive verifies `--json` never
// opens the form: the no-args + --json combination reports the
// automation-facing JSON error envelope on stdout.
func TestSkillInstall_NoArgs_JSON_NeverInteractive(t *testing.T) {
	skillTestEnv(t)
	skillTestNonTTYStdin(t)
	_, stdout, _, err := executeCommand("skill", "install", "--json")
	if err == nil {
		t.Fatal("no-args install with --json must fail (the form never opens with --json)")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodeGeneral {
		t.Errorf("exit code = %d, want %d", code, output.ExitCodeGeneral)
	}
	var envelope struct {
		Version string `json:"version"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a JSON error envelope (the form leaked?): %v\nstdout:\n%s", jerr, stdout)
	}
	if envelope.Status != "error" || envelope.Error == "" {
		t.Errorf("envelope = %+v, want status error with a message", envelope)
	}
	if strings.Contains(stdout, "stage 1 of 3") {
		t.Errorf("the form must never render with --json:\n%s", stdout)
	}
}

// ── Selection → install mapping ─────────────────────────────────────

// TestSkillInstall_Interactive_SelectionToInstall verifies exit criteria
// 3 + 6: the form's selections drive the SAME gated batch path (no
// bypass) — scripted keys select a core skill + a standard-declared
// skill + an agent + the global scope, and runBatchSkillInstall installs
// both through the pipeline with records and the batch report.
func TestSkillInstall_Interactive_SelectionToInstall(t *testing.T) {
	const (
		stdID      = "anvil-standard-laravel"
		stdVersion = "1.2.3"
		skillName  = "overview"
		skillVer   = "1.0.0"
		assetID    = "anvil-skill-overview-1-0-0"
	)
	bundle := skillTestBundle(t, skillName, skillVer, stdID)
	server := skillTestStandardServer(t, assetID, bundle)
	skillTestEnv(t)
	installTestEnv(t, server)
	md, _ := skillTestStandardFixture(t, stdID, stdVersion, registry.LifecycleStatePublished,
		skillName, skillVer, assetID, bundle, server.URL)

	// The full discovery set: three core skills + the declared standard
	// skill (sorted).
	names, corrupt, err := discoverBatchSkills()
	if err != nil {
		t.Fatalf("discoverBatchSkills: %v", err)
	}
	if corrupt != 0 {
		t.Fatalf("corrupt standard records = %d, want 0", corrupt)
	}
	if len(names) != 4 {
		t.Fatalf("discovered = %v, want the 3 core skills + %s", names, skillName)
	}

	// Script: select anvil-overview (index 2) + overview (index 3) in
	// skills; select opencode in agents; select global scope (the radio
	// follows the cursor — no space needed on the scope stage).
	skills := interactiveSkillOptions(names)
	agents := interactiveAgentOptions()
	model, _, err := runFormProgram(t, newInstallFormModel(skills, agents, scopeOptions(agenttarget.ScopeRepo)),
		"\x1b[B\x1b[B \x1b[B \r"+"\x1b[B \r"+"\x1b[B\r")
	if err != nil {
		t.Fatalf("form program failed: %v", err)
	}
	fm := model.(installFormModel)
	if !fm.completed() {
		t.Fatalf("form did not complete: stage=%v aborted=%v", fm.stage, fm.aborted)
	}
	selNames, selAgents, selScope := fm.selections()
	if len(selNames) != 2 || len(selAgents) != 1 || selScope != agenttarget.ScopeGlobal {
		t.Fatalf("unexpected selections: names=%v agents=%v scope=%v", selNames, selAgents, selScope)
	}

	// Drive the same runner the interactive wrapper and --all use. The
	// standard-skill gate needs the fixture's index + trust anchors — set
	// them the same way the flag path would. The runner clears the
	// subcommand's writer (SetOut(nil)) so stdout falls back to the
	// parent chain — point rootCmd's writer at the same buffer.
	var out, errOut bytes.Buffer
	oldRootOut, oldRootErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	cmd := skillInstallCmdForTest(t)
	resetFlags(cmd) // clear stale flag state (e.g. --json) from earlier tests
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader("")) // no further reads expected
	t.Cleanup(func() {
		// Restore the shared command-tree writers and stdin: cobra keeps
		// subcommand writers across tests (the command is a singleton),
		// so a dead buffer left here would swallow later tests' stderr.
		rootCmd.SetOut(oldRootOut)
		rootCmd.SetErr(oldRootErr)
		cmd.SetOut(nil)
		cmd.SetErr(nil)
		cmd.SetIn(nil)
	})
	if err := cmd.Flags().Set("index", skillTestIndexDir(t, md)); err != nil {
		t.Fatalf("set index flag: %v", err)
	}
	if err := cmd.Flags().Set("trust-anchors", skillTestAnchorsFile(t, md)); err != nil {
		t.Fatalf("set trust-anchors flag: %v", err)
	}
	err = runBatchSkillInstall(cmd, selNames, selAgents, selScope, false)
	if err != nil {
		t.Fatalf("runBatchSkillInstall failed: %v (stderr: %q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "Batch install complete") {
		t.Errorf("batch report missing:\n%s", out.String())
	}

	// Both skills recorded with the right sources.
	coreRec := skillTestReadSkillRecord(t, "anvil-overview")
	if coreRec.Source != registry.SkillSourceCore {
		t.Errorf("core record source = %q, want %q", coreRec.Source, registry.SkillSourceCore)
	}
	stdRec := skillTestReadSkillRecord(t, skillName)
	if stdRec.Source != stdID {
		t.Errorf("standard record source = %q, want %q", stdRec.Source, stdID)
	}
	if stdRec.Version != skillVer {
		t.Errorf("standard record version = %q, want %q", stdRec.Version, skillVer)
	}
}

// skillInstallCmdForTest returns the registered skill install command
// with fresh writers (the mapping test drives runBatchSkillInstall
// directly).
func skillInstallCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	cmd, _, err := rootCmd.Find([]string{"skill", "install"})
	if err != nil {
		t.Fatalf("find skill install command: %v", err)
	}
	return cmd
}

// ── Help text ───────────────────────────────────────────────────────

// TestSkillInstall_HelpDocumentsInteractive verifies exit criterion 7:
// the install help documents the interactive mode.
func TestSkillInstall_HelpDocumentsInteractive(t *testing.T) {
	_, helpOut, _, err := executeCommand("skill", "install", "--help")
	if err != nil {
		t.Fatalf("skill install --help failed: %v", err)
	}
	for _, want := range []string{
		"Interactive mode",
		"space",
		"Ctrl-C",
		"--force does not apply to the form",
	} {
		if !strings.Contains(helpOut, want) {
			t.Errorf("help must document %q, got:\n%s", want, helpOut)
		}
	}
}

// TestResolveInteractiveFlagPresets verifies the explicit-flag seeding
// of the form: invalid explicit values fail fast (the same way the
// non-interactive path reports them), valid values pre-select the rows,
// and empty/"auto" keep the auto-detected defaults.
func TestResolveInteractiveFlagPresets(t *testing.T) {
	skillTestEnv(t)
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "test"}
		c.Flags().String("agent", "", "")
		c.Flags().String("scope", "", "")
		return c
	}

	// Empty flags → detected defaults (HOME is empty in the test env, so
	// nothing is pre-selected) and the repo scope default.
	c := newCmd()
	agents, scope, err := resolveInteractiveFlagPresets(c)
	if err != nil {
		t.Fatalf("empty flags must not fail: %v", err)
	}
	if scope != agenttarget.ScopeRepo {
		t.Errorf("default scope = %q, want repo", scope)
	}
	for _, a := range agents {
		if a.Selected {
			t.Errorf("agent %s must not be pre-selected without detection", a.ID)
		}
	}

	// "auto" is the same as empty: no explicit preseed.
	c = newCmd()
	_ = c.Flags().Set("agent", "auto")
	agents, _, err = resolveInteractiveFlagPresets(c)
	if err != nil {
		t.Fatalf("--agent auto must not fail: %v", err)
	}
	for _, a := range agents {
		if a.Selected {
			t.Errorf("agent %s must not be pre-selected with --agent auto", a.ID)
		}
	}

	// An explicit invalid --agent fails fast (no silent fallback).
	c = newCmd()
	_ = c.Flags().Set("agent", "bogus-agent")
	if _, _, err := resolveInteractiveFlagPresets(c); err == nil {
		t.Error("invalid --agent must fail fast")
	}

	// An explicit invalid --scope fails fast.
	c = newCmd()
	_ = c.Flags().Set("scope", "bogus-scope")
	if _, _, err := resolveInteractiveFlagPresets(c); err == nil {
		t.Error("invalid --scope must fail fast")
	}

	// A valid explicit --agent pre-selects exactly that agent.
	c = newCmd()
	_ = c.Flags().Set("agent", "opencode")
	agents, scope, err = resolveInteractiveFlagPresets(c)
	if err != nil {
		t.Fatalf("valid --agent must not fail: %v", err)
	}
	if scope != agenttarget.ScopeRepo {
		t.Errorf("scope = %q, want repo (unchanged by --agent)", scope)
	}
	opencodeSeen := false
	for _, a := range agents {
		if a.ID == "opencode" && !a.Selected {
			t.Errorf("opencode must be pre-selected by --agent opencode")
		}
		if a.ID != "opencode" && a.Selected {
			t.Errorf("agent %s must not be pre-selected by --agent opencode", a.ID)
		}
		if a.ID == "opencode" {
			opencodeSeen = true
		}
	}
	if !opencodeSeen {
		t.Error("selectable set must contain opencode")
	}

	// A valid explicit --scope sets the default.
	c = newCmd()
	_ = c.Flags().Set("scope", "global")
	_, scope, err = resolveInteractiveFlagPresets(c)
	if err != nil {
		t.Fatalf("valid --scope must not fail: %v", err)
	}
	if scope != agenttarget.ScopeGlobal {
		t.Errorf("scope = %q, want global", scope)
	}
}

// ── Exit-code precedence (m4) ───────────────────────────────────────

// TestSkillInstall_Batch_RepoScopePreconditionBeforeDiscovery verifies
// the --all exit-code precedence: a repo-scope precondition (no Anvil
// project / no git root) is diagnosed as exit 4 BEFORE skill discovery,
// so the scope problem is never masked by a discovery outcome (m4). The
// core set is never empty in this build, so the "no skills available"
// exit-3 path cannot be triggered here; the regression guard asserts the
// scope precondition is the reported failure.
func TestSkillInstall_Batch_RepoScopePreconditionBeforeDiscovery(t *testing.T) {
	skillTestEnv(t)
	// A detected agent so the --agent auto-detect gate passes and the
	// scope gate is the one under test.
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Work from a directory that is NOT an Anvil project.
	dir := t.TempDir()
	skillTestChdir(t, dir)

	_, _, stderr, err := executeCommand("skill", "install", "--all")
	if err == nil {
		t.Fatal("--all with default repo scope outside a project must fail")
	}
	if code := skillTestExitCode(t, err); code != output.ExitCodePrecondition {
		t.Errorf("exit code = %d, want %d — the repo-scope precondition must be diagnosed as a precondition",
			code, output.ExitCodePrecondition)
	}
	if !strings.Contains(stderr, "--scope repo requires") {
		t.Errorf("stderr must carry the repo-scope precondition message, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "no skills available") {
		t.Errorf("the scope precondition must be reported before any discovery outcome, got:\n%s", stderr)
	}
}
