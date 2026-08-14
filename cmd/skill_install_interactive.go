// Package cmd implements the Anvil CLI commands.
//
// ── Skill Install — Interactive (ST-021-07) ────────────────────────
//
// `anvil skill install` with no name argument opens an interactive
// three-stage terminal form when stdin is a TTY: skills (checkbox
// multi-select) → agents (checkbox multi-select) → scope (single
// select). Space toggles the row under the cursor, Enter submits the
// stage, arrows move, Ctrl-C aborts cleanly (terminal restored), Esc
// aborts and falls back to the non-interactive hint. The scope stage is
// a single-select radio that follows the cursor: arrow up/down moves
// AND selects, Enter confirms — no space toggle needed.
//
// The form is built on github.com/charmbracelet/bubbletea, the
// battle-tested Go TUI framework: it owns raw-mode setup and restore,
// key decoding (including ESC lookahead for arrow keys), signal
// handling, and in-place frame repainting with correct line endings.
// The earlier hand-rolled renderer had to re-implement all of that —
// including a raw-mode newline bug that left every frame row starting
// at the previous row's end column — which is exactly the class of
// terminal problem the framework solves. `--json` is the automation
// surface and NEVER opens the form.
//
// Trust boundary: the form only RESOLVES the selection
// (names, agents, scope). The installs run through the same gated batch
// path as `--all` (runBatchSkillInstall → installOneSkill → the core /
// adoption pipeline) — the form never bypasses trust verification
// (ADR-037 D4).
//
// Reference: ST-021-07, ST-021-06, ADR-037 D4/D5
package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"maleolabs.com/anvil/internal/agenttarget"
	"maleolabs.com/anvil/internal/output"
)

// ── Theme ───────────────────────────────────────────────────────────
//
// The form uses a small, consistent palette (256-color ANSI, readable on
// dark terminals): a vivid primary for the selected state and accents,
// dim gray for unselected rows and notes, and muted gray for chrome
// (header meta, footer hints). lipgloss strips the colors automatically
// when the output is not a color-capable terminal.
const (
	themePrimary = "135" // vivid violet — selected rows, accents
	themeDim     = "240" // dim gray — unselected glyphs, labels and notes
	themeMuted   = "245" // muted gray — header meta, footer hints
	themeError   = "203" // soft red — inline validation messages
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleStage    = lipgloss.NewStyle().Foreground(lipgloss.Color(themePrimary)).Bold(true)
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color(themeDim))
	styleMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color(themeMuted))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color(themePrimary)).Bold(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color(themeError))
	styleSep      = lipgloss.NewStyle().Foreground(lipgloss.Color(themeDim))
)

// ── TTY gate ────────────────────────────────────────────────────────

// stdinIsTerminal reports whether the command's stdin is an interactive
// terminal (term.IsTerminal on the stdin fd). A buffer, pipe, or
// redirected file is never a terminal, so the interactive form only ever
// opens on a real TTY.
func stdinIsTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// skillInteractiveUnavailableError is the actionable error for every
// path that cannot run the form: non-TTY / piped stdin, and --json (the
// automation surface, which never opens the form). It points at the
// non-interactive alternatives.
func skillInteractiveUnavailableError(cmd *cobra.Command) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	message := "interactive install requires an interactive terminal"
	if jsonOutput {
		message = "skill install with no name requires a terminal for the interactive form"
	}
	return skillReportError(cmd,
		message,
		"the interactive form opens only on a terminal; --json is reserved for automation and never opens the form",
		"Run 'anvil skill install <name>' to install a specific skill, 'anvil skill install --all' to install every skill, or pass --agent/--scope/--force flags",
		output.ExitCodeGeneral, nil)
}

// skillInteractiveAbortError reports an abort AFTER the form was running
// (Esc or a closed stdin): the user was on a terminal, so the message
// states the abort, and the resolution points at the non-interactive
// alternatives.
func skillInteractiveAbortError(cmd *cobra.Command) error {
	return skillReportError(cmd,
		"interactive install aborted",
		"no skills were installed",
		"Run 'anvil skill install <name>' to install a specific skill, 'anvil skill install --all' to install every skill, or pass --agent/--scope/--force flags",
		output.ExitCodeGeneral, nil)
}

// ── Form model ──────────────────────────────────────────────────────

// skillFormOption is one selectable row of the form.
type skillFormOption struct {
	ID       string // stable identity: skill name, agent id, or scope value
	Label    string // rendered label (agent display name / scope word)
	Selected bool
	Note     string // advisory suffix, e.g. "already installed 1.2.3"
}

// skillFormStage enumerates the three form stages.
type skillFormStage int

const (
	stageSkills skillFormStage = iota
	stageAgents
	stageScope
	stageDone
)

// stageNames renders the header of each stage.
var stageNames = map[skillFormStage]string{
	stageSkills: "Select skills",
	stageAgents: "Select agents",
	stageScope:  "Select scope",
}

// stageHints renders the footer key hints of each stage.
var stageHints = map[skillFormStage]string{
	stageSkills: "↑/↓ move · space select · enter continue · esc abort",
	stageAgents: "↑/↓ move · space select · enter continue · esc abort",
	stageScope:  "↑/↓ move selects · enter confirms · esc abort",
}

// skillFormAbort records how the form ended before all stages were
// submitted.
type skillFormAbort int

const (
	abortNone skillFormAbort = iota
	abortCtrlC
	abortEscape
)

// installFormModel is the bubbletea model of the three-stage checkbox
// form. It is a pure state machine: Update applies keys, View renders
// the current frame. The installs themselves never run inside the model
// — on completion the interactive wrapper reads selections() and drives
// runBatchSkillInstall (the same gated path as --all).
type installFormModel struct {
	stage   skillFormStage
	cursor  int
	options [][]skillFormOption // options per stage (skills, agents, scope)
	message string              // inline status/error line rendered above the footer
	scope   agenttarget.Scope
	aborted skillFormAbort
}

// newInstallFormModel builds the model with the given options. skills
// and agents are multi-select checkboxes; scope is a single-select radio
// with the given default (repo per ADR-037).
func newInstallFormModel(skills, agents, scopeOpts []skillFormOption) installFormModel {
	defaultScope := agenttarget.ScopeRepo
	for _, o := range scopeOpts {
		if o.Selected {
			defaultScope = agenttarget.Scope(o.ID)
		}
	}
	return installFormModel{
		stage:   stageSkills,
		options: [][]skillFormOption{skills, agents, scopeOpts},
		scope:   defaultScope,
	}
}

// Init implements tea.Model.
func (m installFormModel) Init() tea.Cmd { return nil }

// Update implements tea.Model: it applies one key to the state machine.
// Keys that arrive after the final stage submitted (queued input that
// bubbletea delivers before processing the quit command) are ignored.
func (m installFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.stage == stageDone {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyUp:
		m.moveCursor(-1)
	case tea.KeyDown:
		m.moveCursor(+1)
	case tea.KeySpace:
		m.toggle()
	case tea.KeyEnter:
		if m.submit() {
			return m, tea.Quit
		}
	case tea.KeyCtrlC:
		m.aborted = abortCtrlC
		return m, tea.Quit
	case tea.KeyEsc:
		m.aborted = abortEscape
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model: it renders the current frame with the
// form's theme. The bubbletea renderer repaints frames in place with
// correct line endings; rows are kept within 60 visible columns so they
// never wrap on a narrow terminal. Colors are stripped automatically on
// non-color terminals (lipgloss).
func (m installFormModel) View() string {
	stage := m.stage
	if stage == stageDone {
		// The final frame after the last submit: nothing to render — the
		// selection summary prints after the program returns.
		return ""
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("◆ Anvil skill installer"))
	b.WriteString("  ")
	b.WriteString(styleMuted.Render(fmt.Sprintf("stage %d of 3", stageNumber(stage))))
	b.WriteString("\n")
	b.WriteString(styleStage.Render(stageNames[stage]))
	b.WriteString("\n")
	b.WriteString(styleSep.Render(strings.Repeat("─", 46)))
	b.WriteString("\n\n")

	// Align appended notes to one column when any option in the stage
	// carries a note, so the note text stays left-aligned across rows of
	// different label lengths (e.g. the scope stage notes).
	maxLabel := 0
	padNotes := false
	for _, o := range m.options[stage] {
		if o.Note != "" {
			padNotes = true
		}
		if l := len(o.Label); l > maxLabel {
			maxLabel = l
		}
	}
	for i, o := range m.options[stage] {
		label := o.Label
		if padNotes {
			label = fmt.Sprintf("%-*s", maxLabel, o.Label)
		}
		if stage == stageScope {
			// Single-select radio: the selection follows the cursor; the
			// selected row is bright, the rest are dimmed.
			if i == m.cursor {
				b.WriteString(styleSelected.Render("◉ ") + styleSelected.Render(label))
			} else {
				b.WriteString(styleDim.Render("○ ") + styleDim.Render(label))
			}
		} else {
			// Multi-select checkbox: the cursor row carries the ▸
			// marker; selected rows are bright, unselected glyphs dim.
			marker := "  "
			if i == m.cursor {
				marker = styleSelected.Render("▸ ")
			}
			glyph := styleDim.Render("○")
			if o.Selected {
				glyph = styleSelected.Render("◉")
			}
			styledLabel := label
			if o.Selected {
				styledLabel = styleSelected.Render(label)
			}
			b.WriteString(marker + glyph + " " + styledLabel)
		}
		if o.Note != "" {
			// Clip long notes so the row stays within the frame's
			// narrow-terminal budget (a wrapped row still renders
			// poorly on short terminals).
			b.WriteString(styleDim.Render("  (" + clipNote(o.Note, noteBudget(maxLabel)) + ")"))
		}
		b.WriteString("\n")
	}
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(styleError.Render(m.message))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(stageHints[stage]))
	return b.String()
}

// selections returns the resolved install inputs: the selected skill
// names (in display order), the selected agents, and the selected scope.
func (m installFormModel) selections() ([]string, []agenttarget.Agent, agenttarget.Scope) {
	var names []string
	for _, o := range m.options[stageSkills] {
		if o.Selected {
			names = append(names, o.ID)
		}
	}
	var agents []agenttarget.Agent
	for _, o := range m.options[stageAgents] {
		if o.Selected {
			if a, ok := agentByID(o.ID); ok {
				agents = append(agents, a)
			}
		}
	}
	return names, agents, m.scope
}

// completed reports whether all three stages were submitted.
func (m installFormModel) completed() bool { return m.stage == stageDone }

// agentByID looks up an agent by its canonical id in the table.
func agentByID(id string) (agenttarget.Agent, bool) {
	for _, a := range agenttarget.AllAgents() {
		if a.ID == id {
			return a, true
		}
	}
	return agenttarget.Agent{}, false
}

// moveCursor moves the cursor by delta, wrapping around the current
// stage's options. On the scope stage the selection follows the cursor
// (single-select radio: the row under the cursor is the selected row).
func (m *installFormModel) moveCursor(delta int) {
	opts := m.options[m.stage]
	if len(opts) == 0 {
		return
	}
	m.cursor = (m.cursor + delta + len(opts)) % len(opts)
	m.message = ""
	if m.stage == stageScope {
		for i := range opts {
			opts[i].Selected = i == m.cursor
		}
		m.scope = agenttarget.Scope(opts[m.cursor].ID)
	}
}

// toggle flips the selection of the row under the cursor. The scope
// stage is a single select that follows the cursor: space selects the
// row under the cursor (harmless duplicate of the arrow behavior).
func (m *installFormModel) toggle() {
	opts := m.options[m.stage]
	if m.cursor < 0 || m.cursor >= len(opts) {
		return
	}
	if m.stage == stageScope {
		for i := range opts {
			opts[i].Selected = i == m.cursor
		}
		m.scope = agenttarget.Scope(opts[m.cursor].ID)
		m.message = ""
		return
	}
	opts[m.cursor].Selected = !opts[m.cursor].Selected
	m.message = ""
}

// submit validates the current stage and advances. It reports true when
// the form is complete (the caller quits the program), false otherwise
// (an inline message explains why the stage cannot be submitted yet).
func (m *installFormModel) submit() bool {
	switch m.stage {
	case stageSkills:
		if !m.hasSelection(stageSkills) {
			m.message = "select at least one skill (space toggles a checkbox)"
			return false
		}
	case stageAgents:
		if !m.hasSelection(stageAgents) {
			m.message = "select at least one agent (space toggles a checkbox)"
			return false
		}
	case stageScope:
		// Scope is a radio: the submitted value is whatever is selected.
	case stageDone:
		return true
	}
	m.stage++
	m.cursor = 0
	m.message = ""
	return m.stage == stageDone
}

// hasSelection reports whether any option of the stage is selected.
func (m *installFormModel) hasSelection(stage skillFormStage) bool {
	for _, o := range m.options[stage] {
		if o.Selected {
			return true
		}
	}
	return false
}

// noteBudget returns how many characters an option note may use so the
// full row stays within 60 columns: marker (2) + checkbox (3) + space
// (1) + padded label (maxLabel) + "  (" + note + ")" (4). Widths are
// measured in runes — the frame glyphs (·, —, …) are single-width.
func noteBudget(maxLabel int) int {
	budget := 60 - (2 + 3 + 1 + maxLabel + 4)
	if budget < 8 {
		return 8
	}
	return budget
}

// clipNote truncates a long note to max characters (with an ellipsis)
// so frame rows never wrap on a narrow terminal. Truncation is rune-
// based so multibyte glyphs do not overrun the budget.
func clipNote(note string, max int) string {
	if utf8.RuneCountInString(note) <= max {
		return note
	}
	if max <= 1 {
		return string([]rune(note)[:1])
	}
	return string([]rune(note)[:max-1]) + "…"
}

// stageNumber renders the 1-based stage number.
func stageNumber(s skillFormStage) int { return int(s) + 1 }

// renderInteractiveSelection prints the selection summary after the
// form completes (the terminal is restored by bubbletea before Run
// returns, so plain line endings are correct here).
func renderInteractiveSelection(cmd *cobra.Command, names []string, agents []agenttarget.Agent, scope agenttarget.Scope) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s — selection confirmed\n", "Anvil skill installer")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Installing %d skill(s) for %d agent(s) at %s scope:\n", len(names), len(agents), scope)
	for _, n := range names {
		fmt.Fprintf(w, "  • %s\n", n)
	}
	for _, a := range agents {
		fmt.Fprintf(w, "  → %s (%s)\n", a.DisplayName, a.ID)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Running the gated install pipeline (same as --all)...")
}

// ── Option builders ─────────────────────────────────────────────────

// scopeOptions renders the repo/global radio options with the given
// default selected.
func scopeOptions(defaultScope agenttarget.Scope) []skillFormOption {
	return []skillFormOption{
		{ID: string(agenttarget.ScopeRepo), Label: "repo", Selected: defaultScope == agenttarget.ScopeRepo,
			Note: "project git root — requires an Anvil project"},
		{ID: string(agenttarget.ScopeGlobal), Label: "global", Selected: defaultScope == agenttarget.ScopeGlobal,
			Note: "home agent directories — no project required"},
	}
}

// interactiveSkillOptions builds the skill checkboxes from the
// discovered names with an "already installed" note when the
// installed-skills store reports a record (best-effort: any store error
// degrades to no note).
func interactiveSkillOptions(names []string) []skillFormOption {
	opts := make([]skillFormOption, 0, len(names))
	store, storeErr := skillStore()
	for _, name := range names {
		note := ""
		if storeErr == nil {
			if rec, rerr := store.Get(name); rerr == nil {
				note = "already installed " + rec.Version
			}
		}
		opts = append(opts, skillFormOption{ID: name, Label: name, Note: note})
	}
	return opts
}

// interactiveAgentOptions builds the agent checkboxes from the
// selectable set (TS-021-02 — SelectableAgents, Roo excluded). The
// auto-detected agents are pre-selected, matching the --agent default
// (ADR-037 D5); with no detection every agent starts unselected and the
// user picks explicitly.
func interactiveAgentOptions() []skillFormOption {
	selectable := agenttarget.SelectableAgents()
	detected := agenttarget.DetectAgentsSelectable(homeDirOrEmpty())
	opts := make([]skillFormOption, 0, len(selectable))
	for _, a := range selectable {
		selected := false
		for _, d := range detected {
			if d.ID == a.ID {
				selected = true
				break
			}
		}
		opts = append(opts, skillFormOption{ID: a.ID, Label: a.DisplayName, Selected: selected})
	}
	return opts
}

// homeDirOrEmpty returns the user home directory; on failure it returns
// "" so agent auto-detection degrades to an empty detected set (the
// caller then relies on explicit selection).
func homeDirOrEmpty() string {
	home, _ := os.UserHomeDir()
	return home
}

// resolveInteractiveFlagPresets seeds the form's agent options and
// default scope from explicit --agent / --scope values. An explicit but
// INVALID value fails fast (m3/T1) with the same error category as the
// non-interactive path (skillReportAgentError for --agent,
// "invalid --scope" for --scope); an empty or "auto" --agent keeps the
// auto-detected default selection. It is a separate function so the
// fail-fast rule is unit-testable without a TTY.
func resolveInteractiveFlagPresets(cmd *cobra.Command) (agents []skillFormOption, defaultScope agenttarget.Scope, err error) {
	agents = interactiveAgentOptions()
	defaultScope = agenttarget.ScopeRepo

	if value, _ := cmd.Flags().GetString("agent"); value != "" && value != "auto" {
		parsed, perr := agenttarget.ParseAgentFlag(value)
		if perr != nil {
			return nil, "", skillReportAgentError(cmd, perr)
		}
		for i := range agents {
			agents[i].Selected = false
			for _, a := range parsed {
				if agents[i].ID == a.ID {
					agents[i].Selected = true
				}
			}
		}
	}
	if value, _ := cmd.Flags().GetString("scope"); value != "" {
		parsed, perr := agenttarget.ParseScope(value)
		if perr != nil {
			return nil, "", skillReportError(cmd, "invalid --scope", perr.Error(), "", output.ExitCodeGeneral, perr)
		}
		defaultScope = parsed
	}
	return agents, defaultScope, nil
}

// ── Interactive entry point ─────────────────────────────────────────

// runSkillInstallInteractive runs the three-stage form (bubbletea) and
// drives the shared gated batch install path with the selections.
func runSkillInstallInteractive(cmd *cobra.Command) error {
	if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
		// --json is the automation surface; it never opens the form.
		return skillInteractiveUnavailableError(cmd)
	}
	if !stdinIsTerminal(cmd) {
		return skillInteractiveUnavailableError(cmd)
	}

	// Discovery and option building happen BEFORE the program starts:
	// they are normal I/O operations and must not run with the terminal
	// in raw mode (bubbletea enables raw mode for the form).
	names, corruptStdCount, err := discoverBatchSkills()
	if err != nil {
		return skillReportError(cmd, "could not discover available skills", err.Error(), "", output.ExitCodeGeneral, err)
	}
	if len(names) == 0 {
		reason := "no embedded core skills and no installed standards declare skills"
		if corruptStdCount > 0 {
			reason = fmt.Sprintf("no skills available — %d installed-standard record(s) could not be read", corruptStdCount)
		}
		return skillReportError(cmd,
			"no skills available to install",
			reason,
			"Install a standard that ships skills first, or check your setup",
			output.ExitCodeRuntime, nil)
	}
	if corruptStdCount > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %d installed-standard record(s) could not be read and were skipped during skill discovery\n", corruptStdCount)
	}

	// Seed the form from explicit flags: an explicitly-passed --agent /
	// --scope pre-selects the matching rows (the form stays authoritative
	// — the user can change anything before submit). An explicit but
	// INVALID value fails fast with the same error the non-interactive
	// path reports (m3/T1): a typo must never silently fall back to
	// auto-detection. Empty or "auto" keeps the auto-detect default.
	agents, defaultScope, err := resolveInteractiveFlagPresets(cmd)
	if err != nil {
		return err
	}

	model := newInstallFormModel(
		interactiveSkillOptions(names), agents, scopeOptions(defaultScope),
	)

	// bubbletea owns raw mode, key decoding (incl. ESC lookahead),
	// signal handling, and in-place frame repainting; it restores the
	// terminal before Run returns, on every exit path.
	program := tea.NewProgram(model,
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	)
	result, err := program.Run()
	if err != nil {
		return skillReportError(cmd, "interactive install failed", err.Error(), "", output.ExitCodeGeneral, err)
	}
	model, ok := result.(installFormModel)
	if !ok {
		return skillReportError(cmd, "interactive install failed", "unexpected form result", "", output.ExitCodeGeneral, nil)
	}

	switch {
	case model.completed():
		selNames, selAgents, selScope := model.selections()
		renderInteractiveSelection(cmd, selNames, selAgents, selScope)
		return runBatchSkillInstall(cmd, selNames, selAgents, selScope, false)
	case model.aborted == abortCtrlC:
		return skillReportError(cmd,
			"interactive install cancelled",
			"no skills were installed",
			"Run 'anvil skill install' again, or use 'anvil skill install <name>' / 'anvil skill install --all'",
			output.ExitCodeGeneral, nil)
	default:
		// Escape or a closed stdin: the form was not usable.
		return skillInteractiveAbortError(cmd)
	}
}

// ── Shared batch runner (extracted from runSkillInstallAll) ─────────

// runBatchSkillInstall installs the given skills for the given agents at
// the given scope through the shared gated pipeline (per-skill failure
// isolation, installOneSkill), then reports the outcome. It is the
// common execution tail of --all (runSkillInstallAll) and the
// interactive installer (runSkillInstallInteractive) — both resolve
// their inputs and meet here, so the form never bypasses the adoption
// pipeline (ADR-037 D4).
func runBatchSkillInstall(cmd *cobra.Command, names []string, agents []agenttarget.Agent, scope agenttarget.Scope, force bool) error {
	// Scope preconditions are checked exactly like the single/--all path
	// (typed classification: repo without a project is exit 4).
	if _, serr := skillScopeBase(scope); serr != nil {
		return skillReportScopeError(cmd, fmt.Sprintf("cannot resolve the %s scope", scope), serr)
	}
	if len(names) == 0 {
		return skillReportError(cmd,
			"no skills selected to install",
			"the selection resolved to an empty skill set",
			"Run 'anvil skill install <name>' or 'anvil skill install --all'",
			output.ExitCodeGeneral, nil)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")

	var results []skillBatchResult
	var anyFailed bool

	for _, name := range names {
		// Suppress per-skill stdout output in batch mode so that
		// individual skill reports do not pollute the aggregated batch
		// output (human or JSON). StepReporter progress and errors still
		// land on stderr. We set the command's own writer to discard and
		// then clear it (nil) so stdout falls back to the parent's writer
		// — this avoids pinning the command to a specific buffer across
		// test invocations (cobra does not reset subcommand writers).
		cmd.SetOut(io.Discard)
		result, installErr := installOneSkill(cmd, name, scope, agents, force)
		cmd.SetOut(nil)
		if installErr != nil {
			anyFailed = true
			results = append(results, skillBatchResult{Name: name, Error: installErr})
		} else {
			results = append(results, skillBatchResult{Name: name, Result: result})
		}
	}

	if jsonOutput {
		return reportBatchInstallJSON(cmd, results)
	}
	return reportBatchInstall(cmd, results, anyFailed)
}
