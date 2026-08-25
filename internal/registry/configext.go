// Configuration extension content resolution (TS-015-03-01).
//
// Per ADR-026 decision 2, configuration extension is standard-driven:
// framework configuration keys and their defaults are supplied by the
// installed delivery lifecycle standard's configuration extension content
// and consumed by the Anvil Runtime (Core) through the specification's
// config extension contract (EPIC-013). The runtime owns no framework
// config keys or defaults (TS-015-01-03, ADR-026 decision 1); the
// installed-standard record is the authoritative local record of what is
// installed (TS-014-03-03), and the standard's config extension content
// is part of that record.
//
// The content shape mirrors the EPIC-013 config extension declaration
// (command-contract.schema.json — the machine-readable authority, ADR-029
// §3): an extension declares a single dot-free namespace plus at least
// one key; each key carries a name within the namespace, a description, an
// optional string default, and an optional required flag. The runtime
// qualifies keys under the framework's own namespace — the fully-qualified
// form is framework.<namespace>.<name> (ADR-005 §4.4, preserved by the
// contract) — and enforces namespace isolation; it never interprets
// framework-specific values (C6, command-contract §4.5).
//
// Resolution semantics (explicit, never invented):
//
//   - the record carries content whose namespace matches the declared
//     framework: the content resolves — keys and defaults come from the
//     installed standard;
//   - the record carries no content: a standard may declare nothing in a
//     category (command-contract §4.1) — the distinguishable no-content
//     outcome (ErrConfigExtensionMissing) hands off to the same
//     hand-off/warning pattern T-004 established for a missing standard
//     (TS-015-02-02 implements the hard-fail later; this component never
//     hard-fails and never degrades silently);
//   - the content's namespace does not match the declared framework:
//     namespace isolation is violated — the record is inconsistent with
//     the standard it belongs to, an actionable error (reinstall the
//     standard), never a silent pass-through;
//   - the standard is not installed: the resolution passes through the
//     no-match hand-off of ResolveFrameworkStandard
//     (ErrStandardNotInstalled).
//
// Reference: TS-015-03-01, ADR-026 decision 2, EPIC-013, ADR-005 §4.4,
// ADR-021 §3.1, command-contract §4.5
package registry

import (
	"errors"
	"fmt"
)

// ErrConfigExtensionMissing reports that the resolved installed standard
// declares no configuration extension content: a standard may declare
// nothing in a category (command-contract §4.1), so this is a
// distinguishable no-content outcome, not a failure of the store. It is
// the hand-off signal for the missing-extension handling (TS-015-03-01):
// the caller decides how the outcome is surfaced — following the same
// hand-off/warning pattern T-004 established for ErrStandardNotInstalled
// (TS-015-02-02 implements the hard-fail later; this component only makes
// the outcome explicit and never degrades silently).
var ErrConfigExtensionMissing = errors.New("installed standard declares no configuration extension content")

// ConfigExtensionKey is one declared framework configuration key of the
// installed standard's configuration extension content (EPIC-013 config
// extension contract, command-contract.schema.json configKeyDeclaration):
// Name is the key within the extension's namespace; the fully-qualified
// configuration key is framework.<namespace>.<name> (ADR-005 §4.4). The
// shape is the contract's, stored as-is — the store never interprets the
// content, and the runtime never validates framework-specific values
// (C6: the standard validates its own extended values).
type ConfigExtensionKey struct {
	// Name is the key name within the extension's namespace (kebab-case,
	// e.g. "version" — fully qualified: "framework.laravel.version").
	Name string `json:"name"`

	// Description explains what the key configures.
	Description string `json:"description"`

	// Default is the key's default value, when the standard declares one.
	// String-only by contract design (command-contract.schema.json
	// configKeyDeclaration.default: value types are deliberately not
	// extended). Empty when the standard declares no default.
	Default string `json:"default,omitempty"`

	// Required reports whether the key must be provided (a validation
	// rule the standard enforces on its own extended values, C6).
	Required bool `json:"required,omitempty"`
}

// ConfigExtensionContent is the configuration extension content of the
// installed standard release (EPIC-013 config extension contract): the
// framework's configuration keys and their defaults under the framework's
// own namespace. It is the standard's content, embedded in the
// installed-standard record and resolved by TS-015-03-01 — never runtime
// knowledge (ADR-026 decision 1).
type ConfigExtensionContent struct {
	// Namespace is the framework's own namespace for the extension: a
	// single dot-free segment (the framework name, e.g. "laravel").
	// Extended configuration lives under this namespace; the runtime
	// enforces namespace isolation (C6, command-contract §4.5).
	Namespace string `json:"namespace"`

	// Keys are the declared framework-specific configuration keys, at
	// least one per extension (command-contract.schema.json
	// configExtensionDeclaration.keys minItems 1).
	Keys []ConfigExtensionKey `json:"keys"`
}

// ConfigExtensionContent resolves the declared framework's configuration
// extension content from this installed-standard record (TS-015-03-01).
// The resolution is explicit and fully determined by the record — never
// by runtime framework knowledge:
//
//   - content present and namespace matching the framework: the content is
//     returned — framework config keys and defaults come from the
//     installed standard;
//   - no content: wrapped ErrConfigExtensionMissing — the standard
//     declares nothing in the config extension category (command-contract
//     §4.1); the caller hands off to the missing-extension handling
//     following the T-004 warning pattern;
//   - content present with a namespace different from the framework:
//     namespace isolation is violated — an actionable error; the record is
//     inconsistent with the standard it belongs to and must be
//     re-established by re-installing the standard.
func (rec InstalledStandardRecord) ConfigExtensionContent(framework string) (ConfigExtensionContent, error) {
	if rec.ConfigExtension == nil {
		return ConfigExtensionContent{}, fmt.Errorf(
			"%w: standard %q declares no configuration extension content; framework config keys and defaults cannot be resolved from the installed standard (a standard may declare nothing in a category — command-contract §4.1)",
			ErrConfigExtensionMissing, rec.ID)
	}
	if rec.ConfigExtension.Namespace != framework {
		return ConfigExtensionContent{}, fmt.Errorf(
			"installed standard %q carries configuration extension content for namespace %q, not the declared framework %q — namespace isolation is violated (C6); the installed-standard record is inconsistent with the standard it belongs to; re-install the standard to re-establish the record",
			rec.ID, rec.ConfigExtension.Namespace, framework)
	}
	return *rec.ConfigExtension, nil
}

// ResolveConfigExtension resolves a declared framework name against the
// installed-standard record store and returns the installed standard's
// configuration extension content (TS-015-03-01): the standard id follows
// the identity convention (StandardIDForFramework), the standard resolves
// through the installed-standard records
// (ResolveFrameworkStandard), and the content is the record's embedded
// configuration extension section. Resolution is explicit and recorded —
// framework config keys and defaults come from the installed standard,
// never from runtime knowledge (ADR-026 decision 2).
//
// Outcomes:
//
//   - installed standard with content: the content is returned — keys and
//     defaults resolve from the standard;
//   - installed standard without content: wrapped ErrConfigExtensionMissing
//     — the hand-off to the missing-extension handling (the same
//     hand-off/warning pattern T-004 established; the hard-fail of
//     TS-015-02-02 is not implemented here);
//   - no installed standard: wrapped ErrStandardNotInstalled — the
//     no-match hand-off of TS-015-02-02;
//   - the store cannot answer: store failures pass through unwrapped in
//     intent (corrupt record, unreadable store, unsafe standard id), and a
//     namespace violation inside the record is an actionable error.
func (s *InstalledStandardStore) ResolveConfigExtension(framework string) (ConfigExtensionContent, error) {
	rec, err := s.ResolveFrameworkStandard(framework)
	if err != nil {
		return ConfigExtensionContent{}, err
	}
	return rec.ConfigExtensionContent(framework)
}
