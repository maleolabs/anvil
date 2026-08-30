// Configuration extension enforcement (TS-015-03-02).
//
// The enforcing side of standard-driven configuration extension: the
// installed standard's configuration extension content (EPIC-013 config
// extension contract) is converted into the validation engine's rule
// shape (internal/config) and enforced against the resolved project
// configuration. The runtime owns no framework config rules or values
// (TS-015-01-03, ADR-026 decision 1); the installed-standard record is
// the authoritative local record of what is installed (TS-014-03-03), and
// the standard's content is the only source of rules this component
// enforces (ADR-026 decision 2).
//
// The contract declares no value types
// (command-contract.schema.json configKeyDeclaration: "value types are
// deliberately not extended"), so the validation surface derives from
// what the contract DOES carry: the required declaration and the
// string-only shape of declared keys. The runtime enforces those rules
// and never interprets framework-specific values (C6 — the standard
// validates its own extended values beyond the contract's surface).
//
// Enforcement outcomes (explicit, never silent):
//
//   - content resolved: the declared rules are enforced against the
//     resolved configuration — validation errors identify the offending
//     fully-qualified key (framework.<namespace>.<name>, ADR-005 §4.4)
//     and the expected format;
//   - the record carries no content: a standard may declare nothing in a
//     category (command-contract §4.1) — there are no rules to enforce
//     and the framework section passes through (the same no-content
//     hand-off TS-015-03-01 established; the init-time warning lives
//     there, enforcement here enforces only what the standard declares);
//   - the standard is not installed: wrapped ErrStandardNotInstalled —
//     the declaration cannot be validated without the standard (ADR-026
//     decision 3 hard-fail semantics, surfaced by the caller);
//   - a namespace violation inside the record: actionable error — the
//     record is inconsistent with the standard it belongs to.
//
// Reference: TS-015-03-02, ADR-026 decisions 2-3, EPIC-013,
// command-contract §4.1, §4.5, ADR-005 §4.4
package registry

import (
	"errors"

	"maleolabs.com/anvil/internal/config"
)

// FrameworkConfigRules converts the configuration extension content into
// the validation engine's rule shape (internal/config
// FrameworkConfigRule): every declared key becomes a rule qualified under
// the framework's own namespace — the fully-qualified form is
// framework.<namespace>.<name> (ADR-005 §4.4) — carrying the contract's
// required declaration. The conversion is mechanical and loses no
// contract information needed for enforcement: the contract declares no
// value types, so the rule shape carries exactly what the contract
// carries (name, namespace, required).
func (c ConfigExtensionContent) FrameworkConfigRules() []config.FrameworkConfigRule {
	rules := make([]config.FrameworkConfigRule, 0, len(c.Keys))
	for _, key := range c.Keys {
		rules = append(rules, config.FrameworkConfigRule{
			Key:      config.FrameworkConfigKey(c.Namespace, key.Name),
			Required: key.Required,
		})
	}
	return rules
}

// ValidateFrameworkConfig validates the resolved configuration's
// framework section against the installed standard's declared config
// extension rules (TS-015-03-02). The enforcement is fully determined by
// the installed-standard record — never by runtime framework knowledge
// (ADR-026 decision 2):
//
//   - content resolved: the standard's rules are enforced via the
//     validation engine (internal/config ValidateFrameworkRules) — the
//     returned errors identify the offending fully-qualified key and the
//     expected format;
//   - no content: the standard declares nothing in the config extension
//     category (command-contract §4.1) — no rules, no errors; the
//     framework section passes through (nothing to enforce);
//   - no installed standard: wrapped ErrStandardNotInstalled — the
//     declaration cannot be validated without the standard; the caller
//     hard-fails with actionable remediation (ADR-026 decision 3);
//   - store failures and namespace violations pass through — real
//     failures, never a silent pass-through.
func (s *InstalledStandardStore) ValidateFrameworkConfig(framework string, resolved map[string]interface{}) ([]config.ValidationError, error) {
	content, err := s.ResolveConfigExtension(framework)
	if err != nil {
		if errors.Is(err, ErrConfigExtensionMissing) {
			// A standard may declare nothing in a category
			// (command-contract §4.1): no content, no rules to enforce —
			// the framework section passes through.
			return nil, nil
		}
		return nil, err
	}
	return config.ValidateFrameworkRules(content.FrameworkConfigRules(), resolved), nil
}
