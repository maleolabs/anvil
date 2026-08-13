// Package agenttarget implements the per-agent target mapping for skill
// distribution (TS-021-02; ADR-037 D5–D7).
//
// The package answers three questions for `anvil skill install`:
//
//  1. Which agents? `--agent <value>` parses to a resolved agent set
//     (`all`, a single supported agent, or auto-detected from the config
//     folders present on the machine). Unsupported agents (instruction-only
//     tools such as Continue, Aider, GitHub Copilot) are rejected with a
//     notice — never silently skipped.
//  2. Where? `--scope repo|global` resolves a scope base (git root for
//     repo — which requires an Anvil project; home directory for global).
//     Each agent then resolves to its skill directory: the master copy at
//     `<scope>/.agents/skills/<name>/`, plus native symlink locations for
//     agents that do not read `.agents/skills` natively.
//  3. Safe? The writer is symlink-first with a copy fallback (Windows or
//     no privilege), every file lands atomically, and conflict/shadow
//     detection follows each agent's precedence — never a silent
//     overwrite.
//
// The resolved targets (`[]Target`) are the shape consumed by the
// installed-skills record store (TS-021-03): this package resolves and
// writes, the record persists `targets[]`.
//
// Reference: TS-021-02, ADR-037 D5/D6/D7, agentskills.io (verified
// 2026-08-12)
package agenttarget

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Agent identifies one supported AI coding agent and carries its skill
// directory mapping.
//
// The path table is centralized here and pinned by tests (ADR-037 §9.3):
// every agent's repo/global locations are derived from these fields, so a
// drift in one agent's convention is a one-line change plus a test update.
type Agent struct {
	// ID is the canonical `--agent` value (for example "claude-code").
	ID string

	// DisplayName is the human-readable agent name used in messages.
	DisplayName string

	// DetectFolder is the config-folder path whose presence marks the
	// agent as installed for auto-detection (for example ".claude").
	// When DetectInConfig is false the folder is resolved relative to the
	// home directory; when true it is resolved relative to
	// os.UserConfigDir() (XDG_CONFIG_HOME on Linux — used by opencode and
	// zed, whose configs live under `~/.config/`). Empty when the agent is
	// not auto-detectable from a config folder.
	DetectFolder   string
	DetectInConfig bool

	// Selectable reports whether the agent may be passed to `--agent`.
	// Roo Code is detectable (reads `.agents/skills` natively) but is not
	// a selectable target in v1.
	Selectable bool

	// NativeRepoRel and NativeGlobalRel are the agent's native skill
	// directories, relative to the scope base (git root for repo, home
	// for global). Empty means the agent reads the `.agents/skills` master
	// copy natively and needs no native location.
	NativeRepoRel   string
	NativeGlobalRel string

	// ReadsAgentsSkills reports whether the agent reads
	// `<scope>/.agents/skills/<name>/` natively. Agents that do not
	// (Claude Code, Cursor) need a symlink (or copy) from their native
	// location to the master (ADR-037 D6).
	ReadsAgentsSkills bool

	// PrecedenceGlobalWins reports whether the agent's global scope
	// shadows its repo scope (Claude Code personal > project; Cline
	// global > project). False means repo shadows global (most others;
	// ADR-037 D7).
	PrecedenceGlobalWins bool
}

// Supported selectable agents (ADR-037 D5): the `--agent` value set.
var (
	// AgentClaudeCode targets Anthropic Claude Code. Reads
	// `.claude/skills/` natively; follows symlinks (verified); personal
	// (global) shadows project.
	AgentClaudeCode = Agent{
		ID:                   "claude-code",
		DisplayName:          "Claude Code",
		DetectFolder:         ".claude",
		Selectable:           true,
		NativeRepoRel:        filepath.Join(".claude", "skills"),
		NativeGlobalRel:      filepath.Join(".claude", "skills"),
		ReadsAgentsSkills:    false,
		PrecedenceGlobalWins: true,
	}

	// AgentOpenCode targets OpenCode. Reads `.agents/skills/` natively
	// (and `.opencode/skills/`, `.claude/skills/` per docs, verified
	// 2026-08-12) — the master copy is its readable location, so no native
	// entry is needed. Detected via `~/.config/opencode` (XDG). Project
	// shadows global.
	AgentOpenCode = Agent{
		ID:                   "opencode",
		DisplayName:          "OpenCode",
		DetectFolder:         "opencode",
		DetectInConfig:       true,
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentCodex targets OpenAI Codex. Reads `.agents/skills/` natively
	// (ADR-037 §1); project shadows global.
	AgentCodex = Agent{
		ID:                   "codex",
		DisplayName:          "Codex",
		DetectFolder:         ".codex",
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentGemini targets Google Gemini CLI. Reads `.agents/skills/`
	// natively (ADR-037 §1); project shadows global.
	AgentGemini = Agent{
		ID:                   "gemini",
		DisplayName:          "Gemini CLI",
		DetectFolder:         ".gemini",
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentCursor targets Cursor. Reads `.cursor/skills/` natively and —
	// since 2026 — `.agents/skills/` as well (docs, verified 2026-08-12;
	// Sprint Assumption 7). The native symlink is kept for compatibility
	// with older Cursor versions. Project shadows global.
	AgentCursor = Agent{
		ID:                   "cursor",
		DisplayName:          "Cursor",
		DetectFolder:         ".cursor",
		Selectable:           true,
		NativeRepoRel:        filepath.Join(".cursor", "skills"),
		NativeGlobalRel:      filepath.Join(".cursor", "skills"),
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentZed targets Zed. Reads `.agents/skills/` natively (ADR-037
	// §1); project shadows global. Detected via `~/.config/zed` (XDG).
	AgentZed = Agent{
		ID:                   "zed",
		DisplayName:          "Zed",
		DetectFolder:         "zed",
		DetectInConfig:       true,
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentWindsurf targets Windsurf. Reads `.agents/skills/` natively
	// (ADR-037 §1); project shadows global. Not auto-detectable from a
	// config folder in v1 (no folder pinned in the ticket).
	AgentWindsurf = Agent{
		ID:                   "windsurf",
		DisplayName:          "Windsurf",
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}

	// AgentCline targets Cline. Reads `.agents/skills/` natively
	// (ADR-037 §1); global shadows project (ADR-037 D7).
	AgentCline = Agent{
		ID:                   "cline",
		DisplayName:          "Cline",
		DetectFolder:         ".cline",
		Selectable:           true,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: true,
	}

	// AgentRoo targets Roo Code. Reads `.roo/skills/` and `.agents/skills/`
	// natively (docs, verified 2026-08-12); follows symlinks; project
	// shadows global. Detectable from `~/.roo` but not a selectable
	// `--agent` value in v1 — the `.agents/skills` master copy covers it.
	AgentRoo = Agent{
		ID:                   "roo",
		DisplayName:          "Roo Code",
		DetectFolder:         ".roo",
		Selectable:           false,
		ReadsAgentsSkills:    true,
		PrecedenceGlobalWins: false,
	}
)

// agentsByID indexes every agent by ID for parse/lookup.
var agentsByID = func() map[string]Agent {
	m := make(map[string]Agent, 10)
	for _, a := range AllAgents() {
		m[a.ID] = a
	}
	return m
}()

// AllAgents returns every agent in the table (selectable and detectable),
// in a stable order.
func AllAgents() []Agent {
	return []Agent{
		AgentClaudeCode, AgentOpenCode, AgentCodex, AgentGemini,
		AgentCursor, AgentZed, AgentWindsurf, AgentCline, AgentRoo,
	}
}

// SelectableAgents returns the agents that may be passed to `--agent`
// (all except Roo in v1).
func SelectableAgents() []Agent {
	var out []Agent
	for _, a := range AllAgents() {
		if a.Selectable {
			out = append(out, a)
		}
	}
	return out
}

// SelectableIDs returns the canonical `--agent` values in the order of the
// ADR-037 D5 value set: all | claude-code | opencode | codex | gemini |
// cursor | zed | windsurf | cline.
func SelectableIDs() []string {
	return []string{
		"all", "claude-code", "opencode", "codex", "gemini",
		"cursor", "zed", "windsurf", "cline",
	}
}

// unsupportedAgents are known agent tools that are out of scope for v1
// (ADR-037 §7: instruction-only agents) and are reported with a notice.
var unsupportedAgents = map[string]string{
	"continue": "Continue is an instruction-only agent (ADR-037 §7); skills are not supported for it in v1",
	"aider":    "Aider is an instruction-only agent (ADR-037 §7); skills are not supported for it in v1",
	"copilot":  "GitHub Copilot custom instructions are out of scope for v1 (ADR-037 §7); skills are not supported for it",
	"roo":      "Roo Code reads the .agents/skills master copy natively and is covered by any scope-level install — it is not a selectable --agent value in v1",
}

// ParseAgentFlag resolves the `--agent` value to the set of agents to
// install for.
//
//   - "all" returns every selectable agent.
//   - a supported agent ID returns that single agent.
//   - a known unsupported agent (continue/aider/copilot, or roo) returns
//     an UnsupportedAgentError carrying the notice.
//   - anything else returns an actionable error listing the valid values.
//
// The special value "auto" resolves to auto-detection (see DetectAgents);
// it is not a documented `--agent` value but is used by the command layer
// as the default when no `--agent` is supplied.
func ParseAgentFlag(value string) ([]Agent, error) {
	switch value {
	case "all":
		return SelectableAgents(), nil
	case "auto":
		return nil, nil // caller resolves via DetectAgents
	}
	if a, ok := agentsByID[value]; ok {
		if !a.Selectable {
			return nil, &UnsupportedAgentError{Agent: value, Notice: unsupportedAgents[value]}
		}
		return []Agent{a}, nil
	}
	if notice, ok := unsupportedAgents[value]; ok {
		return nil, &UnsupportedAgentError{Agent: value, Notice: notice}
	}
	return nil, &UnknownAgentError{Value: value, Valid: SelectableIDs()}
}

// DetectAgents auto-detects the installed agents from their config folders
// (ADR-037 D5; ticket list: ~/.claude, ~/.config/opencode, ~/.codex,
// ~/.gemini, ~/.cursor, ~/.cline, ~/.roo, ~/.config/zed).
//
// Home-based folders resolve under home; XDG folders (opencode, zed)
// resolve under os.UserConfigDir() so XDG_CONFIG_HOME is honored (m-2).
// The result contains every agent whose config folder exists, in table
// order, including Roo (detectable but not selectable). When no agent is
// detected the returned slice is empty — the caller decides the error.
func DetectAgents(home string) []Agent {
	configDir, configErr := os.UserConfigDir()
	var detected []Agent
	for _, a := range AllAgents() {
		if a.DetectFolder == "" {
			continue
		}
		base := home
		if a.DetectInConfig {
			if configErr != nil {
				continue // no config dir to check; agent not detectable
			}
			base = configDir
		}
		folder := filepath.Join(base, a.DetectFolder)
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			detected = append(detected, a)
		}
	}
	return detected
}

// DetectAgentsSelectable is DetectAgents filtered to selectable agents.
// Roo alone is never enough to trigger an install.
func DetectAgentsSelectable(home string) []Agent {
	var out []Agent
	for _, a := range DetectAgents(home) {
		if a.Selectable {
			out = append(out, a)
		}
	}
	return out
}

// UnknownAgentError reports an `--agent` value that is neither supported
// nor a known unsupported tool.
type UnknownAgentError struct {
	Value string
	Valid []string
}

func (e *UnknownAgentError) Error() string {
	return fmt.Sprintf("unknown agent %q — supported values: %s", e.Value, strings.Join(e.Valid, " | "))
}

// UnsupportedAgentError reports a known agent that v1 does not support for
// skill installation (ADR-037 §7). It carries the notice so the caller can
// present it without dropping the agent's name.
type UnsupportedAgentError struct {
	Agent  string
	Notice string
}

func (e *UnsupportedAgentError) Error() string {
	return fmt.Sprintf("agent %q is not supported for skill installation: %s", e.Agent, e.Notice)
}

// preferWindowsCopy reports whether the platform cannot create symlinks
// without privilege (Windows) and must fall back to copying. Tests force
// this via the writer's copy mode instead of the platform constant.
func preferWindowsCopy() bool {
	return runtime.GOOS == "windows"
}
