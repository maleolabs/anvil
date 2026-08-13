// Template content resolution (TS-015-02-03).
//
// Per ADR-026 decision 2, template generation is standard-driven: the
// project content generated at init (the build and CI pipeline
// definitions under .anvil/pipelines/) is supplied by the installed
// delivery lifecycle standard's template content and consumed by the
// Anvil Runtime (Core) through the specification's template declaration
// (EPIC-013). The runtime owns no template content (TS-015-01-02,
// ADR-026 decision 1); the installed-standard record is the
// authoritative local record of what is installed (TS-014-03-03), and
// the standard's template content is part of that record.
//
// The content shape mirrors the template declaration of the capability
// surface (command-contract.schema.json — the machine-readable
// authority, ADR-029 §3): a standard declares templates by stable
// identifier (e.g. "build", "ci" — 005 §5.7). This component carries the
// declared template files as record content: each template is identified
// by its id and carries the pipeline file content the standard supplies
// (the YAML definition the runtime validates through the pipeline loader
// before writing it — the runtime never interprets framework-specific
// content). Concrete template content formats are implementation design
// (EPIC-013 / TS-015-02-03); the record stores the content's JSON shape
// as-is — the store never interprets it, and the runtime validates the
// pipeline files it writes through the same loader used at execution
// time (ADR-007 — pipelines are project configuration the generic engine
// executes).
//
// Resolution semantics (explicit, never invented):
//
//   - the record carries content whose namespace matches the declared
//     framework: the content resolves — the pipeline templates come from
//     the installed standard;
//   - the record carries no content (or an empty templates list): a
//     standard may declare nothing in a category (command-contract §4.1)
//     — the distinguishable no-content outcome
//     (ErrTemplateContentMissing) hands off to the interim
//     adapter-driven generation (TS-015-02-03 keeps the ADR-020 adapter
//     `template` command path as fallback until standard content
//     extraction lands, EPIC-016);
//   - the content's namespace does not match the declared framework:
//     namespace isolation is violated — the record is inconsistent with
//     the standard it belongs to, an actionable error (reinstall the
//     standard), never a silent pass-through;
//   - the standard is not installed: the resolution passes through the
//     no-match hand-off of ResolveFrameworkStandard
//     (ErrStandardNotInstalled).
//
// Reference: TS-015-02-03, ADR-026 decision 2, EPIC-013, ADR-021 §3.1,
// ADR-022 §3, command-contract §4.1
package registry

import (
	"errors"
	"fmt"
)

// ErrTemplateContentMissing reports that the resolved installed standard
// declares no template content: a standard may declare nothing in a
// category (command-contract §4.1), so this is a distinguishable
// no-content outcome, not a failure of the store. It is the hand-off
// signal for the missing-template-content handling (TS-015-02-03): the
// caller decides how the outcome is surfaced — following the same
// hand-off/warning pattern T-004 established for
// ErrConfigExtensionMissing (TS-015-03-01) — and the interim
// adapter-driven generation (ADR-020) covers the gap until standard
// content extraction lands (EPIC-016).
var ErrTemplateContentMissing = errors.New("installed standard declares no template content")

// TemplateFile is one declared pipeline template of the installed
// standard's template content (TS-015-02-03): ID is the template's
// stable identifier — the pipeline position it generates ("build" →
// .anvil/pipelines/build.yaml, "ci" → .anvil/pipelines/ci.yaml, the
// pipeline file names the Core owns as contract knowledge, ADR-007);
// Content is the pipeline file content the standard supplies (the YAML
// the runtime validates through the pipeline loader before writing).
// The shape is the contract's, stored as-is — the store never
// interprets the content, and the runtime never supplies content of its
// own (TS-015-01-02, ADR-026 decision 1).
type TemplateFile struct {
	// ID is the stable identifier of the template: the pipeline
	// position it generates ("build", "ci" — 005 §5.7). The runtime
	// maps known template ids to the pipeline file names it owns;
	// a template id the runtime has no pipeline position for is a
	// record inconsistency, rejected with an actionable error (C7 —
	// rejected, never patched).
	ID string `json:"id"`

	// Description is an optional human-readable description of the
	// generated pipeline this template produces.
	Description string `json:"description,omitempty"`

	// Content is the pipeline file content the standard supplies, in
	// the pipeline file format (YAML). The runtime validates it
	// through the pipeline loader (execution.ParsePipeline) before
	// writing; a file that fails validation is a broken standard
	// record, rejected with an actionable error — never written and
	// never replaced by runtime content.
	Content string `json:"content"`
}

// TemplateContent is the template content of the installed standard
// release (TS-015-02-03): the framework's pipeline template files under
// the framework's own namespace. It is the standard's content, embedded
// in the installed-standard record and resolved by the generation flow —
// never runtime knowledge (ADR-026 decision 1).
type TemplateContent struct {
	// Namespace is the framework's own namespace for the content: a
	// single dot-free segment (the framework name, e.g. "laravel").
	// The runtime enforces namespace isolation (C6, command-contract
	// §4.5).
	Namespace string `json:"namespace"`

	// Templates are the declared pipeline template files of the
	// standard, at least one per content section: each template
	// carries a stable id and the pipeline file content it generates.
	Templates []TemplateFile `json:"templates"`
}

// TemplateContent resolves the declared framework's template content
// from this installed-standard record (TS-015-02-03). The resolution is
// explicit and fully determined by the record — never by runtime
// framework knowledge:
//
//   - content present (non-nil, non-empty templates) and namespace
//     matching the framework: the content is returned — the pipeline
//     templates come from the installed standard;
//   - no content or an empty templates list: wrapped
//     ErrTemplateContentMissing — the standard declares nothing in the
//     template category (command-contract §4.1); the caller hands off
//     to the interim adapter-driven generation (TS-015-02-03);
//   - content present with a namespace different from the framework:
//     namespace isolation is violated — an actionable error; the record
//     is inconsistent with the standard it belongs to and must be
//     re-established by re-installing the standard.
func (rec InstalledStandardRecord) TemplateContent(framework string) (TemplateContent, error) {
	if rec.Templates == nil || len(rec.Templates.Templates) == 0 {
		return TemplateContent{}, fmt.Errorf(
			"%w: standard %q declares no template content; pipeline templates cannot be generated from the installed standard (a standard may declare nothing in a category — command-contract §4.1)",
			ErrTemplateContentMissing, rec.ID)
	}
	if rec.Templates.Namespace != framework {
		return TemplateContent{}, fmt.Errorf(
			"installed standard %q carries template content for namespace %q, not the declared framework %q — namespace isolation is violated (C6); the installed-standard record is inconsistent with the standard it belongs to; re-install the standard to re-establish the record",
			rec.ID, rec.Templates.Namespace, framework)
	}
	return *rec.Templates, nil
}
