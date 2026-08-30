// Package cmd implements the Anvil CLI commands.
//
// ── Skill Shared (ST-021-01; ADR-037) ────────────────────────────────
//
// Shared plumbing for the "anvil skill" command group: target flags
// (--agent/--scope/--force), the installed-skills and installed-standards
// stores, the core-skill provenance injection, the standard-skill
// resolution/fetch/verification helpers, and the target-path record and
// containment/prune helpers shared by install/update/uninstall.
//
// The standard-skill resolution reads the declaration from the
// installed-standard record's Skills section (ST-021-04, ADR-037 D3 —
// the record IS the skill registry) and resolves the MATCHED standard's
// pinned release metadata from the registry index for the install
// pipeline — see resolveStandardSkill.
package cmd

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/agenttarget"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
)

// ── Target Flags ─────────────────────────────────────────────────────

// addSkillTargetFlags registers the shared --agent / --scope / --force
// flags on an install/update/uninstall command. --json is added
// separately via AddJSONFlag.
func addSkillTargetFlags(cmd *cobra.Command) {
	cmd.Flags().String("agent", "", "agent to install for: all | claude-code | opencode | codex | gemini | cursor | zed | windsurf | cline (default: auto-detect from the agent config folders on this machine)")
	cmd.Flags().String("scope", "", "install scope: repo | global (default: repo — the current Anvil project's git root; requires an Anvil project)")
	cmd.Flags().Bool("force", false, "Replace existing same-name skills at the target locations and ignore shadow warnings (destructive: the replaced content is removed)")
}

// skillAgents resolves the --agent flag into the target agent set. An
// empty (or explicit "auto") value resolves to auto-detection from the
// agent config folders on the machine (ADR-037 D5).
func skillAgents(cmd *cobra.Command) ([]agenttarget.Agent, error) {
	value, _ := cmd.Flags().GetString("agent")
	if value == "" || value == "auto" {
		return (&agenttarget.Installer{}).AutoDetect()
	}
	return agenttarget.ParseAgentFlag(value)
}

// skillScope resolves the --scope flag (default: repo per ADR-037 §4).
func skillScope(cmd *cobra.Command) (agenttarget.Scope, error) {
	value, _ := cmd.Flags().GetString("scope")
	return agenttarget.ParseScope(value)
}

// skillForce resolves the --force flag.
func skillForce(cmd *cobra.Command) (bool, error) {
	return cmd.Flags().GetBool("force")
}

// ── Scope classification (MIN-3) ─────────────────────────────────────

// skillScopePreconditionError marks a repo-scope failure caused by a
// missing prerequisite (no Anvil project or no git repository) — mapped
// to exit 4 (TS-P8-07 precondition).
type skillScopePreconditionError struct{ err error }

func (e *skillScopePreconditionError) Error() string { return e.err.Error() }

func (e *skillScopePreconditionError) Unwrap() error { return e.err }

// skillScopeBase resolves the scope base with typed precondition
// classification: for the repo scope the missing-prerequisite failures
// (no Anvil project, no git repository) come back as
// *skillScopePreconditionError (exit 4); any other failure stays a
// general error (exit 1). The classification uses exported, typed
// primitives (project.Discover + a git-root probe) — never error-string
// matching.
func skillScopeBase(scope agenttarget.Scope) (string, error) {
	if scope == agenttarget.ScopeRepo {
		if err := skillRepoScopePrecondition(); err != nil {
			return "", err
		}
	}
	return agenttarget.ScopeBase(scope, "")
}

// skillRepoScopePrecondition checks the two repo-scope preconditions
// with typed primitives: an Anvil project must be discoverable
// (project.ErrNoProjectFound is the exported sentinel) and the project
// must live inside a git repository (the .git probe mirrors
// agenttarget's findGitRoot without touching the mapping package).
func skillRepoScopePrecondition() error {
	if _, err := project.Discover(); err != nil {
		if errors.Is(err, project.ErrNoProjectFound) {
			return &skillScopePreconditionError{err: fmt.Errorf(
				"--scope repo requires an Anvil project: %v. Run 'anvil init' to create a project, or use --scope global to install into your home directory", err)}
		}
		return fmt.Errorf("--scope repo: cannot locate the Anvil project: %w", err)
	}
	if _, err := skillFindGitRoot(); err != nil {
		return &skillScopePreconditionError{err: fmt.Errorf(
			"--scope repo requires the Anvil project to be inside a git repository: %v. Run 'git init' in the project root, or use --scope global", err)}
	}
	return nil
}

// skillFindGitRoot locates the git root by walking up from the current
// working directory until a .git entry is found — the same probe
// agenttarget's scope resolution uses, mirrored here so the precondition
// classification stays typed.
func skillFindGitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the current directory: %w", err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the current directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot inspect %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a git repository (no .git found in this directory or any parent)")
		}
		dir = parent
	}
}

// skillScopeExitCode classifies a scope error into its deterministic
// exit code: a precondition (typed *skillScopePreconditionError) is exit
// 4, anything else exit 1.
func skillScopeExitCode(err error) int {
	var pre *skillScopePreconditionError
	if errors.As(err, &pre) {
		return output.ExitCodePrecondition
	}
	return output.ExitCodeGeneral
}

// ── Stores ───────────────────────────────────────────────────────────

// skillStore returns the installed-skills record store under the Anvil
// global config directory (TS-021-03).
func skillStore() (*registry.InstalledSkillStore, error) {
	dir, err := registry.DefaultInstalledSkillsDir()
	if err != nil {
		return nil, fmt.Errorf("resolve the installed-skills directory: %w", err)
	}
	return registry.NewInstalledSkillStore(dir), nil
}

// skillStandardStore returns the installed-standard record store
// (TS-014-03-03). It satisfies registry.StandardLookup, so it can back
// the stale-status queries of the installed-skills store.
func skillStandardStore() (*registry.InstalledStandardStore, error) {
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return nil, fmt.Errorf("resolve the installed-standards directory: %w", err)
	}
	return registry.NewInstalledStandardStore(dir), nil
}

// ── Core Skill Materialization ───────────────────────────────────────

// coreProvenancePrefix is the provenance comment prefix injected into
// installed SKILL.md frontmatter (ADR-037 D10) — the same canonical line
// the bundle extractor writes (skill-bundle-format.md §5.4).
const coreProvenancePrefix = "# source:"

// injectCoreProvenance injects the provenance header
// "# source: core <cli-version>" into the embedded core SKILL.md
// frontmatter as a YAML comment (ADR-037 D10).
//
// Unlike skillbundle.InjectProvenance, the version is NOT required to be
// strict semver: the CLI version is the version of every core skill
// (ADR-037 D2, lockstep) and may carry a build suffix such as
// "0.0.0-dev" (cmd.CliVersion default). The injected line is the same
// canonical comment shape the bundle extractor writes, so the installed
// copy is indistinguishable from an extracted one. Injection is
// idempotent: an existing "# source:" comment line is replaced.
//
// Seam: if core versions must become semver-strict (T-007/T-009), this
// helper can delegate to skillbundle.InjectProvenance unchanged.
func injectCoreProvenance(skillMD []byte, cliVersion string) ([]byte, error) {
	if cliVersion == "" {
		return nil, fmt.Errorf("inject core provenance: the CLI version is empty")
	}
	contentStart, contentEnd, err := coreFrontmatterBlock(skillMD)
	if err != nil {
		return nil, err
	}
	line := []byte(coreProvenancePrefix + " core " + cliVersion)

	// Replace an existing provenance comment line, if any (preserving the
	// original line terminator — the same byte-preserving logic as the
	// bundle extractor's InjectProvenance).
	pos := contentStart
	for {
		l, lineEnd, ok := nextLine(skillMD, pos)
		if !ok || lineEnd > contentEnd {
			break
		}
		if isCoreProvenanceComment(l) {
			out := make([]byte, 0, len(skillMD)+len(line)-len(l))
			out = append(out, skillMD[:pos]...)
			out = append(out, line...)
			out = append(out, skillMD[pos+len(l):lineEnd]...)
			out = append(out, skillMD[lineEnd:]...)
			return out, nil
		}
		pos = lineEnd
	}

	// No existing header: insert it as the first line of the frontmatter
	// content, preserving every original byte.
	out := make([]byte, 0, len(skillMD)+len(line)+1)
	out = append(out, skillMD[:contentStart]...)
	out = append(out, line...)
	out = append(out, '\n')
	out = append(out, skillMD[contentStart:contentEnd]...)
	out = append(out, skillMD[contentEnd:]...)
	return out, nil
}

// coreFrontmatterBlock locates the frontmatter block of a SKILL.md
// document (the same shape the skillbundle frontmatter parser enforces):
// the document must open with a '---' delimiter line and close with a
// '---' delimiter line. It returns the byte offsets of the block content
// (between the delimiters). The document is expected to have passed
// skillbundle.ParseFrontmatter already; this helper only locates the
// block for injection.
func coreFrontmatterBlock(doc []byte) (contentStart, contentEnd int, err error) {
	first, firstEnd, ok := nextLine(doc, 0)
	if !ok || !isDelimiter(first) {
		return 0, 0, fmt.Errorf("inject core provenance: the SKILL.md does not open with a '---' frontmatter delimiter")
	}
	pos := firstEnd
	for {
		line, lineEnd, ok := nextLine(doc, pos)
		if !ok {
			return 0, 0, fmt.Errorf("inject core provenance: the frontmatter block has no closing '---' delimiter line")
		}
		if isDelimiter(line) {
			return firstEnd, pos, nil
		}
		pos = lineEnd
	}
}

// nextLine returns the next line starting at pos (excluding its line
// ending; a trailing '\r' is stripped so CRLF documents are handled) and
// the offset just past the line ending; ok is false at EOF with no
// further line.
func nextLine(doc []byte, pos int) (line []byte, end int, ok bool) {
	if pos >= len(doc) {
		return nil, pos, false
	}
	start := pos
	for pos < len(doc) && doc[pos] != '\n' {
		pos++
	}
	if pos >= len(doc) {
		return stripCR(doc[start:]), pos, true
	}
	return stripCR(doc[start:pos]), pos + 1, true
}

// stripCR removes a trailing carriage return (CRLF documents).
func stripCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

// isDelimiter reports whether line is exactly "---".
func isDelimiter(line []byte) bool {
	return len(line) == 3 && line[0] == '-' && line[1] == '-' && line[2] == '-'
}

// isCoreProvenanceComment reports whether line is a "# source:" comment
// (allowing leading whitespace, as YAML permits indented comments).
func isCoreProvenanceComment(line []byte) bool {
	trimmed := strings.TrimLeft(string(line), " \t")
	return strings.HasPrefix(trimmed, coreProvenancePrefix)
}

// validateCoreSkillContent validates an embedded core skill the same way
// the bundle extractor validates a bundle's SKILL.md: the frontmatter
// must pass the portable-field parse (agentskills.io; ADR-037 D1) and
// the frontmatter name must equal the skill directory name. It returns
// the validated frontmatter. The content is then provenance-injected and
// materialized.
func validateCoreSkillContent(skillName string, skillMD []byte) (*skillbundle.Frontmatter, error) {
	fm, err := skillbundle.ParseFrontmatter(skillMD)
	if err != nil {
		return nil, fmt.Errorf("the embedded core skill %q is rejected by the portable frontmatter validation (agentskills.io; ADR-037 D1) — fix internal/skills/core/%s/SKILL.md: %w", skillName, skillName, err)
	}
	if fm.Name != skillName {
		return nil, fmt.Errorf("the embedded core skill %q declares frontmatter name %q — the skill's identity must match its directory (agentskills.io)", skillName, fm.Name)
	}
	return fm, nil
}

// ── Standard Skill Resolution ────────────────────────────────────────

// skillStandardMatch is the outcome of resolving a standard-sourced
// skill: the parsed metadata of the source standard's PINNED release and
// the skill declaration.
type skillStandardMatch struct {
	Metadata registry.Metadata
	Skill    registry.Skill
}

// skillResolutionError classifies a skill-resolution failure for exit
// codes (TS-P8-07; MIN-4): a genuinely absent skill (no installed
// standard declares it) is "not found" (exit 3); every other resolution
// failure — an unreadable index, a corrupt index document, an unreadable
// standard store, or an ambiguous declaration — is an environment or
// data problem (exit 1).
type skillResolutionError struct {
	// notProvided marks the skill as genuinely absent.
	notProvided bool
	err         error
}

func (e *skillResolutionError) Error() string { return e.err.Error() }

func (e *skillResolutionError) Unwrap() error { return e.err }

// skillResolutionNotFound reports whether a resolution failure means the
// skill is genuinely absent (exit 3) rather than an environment problem.
func skillResolutionNotFound(err error) bool {
	var resErr *skillResolutionError
	return errors.As(err, &resErr) && resErr.notProvided
}

// skillResolutionNotes carries advisory facts gathered while resolving —
// unreadable installed-standard records — surfaced as hints by the caller
// without failing the resolution (MIN-5).
type skillResolutionNotes struct {
	// CorruptRecords is the count of installed-standard records that
	// could not be read.
	CorruptRecords int
}

// empty reports whether the notes carry nothing to surface.
func (n skillResolutionNotes) empty() bool {
	return n.CorruptRecords == 0
}

// hints renders the advisory notes as actionable stderr hints.
func (n skillResolutionNotes) hints() []string {
	var out []string
	if n.CorruptRecords > 0 {
		out = append(out, fmt.Sprintf("%d installed-standard record(s) could not be read and were skipped during resolution", n.CorruptRecords))
	}
	return out
}

// skillDeclarationMatch is the outcome of matching a standard-sourced
// skill against the installed-standard records: the record that declares
// it and the declaration.
type skillDeclarationMatch struct {
	Record      registry.InstalledStandardRecord
	Declaration registry.SkillDeclaration
}

// resolveStandardSkill resolves a standard-sourced skill by name
// (ADR-037 D4): it iterates the installed-standard records and matches
// the record's Skills declarations (ST-021-04 — the record IS the skill
// registry, ADR-037 D3). The skill is installable only when its source
// standard is installed and its declaration is registered.
//
// The declaration source is the RECORD, never a search over the registry
// index (W2 T-003 seam, resolved by T-006): discovery works even when the
// index is missing or stale. The registry index is consulted only for the
// MATCHED standard's pinned release metadata — the strict-parsed document
// that carries the asset URL, the attested named digests, and the
// lifecycle/compatibility declarations the install pipeline needs
// (TS-021-04, TS-014-04-04). A matched standard whose pinned release
// metadata is missing or invalid in the index cannot provide skills and
// the resolution fails with an actionable error (exit 1 — an environment
// problem, not a missing skill).
//
// Exactly one match is required: no match yields a typed
// *skillResolutionError with notProvided=true (exit 3); multiple matches
// (the same skill name declared by two installed standards) is an
// ambiguity error (exit 1) — each standard's skills live under its own
// namespace (skills/<standard-id>/<name>, ADR-037 §7).
//
// Unreadable installed-standard records are counted in the returned notes
// (MIN-5) — never silently dropped. The strict-parsed metadata's own
// declaration is used for the install pipeline (parser-validated and
// digest-bound); a record declaration that the release metadata no longer
// carries is a record/metadata divergence and fails with an actionable
// error.
func resolveStandardSkill(cmd *cobra.Command, skillName string) (*skillStandardMatch, skillResolutionNotes, error) {
	var notes skillResolutionNotes

	store, err := skillStandardStore()
	if err != nil {
		return nil, notes, fmt.Errorf("resolve standard skill %q: %w", skillName, err)
	}
	records, corrupt, err := store.ListRecords()
	if err != nil {
		return nil, notes, fmt.Errorf("resolve standard skill %q: read installed standards: %w", skillName, err)
	}
	notes.CorruptRecords = len(corrupt)

	var matches []skillDeclarationMatch
	var providerNames []string // every standard that declares ANY skill (for the not-found message)
	var installedIDs []string  // every readable installed standard (for the no-declarations hint)
	for _, rec := range records {
		installedIDs = append(installedIDs, rec.ID)
		if len(rec.Skills) == 0 {
			continue
		}
		providerNames = append(providerNames, rec.ID)
		for _, sk := range rec.Skills {
			if sk.Name == skillName {
				matches = append(matches, skillDeclarationMatch{Record: rec, Declaration: sk})
			}
		}
	}

	if len(matches) == 0 {
		msg := fmt.Sprintf("skill %q is not provided by any installed standard", skillName)
		switch {
		case len(providerNames) > 0:
			msg += fmt.Sprintf(" (standards that declare skills: %s)", strings.Join(providerNames, ", "))
		case len(installedIDs) > 0:
			// The standard IS installed, but its record carries no
			// declarations — a record from an older CLI version
			// (pre-ST-021-04, format 1) or a release that ships no
			// skills. The fix for a legacy record is an explicit
			// re-adoption, which registers the declarations.
			msg += fmt.Sprintf(
				" — the installed standard(s) %s declare no skills; a standard's skills are registered at its install or update — refresh the record with 'anvil standard install <id> <version>' or 'anvil standard update <id> <version>' to register its skills",
				strings.Join(installedIDs, ", "))
		default:
			msg += " — no installed standard declares skills (install a standard that ships skills first)"
		}
		return nil, notes, &skillResolutionError{notProvided: true, err: errors.New(msg)}
	}
	if len(matches) > 1 {
		// Deduplicate the standard ids: a hand-edited record may declare
		// the same skill name twice, which must not render as a repeated
		// id — or as "multiple installed standards" when only ONE
		// standard is involved (that is an inconsistent record, not an
		// ambiguity).
		var ids []string
		seen := make(map[string]bool, len(matches))
		for _, m := range matches {
			if seen[m.Record.ID] {
				continue
			}
			seen[m.Record.ID] = true
			ids = append(ids, m.Record.ID)
		}
		if len(ids) == 1 {
			return nil, notes, fmt.Errorf(
				"the installed record of %s declares skill %q more than once — the record is inconsistent (duplicate declarations); re-install or update the standard to refresh its declarations",
				ids[0], skillName)
		}
		return nil, notes, fmt.Errorf(
			"skill %q is declared by multiple installed standards (%s) — each standard's skills live under its own namespace (skills/<standard-id>/<name>); install the standard that owns the skill, or uninstall the standard whose skill you do not want",
			skillName, strings.Join(ids, ", "))
	}

	// Single match: resolve the matched standard's PINNED release
	// metadata from the registry index — the pipeline input (asset URL,
	// attested named digests, lifecycle + compatibility declarations).
	// The index is consulted ONLY for the matched standard: discovery is
	// record-based, unrelated standards are never index-resolved.
	m := matches[0]
	ix, err := loadStandardIndex(cmd)
	if err != nil {
		return nil, notes, fmt.Errorf("resolve standard skill %q: %w", skillName, err)
	}
	if err != nil {
		return nil, notes, fmt.Errorf("resolve standard skill %q: %w", skillName, err)
	}
	entry, err := ix.Resolve(m.Record.ID, m.Record.Version)
	if err != nil {
		return nil, notes, fmt.Errorf(
			"skill %q is declared by the installed standard %s, but the release metadata of %s %s could not be resolved in the registry index (%v) — the installed record declares the skill, but its asset URL and attested digests come from the release metadata; update the registry index or re-adopt the standard, then run the install again",
			skillName, m.Record.ID, m.Record.ID, m.Record.Version, err)
	}
	md, _, err := parseStandardEntry(entry)
	if err != nil {
		return nil, notes, fmt.Errorf(
			"skill %q is declared by the installed standard %s, but the release metadata of %s %s failed strict validation (%v) — the release cannot provide skills until its metadata is fixed; update the registry index or re-adopt the standard, then run the install again",
			skillName, m.Record.ID, m.Record.ID, m.Record.Version, err)
	}

	// The strict-parsed metadata's own declaration is authoritative for
	// the install pipeline (parser-validated, digest-bound). A record
	// declaration the release metadata no longer carries — or that
	// diverges on version/asset (a tampered index, a re-published
	// release, or a hand-edited record) — is a record/metadata
	// divergence and fails with an actionable error rather than
	// installing from unvalidated state.
	var sk *registry.Skill
	for i := range md.Skills {
		if md.Skills[i].Name == skillName {
			sk = &md.Skills[i]
			break
		}
	}
	if sk == nil {
		return nil, notes, fmt.Errorf(
			"skill %q is declared by the installed record of %s %s, but the release metadata of that pinned version no longer declares it — the record and the release metadata disagree; re-install or update the standard to refresh the declarations",
			skillName, m.Record.ID, m.Record.Version)
	}
	if sk.Version != m.Declaration.Version || sk.Asset != m.Declaration.Asset {
		return nil, notes, fmt.Errorf(
			"the installed record of %s %s declares skill %q as version %s with asset %s, but the release metadata of that pinned version declares version %s with asset %s — the record and the release metadata disagree; re-install or update the standard to refresh the declarations",
			m.Record.ID, m.Record.Version, skillName, m.Declaration.Version, m.Declaration.Asset, sk.Version, sk.Asset)
	}
	return &skillStandardMatch{Metadata: *md, Skill: *sk}, notes, nil
}

// skillAssetURL builds the https URL of a skill's release asset: the
// release channel base of the standard's distribution location plus the
// declared asset identifier (skills[].asset, e.g.
// anvil-skill-overview-1-0-0 — the physical
// anvil-skill-<name>-<version>.tar.gz file is bound to that identifier
// by the release pipeline, ADR-037 D2).
func skillAssetURL(md *registry.Metadata, sk registry.Skill) (string, error) {
	base, err := standardReleaseDownloadBase(md.Distribution.Location)
	if err != nil {
		return "", err
	}
	return standardReleaseAssetURL(base, sk.Asset)
}

// skillAssetMaxBytes caps a single skill asset download BEFORE any
// extraction (security INFO-2 from T-002: the skill install is
// digest-verified AND size-checked). A skill bundle's uncompressed
// content is capped at skillbundle.MaxTotalSize (64 MiB) with a 10 MiB
// per-asset cap; the download cap adds 1 MiB headroom for the gzip/tar
// framing so a legitimate bundle is never rejected, while an oversized
// asset is reported precisely instead of buffered unbounded. It is a
// variable so tests can shrink it; the production value is fixed.
var skillAssetMaxBytes = int64(skillbundle.MaxTotalSize) + 1<<20

// skillAssetFetch downloads a skill asset from its https release-channel
// URL under the hardened fetch policy (ADR-030 §3): https-only,
// userinfo rejected, bounded redirects, the size cap enforced DURING the
// download via a limit reader, and the download timeout model (TD-008 —
// connection bounded by the transport, body read bounded by the idle
// window). It returns the content, its lowercase-hex SHA-256 (the shape
// VerifyAssetDigest consumes), and the ACTUAL endpoint used — the final
// response URL after any allowed redirects — which the caller records as
// the explicit resolution (ADR-022 §3).
func skillAssetFetch(location string) ([]byte, string, string, error) {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, "", "", fmt.Errorf(
			"the skill asset location %s is not a well-formed https URL; skill assets are fetched over TLS only (ADR-030 §3)",
			standardScrubLocation(location))
	}
	if parsed.User != nil {
		return nil, "", "", fmt.Errorf(
			"the skill asset location %s carries userinfo (username or password); credentials would be sent as Basic auth and recorded — publish the skill asset at a location without userinfo (ADR-030 §3)",
			standardURLWithoutUserinfo(parsed))
	}

	req, err := http.NewRequest(http.MethodGet, location, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("could not build the request for %s: %w", location, err)
	}
	resp, err := standardInstallHTTPClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf(
			"the skill asset at %s could not be reached: %v. If you are the publisher, fix the release asset; otherwise report the broken release",
			location, httpErrorWithTimeout(downloadResponseHeaderTimeout, standardScrubURLError(err)))
	}
	defer resp.Body.Close()

	// Bound the body read by ACTIVITY, not by a total deadline (TD-008).
	resp.Body = newIdleTimeoutBody(resp.Body, downloadIdleTimeout())

	// Defensive re-checks after redirects: the final endpoint must stay
	// https and userinfo-free (credentials are never sent or recorded).
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return nil, "", "", fmt.Errorf(
			"the skill asset at %s resolved to a non-https response; skill assets are fetched over TLS only (ADR-030 §3)", location)
	}
	if resp.Request.URL.User != nil {
		return nil, "", "", fmt.Errorf(
			"the skill asset at %s resolved to %s, which carries userinfo; credentials must never be sent or recorded (ADR-030 §3)",
			location, standardURLWithoutUserinfo(resp.Request.URL))
	}
	contentSource := resp.Request.URL.String()

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fetchStatusError(location, resp.StatusCode)
	}

	// The size cap is enforced DURING the download (at most cap+1 bytes
	// are read) — an oversized asset is reported precisely instead of
	// buffered unbounded (security INFO-2, T-002).
	body, err := io.ReadAll(io.LimitReader(resp.Body, skillAssetMaxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf(
			"the skill asset at %s could not be downloaded: %v. If you are the publisher, fix the release asset; otherwise report the broken release",
			location, httpErrorWithTimeout(downloadIdleTimeout(), err))
	}
	if int64(len(body)) > skillAssetMaxBytes {
		return nil, "", "", fmt.Errorf(
			"the skill asset at %s exceeds the %d-byte download cap; content is never buffered unbounded. If you are the publisher, republish the skill asset under the cap; otherwise report the broken release",
			location, skillAssetMaxBytes)
	}
	sum := sha256.Sum256(body)
	return body, fmt.Sprintf("%x", sum[:]), contentSource, nil
}

// ── Target / Record Helpers ──────────────────────────────────────────

// targetsFromResolvedSet converts a materialized resolved set into the
// record store's targets[] shape ({agent, scope, path}).
func targetsFromResolvedSet(set *agenttarget.ResolvedSet) []registry.InstalledSkillTarget {
	var out []registry.InstalledSkillTarget
	for _, t := range set.Targets {
		out = append(out, registry.InstalledSkillTarget{
			Agent: t.Agent,
			Scope: string(t.Scope),
			Path:  t.Path,
		})
	}
	return out
}

// skillTargetContainment validates that an absolute target path is safe
// to remove or prune for a skill: it must be an absolute path, its final
// path component must be the skill name (every recorded target is
// <scope-base>/<...>/<skill-name>), and it must resolve inside the scope
// base. A hand-edited or stale record pointing anywhere else must never
// be RemoveAll'ed (T-005 reviewer note; defensive filesystem safety).
func skillTargetContainment(path, base, skillName string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("skill target path %q is not an absolute path — refusing to remove it", path)
	}
	if filepath.Base(path) != skillName {
		return fmt.Errorf("skill target path %q does not end with the skill directory %q — refusing to remove it", path, skillName)
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skill target path %q lies outside the scope base %q — refusing to remove it", path, base)
	}
	return nil
}

// skillUninstallPaths returns the distinct absolute paths a recorded
// install left on disk, via agenttarget.ReadAllTargets (the shared
// path-set logic of the mapping package). The record's targets[] ARE the
// resolved set; Master is left empty so every recorded path is returned.
func skillUninstallPaths(rec registry.InstalledSkillRecord) []string {
	set := &agenttarget.ResolvedSet{
		SkillName: rec.ID,
	}
	for _, t := range rec.Targets {
		set.Targets = append(set.Targets, agenttarget.Target{
			Agent: t.Agent,
			Scope: agenttarget.Scope(t.Scope),
			Path:  t.Path,
		})
	}
	return agenttarget.ReadAllTargets(set)
}

// skillTargetKey renders the identity of one recorded target
// (agent\x00scope\x00path) for set-based filtering and dedup.
func skillTargetKey(t registry.InstalledSkillTarget) string {
	return t.Agent + "\x00" + t.Scope + "\x00" + t.Path
}

// skillInstallMarkerName is the ownership marker file the agent-target
// writer writes into every skill directory it creates
// (internal/agenttarget, installMarkerName). It is preserved during
// update pruning because the writer re-writes it; removing it mid-update
// would break the ownership recognition of a crashed update.
const skillInstallMarkerName = ".anvil-install"

// pruneStaleFiles removes, inside one materialized skill directory,
// every entry that is not part of the new content — update refreshes the
// FULL target (re-extract penuh + prune, never overwrite-only; ticket
// item 6 / T-004 N-4). Symlinks are never followed (a symlink entry
// inside the tree is removed as the link itself); the ownership marker
// is preserved. The directory must pass skillTargetContainment.
//
// The order of operations in update is install-then-prune: the new
// content and marker land first, so a prune failure leaves the new
// content present rather than a half-emptied target.
func pruneStaleFiles(dir, base, skillName string, keep map[string]bool) error {
	if err := skillTargetContainment(dir, base, skillName); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("prune %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// A symlink target (Claude Code / Cursor native location): the
		// content lives in the master copy, which is also a target and is
		// pruned there. The symlink itself is refreshed by the writer.
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	return pruneTree(dir, "", keep)
}

// pruneTree removes every non-kept entry under dir recursively, then
// removes directories left empty. relPrefix tracks the skill-relative
// path of the current directory so nested files match the keep set.
func pruneTree(dir, relPrefix string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("prune %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == skillInstallMarkerName {
			continue
		}
		child := filepath.Join(dir, name)
		rel := name
		if relPrefix != "" {
			rel = relPrefix + "/" + name
		}
		if e.IsDir() {
			if err := pruneTree(child, rel, keep); err != nil {
				return err
			}
			// Remove the directory when the prune emptied it.
			if empty, err := isEmptyDir(child); err == nil && empty {
				if err := os.Remove(child); err != nil {
					return fmt.Errorf("prune %s: remove empty directory: %w", child, err)
				}
			}
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			// An extracted skill tree never contains symlinks; a stale
			// symlink inside our own tree is removed as the link itself.
			if err := os.Remove(child); err != nil {
				return fmt.Errorf("prune %s: remove symlink: %w", child, err)
			}
			continue
		}
		if !keep[rel] {
			if err := os.Remove(child); err != nil {
				return fmt.Errorf("prune %s: remove stale file: %w", child, err)
			}
		}
	}
	return nil
}

// isEmptyDir reports whether dir contains no entries.
func isEmptyDir(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// skillPreCleanShapeConflicts removes, inside a kept materialized target
// and BEFORE the install, every existing entry whose path collides in
// SHAPE with the new content — a directory where the new content has a
// file, or a file where the new content has a directory (LOW-2). The
// writer cannot overwrite across shapes (a file at a directory path, or
// a directory at a file path, is a hard error), and the stale-file prune
// runs AFTER the install, so the conflicts must go first. Only our OWN
// target (ownership marker) is touched, and the directory must pass
// skillTargetContainment — a target the user replaced with their own
// content stays for the writer's conflict/--force gate.
func skillPreCleanShapeConflicts(dir, base, skillName string, files map[string][]byte, rec registry.InstalledSkillRecord) error {
	if err := skillTargetContainment(dir, base, skillName); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("pre-clean %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// A symlink target (Claude Code / Cursor native location): the
		// content lives in the master copy, which is also a target and is
		// pre-cleaned there.
		return nil
	}
	if !info.IsDir() {
		return nil
	}
	if !skillTargetIsOurs(dir, skillName, rec) {
		return nil // not our own — the writer's conflict/--force gate owns it
	}
	// The directory set the new content requires: every proper prefix of
	// every content path (and the skill root itself is the target dir).
	newDirs := make(map[string]bool)
	for rel := range files {
		parent := path.Dir(rel)
		for parent != "." && parent != "" {
			newDirs[parent] = true
			parent = path.Dir(parent)
		}
	}
	return preCleanShapeWalk(dir, "", newDirs, files)
}

// preCleanShapeWalk walks one skill directory tree and removes entries
// whose shape conflicts with the new content: an existing directory at a
// path the new content declares as a FILE (files[rel] present) is removed
// in full; an existing file or symlink at a path the new content needs as
// a DIRECTORY (rel ∈ newDirs) is removed. Directories the new content
// populates are walked recursively for deeper conflicts; everything else
// is left for the post-install prune.
func preCleanShapeWalk(dir, relPrefix string, newDirs map[string]bool, files map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("pre-clean %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == skillInstallMarkerName {
			continue
		}
		child := filepath.Join(dir, name)
		rel := name
		if relPrefix != "" {
			rel = relPrefix + "/" + name
		}
		switch {
		case e.IsDir():
			if _, isFile := files[rel]; isFile {
				// The new content declares a FILE here — remove the
				// whole directory so the writer can write the file.
				if err := os.RemoveAll(child); err != nil {
					return fmt.Errorf("pre-clean %s: remove conflicting directory: %w", child, err)
				}
				continue
			}
			if newDirs[rel] {
				if err := preCleanShapeWalk(child, rel, newDirs, files); err != nil {
					return err
				}
			}
			// A directory the new content does not populate is stale —
			// the post-install prune removes it (and its contents).
		case e.Type()&os.ModeSymlink != 0:
			if _, isFile := files[rel]; isFile || newDirs[rel] {
				if err := os.Remove(child); err != nil {
					return fmt.Errorf("pre-clean %s: remove conflicting symlink: %w", child, err)
				}
			}
		default:
			if newDirs[rel] {
				// The new content needs a DIRECTORY here — remove the
				// file so the writer can create the directory.
				if err := os.Remove(child); err != nil {
					return fmt.Errorf("pre-clean %s: remove conflicting file: %w", child, err)
				}
			}
			// A plain file the new content also declares is overwritten
			// by the writer; a stale file is handled by the prune.
		}
	}
	return nil
}

// skillFilesFromExtraction converts a validated bundle extraction into
// the skill-relative content map the agent-target writer consumes:
// Extraction.Files are "<name>/…" paths under the extraction root; the
// content-root prefix is stripped ("<name>/SKILL.md" → "SKILL.md").
func skillFilesFromExtraction(ext *skillbundle.Extraction) (map[string][]byte, error) {
	files := make(map[string][]byte, len(ext.Files))
	prefix := ext.Manifest.Name + "/"
	for _, f := range ext.Files {
		if !strings.HasPrefix(f, prefix) {
			return nil, fmt.Errorf("extracted file %q lies outside the content root %q", f, prefix)
		}
		rel := strings.TrimPrefix(f, prefix)
		data, err := os.ReadFile(filepath.Join(ext.Dest, filepath.FromSlash(f)))
		if err != nil {
			return nil, fmt.Errorf("read extracted file %s: %w", f, err)
		}
		files[rel] = data
	}
	return files, nil
}

// ── Gates ────────────────────────────────────────────────────────────

// skillAdoptionGates runs the lifecycle + compatibility gates of the
// skill install pipeline against the source standard's PINNED release
// (ADR-037 D4; TS-014-04-03, pinned adoption order — the lifecycle gate
// runs before compatibility). For a fresh install the lifecycle gate
// accepts published and deprecated releases (deprecated installs with a
// warning); for an update only published releases pass — the
// deprecated/retired no-updates rule propagates to skills (ADR-023 §3;
// W3 T-006 exit criteria). It returns the pre-fetch adoption result and
// any advisory warnings.
func skillAdoptionGates(cmd *cobra.Command, md *registry.Metadata, update bool) (registry.AdoptionResult, []string, error) {
	if update && !registry.LifecycleUpdateAllowed(md.Lifecycle.State) {
		return registry.AdoptionResult{}, nil, fmt.Errorf(
			"source standard %s %s is %s — skills of deprecated or retired standards receive no updates (ADR-023 §3); re-adopt the standard or uninstall the skill",
			md.ID, md.Version, md.Lifecycle.State)
	}

	supported, err := supportedContractMajors()
	if err != nil {
		return registry.AdoptionResult{}, nil, fmt.Errorf("could not load the compatibility matrix: %w", err)
	}
	projectVersion, err := projectFrameworkVersionForInstall()
	if err != nil {
		return registry.AdoptionResult{}, nil, fmt.Errorf("could not determine the project's framework version: %w", err)
	}

	before := registry.ValidateAdoptionBeforeFetch(*md, supported, projectVersion)
	if !before.Valid {
		if !before.Adoptable {
			return before, nil, fmt.Errorf(
				"source standard %s %s is not offered for adoption (lifecycle %s): %s",
				md.ID, md.Version, md.Lifecycle.State, strings.Join(before.Errors, "; "))
		}
		return before, nil, fmt.Errorf(
			"source standard %s %s is not compatible: %s",
			md.ID, md.Version, strings.Join(before.Errors, "; "))
	}

	var warnings []string
	if warning, ok := registry.LifecycleWarning(md.Lifecycle); ok {
		warnings = append(warnings, warning)
	}
	return before, warnings, nil
}

// ── Error Reporting ──────────────────────────────────────────────────

// skillTargetJSON is the machine-readable shape of one installed target
// ({agent, scope, path}), shared by list/install/update/uninstall.
type skillTargetJSON struct {
	Agent string `json:"agent"`
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

// skillTargetsJSON converts the record store's targets into the
// machine-readable shape.
func skillTargetsJSON(targets []registry.InstalledSkillTarget) []skillTargetJSON {
	out := make([]skillTargetJSON, 0, len(targets))
	for _, t := range targets {
		out = append(out, skillTargetJSON{Agent: t.Agent, Scope: t.Scope, Path: t.Path})
	}
	return out
}

// skillReportError renders a structured error (TS-P8-06) with a
// deterministic exit code (TS-P8-07). With --json the error envelope goes
// to stdout; the returned error still carries the exit code.
func skillReportError(cmd *cobra.Command, message, reason, resolution string, exitCode int, err error) error {
	return ReportErrorWithCode(cmd, &output.AppError{
		Message:    message,
		Reason:     reason,
		Resolution: resolution,
		Err:        err,
	}, exitCode)
}

// skillReportStoreError maps an installed-skills store error to the
// right category: a missing record is "not found" (exit 3), a version
// conflict (install of a skill recorded at a different version) is exit
// 2 with the update hint, anything else is a general error (exit 1).
func skillReportStoreError(cmd *cobra.Command, op, name string, err error) error {
	if errors.Is(err, registry.ErrSkillRecordNotFound) {
		return skillReportError(cmd,
			fmt.Sprintf("skill %q is not installed", name),
			err.Error(),
			fmt.Sprintf("Run 'anvil skill install %s' to install it first", name),
			output.ExitCodeRuntime, err)
	}
	if errors.Is(err, registry.ErrSkillRecordVersionConflict) {
		return skillReportError(cmd,
			fmt.Sprintf("skill %q is recorded at a different version", name),
			err.Error(),
			fmt.Sprintf("Version change is an update — run 'anvil skill update %s'", name),
			output.ExitCodeConfig, err)
	}
	return skillReportError(cmd,
		fmt.Sprintf("could not %s skill %q", op, name),
		err.Error(),
		"",
		output.ExitCodeGeneral, err)
}

// skillReportScopeError renders a scope-resolution failure with the
// typed exit-code classification (precondition → 4, other → 1).
func skillReportScopeError(cmd *cobra.Command, message string, err error) error {
	return skillReportError(cmd, message, err.Error(),
		"Run 'anvil init' in an Anvil project inside a git repository, or use --scope global",
		skillScopeExitCode(err), err)
}

// skillReportResolutionNotes surfaces advisory resolution facts (corrupt
// installed-standard records, index-resolution skips) to stderr without
// failing the command (MIN-5, F-4).
func skillReportResolutionNotes(cmd *cobra.Command, notes skillResolutionNotes) {
	if notes.empty() {
		return
	}
	for _, h := range notes.hints() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s\n", h)
	}
}

// now is the timestamp source for records, so tests can pin time if
// needed.
var now = time.Now

// skillPreflightExisting reads the existing record for an install and
// rejects a version conflict BEFORE any side effect (P2): a record at a
// different version returns a wrapped ErrSkillRecordVersionConflict (exit
// 2) with zero writes and zero fetches; a record at the SAME version is
// returned for the re-install refresh path (P3); a missing or corrupt
// record returns nil (fresh install; corrupt records recover by
// re-adoption).
func skillPreflightExisting(store *registry.InstalledSkillStore, name, newVersion string) (*registry.InstalledSkillRecord, error) {
	rec, err := store.Get(name)
	if err != nil {
		if errors.Is(err, registry.ErrSkillRecordNotFound) || errors.Is(err, registry.ErrSkillRecordCorrupt) {
			return nil, nil
		}
		return nil, err
	}
	if rec.Version != newVersion {
		return nil, fmt.Errorf(
			"%w: skill %q is recorded at version %q, not %q — install never changes versions; run 'anvil skill update %s' to re-adopt",
			registry.ErrSkillRecordVersionConflict, name, rec.Version, newVersion, name)
	}
	return &rec, nil
}
