// Compatibility validation at adoption (TS-014-04-01). Per ADR-023 §3,
// adoption-time validation checks the declared contract version and the
// capability declaration (ADR-021 §3.2): a standard that does not declare
// compatibility is rejected (PRD-002 §5.8), and compatibility errors
// surface at install — not in production. Compatibility is declared,
// validated, and recorded, never assumed (Transition Plan A2; ADR-024
// §3.6).
//
// The validation engine in this file is pure: it consumes a metadata
// document (Metadata), the runtime's supported contract major set, and
// the adopting project's framework version, and produces a
// CompatibilityResult record that the validation orchestration (T-012)
// and the install/update flows (T-007/T-008) persist for auditability.
// Wiring into those flows is out of scope here.
//
// Contract-version semantics follow EPIC-013: the contract major version
// is the unit of compatibility (ADR-024 §3.1) — minor and patch releases
// within a contract major are backward compatible and do not change the
// unit. The runtime implements at most two concurrently supported
// contract majors (ADR-024 §3.4); the supported set is recorded in the
// compatibility matrix (docs/specification-corpus/compatibility-matrix.
// json), the reference a standard's declared target contract version is
// checked against. This engine is engine-path-independent: it does not
// read the matrix; the caller supplies the supported set.
//
// Framework-version semantics: the capability declaration's
// frameworkVersion is the set of framework versions the release supports
// (schema: at least one, unique, each major.minor.patch). The declaration
// satisfies the runtime when the adopting project's framework version is
// covered by the declared set. Coverage follows the same semver
// compatibility convention as the contract version: same major — within a
// major, minor and patch releases are backward compatible (ADR-024 §3.1).
//
// Reference: TS-014-04-01, ADR-023 §3, ADR-021 §3.2, ADR-024 §3.1, §3.4,
// PRD-002 §5.8
package registry

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CompatibilityResult records the outcome of adoption-time compatibility
// validation: whether the adoption may proceed (Valid), the per-dimension
// check flags, and the declared values the checks were performed against.
// The record is persisted by the validation orchestration (T-012) and the
// install/update flows (T-007/T-008) for auditability (TS-014-04-01 DoD:
// validation results are recorded; ADR-023 §3; ADR-024 §3.6: declared,
// validated, and recorded — not assumed). The json tags are the
// persistence shape of the record; the registry metadata format is
// untouched — this type is runtime-side, not part of the format.
type CompatibilityResult struct {
	// Valid reports whether the adoption may proceed: every check
	// passed. When false, Errors carries every rejection reason.
	Valid bool `json:"valid"`

	// ContractVersionCompatible reports whether the declared contract
	// version is present, well-formed semver, and targets a supported
	// contract major (ADR-024 §3.1, §3.4).
	ContractVersionCompatible bool `json:"contractVersionCompatible"`

	// CapabilityCompatible reports whether the capability declaration
	// is present and well-formed (at least one unique, well-formed
	// framework version) and, when the project framework version was
	// provided, covers it (ADR-021 §3.2; PRD-002 §5.8).
	CapabilityCompatible bool `json:"capabilityCompatible"`

	// FrameworkVersionChecked reports whether the adopting project's
	// framework version was provided and was actually compared against
	// the declared support scope. False when no project framework
	// version was provided — the capability declaration is then
	// validated for shape only — and when the declared scope is
	// malformed, duplicated, or overflowing: the rejection path returns
	// before any scope comparison. "Checked" therefore never means
	// "assumed compatible": it records that the scope was really
	// compared against the project's framework version.
	FrameworkVersionChecked bool `json:"frameworkVersionChecked"`

	// DeclaredContractVersion is the contract version the metadata
	// document declared — the auditable record of what was declared.
	DeclaredContractVersion string `json:"declaredContractVersion"`

	// DeclaredFrameworkVersions is the framework-version support scope
	// the metadata document declared.
	DeclaredFrameworkVersions []string `json:"declaredFrameworkVersions"`

	// SupportedContractMajors is the runtime's supported contract major
	// set the declaration was checked against.
	SupportedContractMajors []int `json:"supportedContractMajors"`

	// ProjectFrameworkVersion is the adopting project's framework
	// version the declared support scope was checked against. Empty
	// when none was provided.
	ProjectFrameworkVersion string `json:"projectFrameworkVersion,omitempty"`

	// Errors lists every rejection reason found, each actionable: what
	// failed and how to resolve it. Empty when Valid is true.
	Errors []string `json:"errors,omitempty"`
}

// ValidateCompatibility performs adoption-time compatibility validation
// of a registry metadata document (TS-014-04-01):
//
//  1. Contract version check — the document must declare a contract
//     version (PRD-002 §5.8: a standard that does not declare
//     compatibility is rejected); the declared version must be
//     well-formed semver; its major must be in the runtime's supported
//     contract major set (ADR-024 §3.1, §3.4).
//  2. Capability declaration check — the document must declare a
//     framework-version support scope (ADR-021 §3.2); the scope must be
//     non-empty, unique, and well-formed semver (schema:
//     capability.frameworkVersion minItems 1, uniqueItems). When
//     projectFrameworkVersion is provided, it must be covered by the
//     declared scope (same-major compatibility).
//
// Incompatibilities are never errors: they are reported in the result
// with Valid=false and one actionable message per rejection reason, so
// the caller surfaces them at install (ADR-023 §3: compatibility errors
// surface at install, not in production). Malformed declared values are
// rejection reasons, not Go errors — the validation's job is exactly to
// produce the actionable record. The result carries the declared values
// and the checked-against values for auditability; persistence belongs to
// the consuming flows (T-009).
//
// Reference: TS-014-04-01, ADR-023 §3, ADR-024 §3.1, §3.4, PRD-002 §5.8
func ValidateCompatibility(md Metadata, supportedContractMajors []int, projectFrameworkVersion string) CompatibilityResult {
	result := CompatibilityResult{
		DeclaredContractVersion: md.ContractVersion,
		// The record is persisted for auditability; copy the input
		// slices so later caller mutation cannot rewrite what was
		// validated and recorded.
		DeclaredFrameworkVersions: append([]string(nil), md.Capability.FrameworkVersion...),
		SupportedContractMajors:   append([]int(nil), supportedContractMajors...),
		ProjectFrameworkVersion:   projectFrameworkVersion,
	}
	label := md.ID
	if label == "" {
		label = "<standard>"
	}

	checkContractVersion(&result, md.ContractVersion, supportedContractMajors, label)
	checkCapability(&result, md.Capability.FrameworkVersion, projectFrameworkVersion, label)

	result.Valid = result.ContractVersionCompatible && result.CapabilityCompatible
	return result
}

// checkContractVersion validates the declared contract version: present
// (PRD-002 §5.8), well-formed semver without leading zeros (mirroring the
// schema's contractVersion pattern), a representable major (overflow is
// surfaced, never coerced), and targeting a supported contract major
// (ADR-024 §3.1, §3.4). Every failure appends an actionable message to
// result.Errors.
func checkContractVersion(result *CompatibilityResult, declared string, supported []int, label string) {
	if declared == "" {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares no contract version; a standard that does not declare compatibility is rejected (PRD-002 §5.8). Declare the target contract version in the metadata document's contractVersion field.",
			label))
		return
	}

	if !contractVersionPattern.MatchString(declared) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q, which is not well-formed semver (expected major.minor.patch without leading zeros, e.g. \"1.0.0\"). Fix the metadata document's contractVersion field.",
			label, declared))
		return
	}

	major, ok := semverMajor(declared)
	if !ok {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q, whose major overflows the supported range; contract majors are compared numerically and this value cannot be represented. Migrate the standard to a supported contract major.",
			label, declared))
		return
	}

	if len(supported) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q targeting contract major %d, but the runtime declares no supported contract majors; adoption cannot proceed. Declare the supported contract major(s) from the version line (ADR-024 §3.4).",
			label, declared, major))
		return
	}

	if !containsMajor(supported, major) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q targeting contract major %d, which the runtime does not support (supported contract major(s): %s; ADR-024 §3.4). Migrate the standard to a supported contract major.",
			label, declared, major, FormatContractMajors(supported)))
		return
	}

	result.ContractVersionCompatible = true
}

// checkCapability validates the capability declaration: a framework-
// version support scope must be declared (ADR-021 §3.2; PRD-002 §5.8),
// non-empty, unique, well-formed semver with representable majors
// (schema: minItems 1, uniqueItems; overflow is surfaced, never coerced).
// When projectFrameworkVersion is provided, it must be covered by the
// declared scope under same-major semver compatibility (ADR-024 §3.1).
// Every failure appends an actionable message to result.Errors.
func checkCapability(result *CompatibilityResult, declared []string, projectFrameworkVersion, label string) {
	if len(declared) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares no capability declaration; a standard that does not declare compatibility is rejected (PRD-002 §5.8). Declare the framework-version support scope in the metadata document's capability.frameworkVersion field (at least one version).",
			label))
		return
	}

	seen := make(map[string]bool, len(declared))
	for _, version := range declared {
		if !frameworkVersionPattern.MatchString(version) {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"standard %q declares framework version %q, which is not well-formed semver (expected major.minor.patch, e.g. \"5.1.0\"). Fix the capability.frameworkVersion entries.",
				label, version))
			continue
		}
		if _, ok := semverMajor(version); !ok {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"standard %q declares framework version %q, whose major overflows the supported range; framework versions are compared numerically by major and this value cannot be represented. Fix the capability.frameworkVersion entries.",
				label, version))
			continue
		}
		if seen[version] {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"standard %q declares framework version %q more than once; capability.frameworkVersion must be unique. Remove the duplicate entry.",
				label, version))
		}
		seen[version] = true
	}

	// A malformed or duplicated scope is rejected regardless of the
	// project framework version — the rejection messages are already
	// recorded above; the scope check must not compound messages on a
	// broken declaration.
	if !declaredScopeWellFormed(declared) {
		return
	}

	if projectFrameworkVersion == "" {
		result.CapabilityCompatible = true
		return
	}
	result.FrameworkVersionChecked = true

	if !frameworkVersionPattern.MatchString(projectFrameworkVersion) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the project's framework version %q is not well-formed semver (expected major.minor.patch, e.g. \"5.1.0\"); the declared support scope cannot be checked against it. Fix the project's framework version.",
			projectFrameworkVersion))
		return
	}
	if _, ok := semverMajor(projectFrameworkVersion); !ok {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the project's framework version %q has a major that overflows the supported range; it cannot be compared against the declared support scope. Fix the project's framework version.",
			projectFrameworkVersion))
		return
	}

	if !coveredByScope(projectFrameworkVersion, declared) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares framework-version support scope [%s], which does not cover the project's framework version %q (compatibility by major per semver; ADR-024 §3.1). Update capability.frameworkVersion or the project's framework version.",
			label, strings.Join(declared, ", "), projectFrameworkVersion))
		return
	}

	result.CapabilityCompatible = true
}

// declaredScopeWellFormed reports whether every declared framework
// version matches the schema's frameworkVersion pattern, has a
// representable major, and appears once. Malformed, overflowing, or
// duplicated entries have already produced actionable rejection messages;
// this gate keeps the scope check from compounding messages on a broken
// declaration.
func declaredScopeWellFormed(declared []string) bool {
	seen := make(map[string]bool, len(declared))
	for _, version := range declared {
		if !frameworkVersionPattern.MatchString(version) || seen[version] {
			return false
		}
		if _, ok := semverMajor(version); !ok {
			return false
		}
		seen[version] = true
	}
	return true
}

// coveredByScope reports whether the project's framework version is
// covered by the declared support scope: same-major compatibility with at
// least one declared version (ADR-024 §3.1 — within a major, minor and
// patch releases are backward compatible). Both inputs are assumed
// well-formed semver; a major that cannot be represented never covers
// anything (callers surface overflow before this point).
func coveredByScope(projectVersion string, declared []string) bool {
	projectMajor, ok := semverMajor(projectVersion)
	if !ok {
		return false
	}
	for _, version := range declared {
		if major, ok := semverMajor(version); ok && major == projectMajor {
			return true
		}
	}
	return false
}

// contractVersionPattern mirrors the schema's contractVersion pattern
// (registry-metadata.schema.json): semver major.minor.patch without
// leading zeros. The schema is the machine-readable authority (ADR-029
// §3); runtime validation mirrors it exactly so the two cannot drift.
// Coordination: the metadata parse client (TS-014-01-02, parse.go) is
// expected to carry the same shape patterns; when both land on develop
// the PM coordinates sharing them at the validation orchestration (T-012).
var contractVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// frameworkVersionPattern mirrors the schema's capability.frameworkVersion
// item pattern: semver major.minor.patch (the schema allows leading zeros
// on this field; the pattern reproduces the schema text). See the
// coordination note on contractVersionPattern.
var frameworkVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// semverMajor returns the major component of a semver major.minor.patch
// string and whether it is representable as an int. The input must
// already match the caller's pattern; the pattern guarantees the
// component is numeric, but it does not bound the digit count — a
// component with 20+ digits overflows int. Callers surface overflow as
// an actionable rejection instead of coercing to 0: silently treating an
// overflowing major as major 0 could wrongly accept a declaration when 0
// is in the supported set (the schema allows a 0 major).
func semverMajor(version string) (int, bool) {
	parts := strings.SplitN(version, ".", 2)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return major, true
}

// containsMajor reports whether the supported major set contains major.
func containsMajor(supported []int, major int) bool {
	for _, m := range supported {
		if m == major {
			return true
		}
	}
	return false
}
