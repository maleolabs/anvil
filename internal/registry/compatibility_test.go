package registry

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// sampleMetadata returns a fully valid metadata document: declared
// contract version 1.0.0 and a framework-version support scope of
// [5.1.0, 5.2.0]. Tests mutate the fields they exercise.
func sampleMetadata() Metadata {
	return Metadata{
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Capability: Capability{
			FrameworkVersion: []string{"5.1.0", "5.2.0"},
		},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
		},
		Lifecycle: Lifecycle{
			State: LifecycleStatePublished,
		},
		Trust: Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: "c2lnbmF0dXJlLXZhbHVl",
				PublicKey: "cHVibGljLWtleS12YWx1ZQ==",
			},
		},
	}
}

// TestValidateCompatibilityAccepted asserts a fully declared, compatible
// standard is accepted at adoption: declared contract version within the
// runtime's supported majors and a capability declaration covering the
// project's framework version (TS-014-04-01 DoD).
func TestValidateCompatibilityAccepted(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "5.2.3")

	if !result.Valid {
		t.Errorf("Valid = false, want true; errors: %v", result.Errors)
	}
	if !result.ContractVersionCompatible {
		t.Error("ContractVersionCompatible = false, want true")
	}
	if !result.CapabilityCompatible {
		t.Error("CapabilityCompatible = false, want true")
	}
	if !result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = false, want true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
}

// TestValidateCompatibilityRejectsMissingContractVersion asserts an
// adoption without a declared contract version is rejected with an
// actionable message (TS-014-04-01 DoD; PRD-002 §5.8: a standard that
// does not declare compatibility is rejected).
func TestValidateCompatibilityRejectsMissingContractVersion(t *testing.T) {
	md := sampleMetadata()
	md.ContractVersion = ""

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.ContractVersionCompatible {
		t.Error("ContractVersionCompatible = true, want false")
	}
	if !hasMessage(result.Errors, "declares no contract version") {
		t.Errorf("Errors = %v, want a missing-contract-version message", result.Errors)
	}
	if !hasMessage(result.Errors, "contractVersion") {
		t.Errorf("Errors = %v, want a resolution hint naming contractVersion", result.Errors)
	}
}

// TestValidateCompatibilityRejectsIncompatibleContractMajor asserts an
// adoption declaring a contract version outside the runtime's supported
// majors is rejected with an actionable message naming both the declared
// and the supported majors (TS-014-04-01 DoD; ADR-024 §3.4).
func TestValidateCompatibilityRejectsIncompatibleContractMajor(t *testing.T) {
	md := sampleMetadata()
	md.ContractVersion = "2.0.0"

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.ContractVersionCompatible {
		t.Error("ContractVersionCompatible = true, want false")
	}
	if !hasMessage(result.Errors, `"2.0.0"`) || !hasMessage(result.Errors, "contract major 2") {
		t.Errorf("Errors = %v, want a message naming the declared version and major", result.Errors)
	}
	if !hasMessage(result.Errors, "[1]") {
		t.Errorf("Errors = %v, want a message naming the supported majors", result.Errors)
	}
	if !hasMessage(result.Errors, "Migrate the standard") {
		t.Errorf("Errors = %v, want a resolution hint", result.Errors)
	}
}

// TestValidateCompatibilityRejectsMalformedContractVersion asserts a
// declared contract version that is not well-formed semver is rejected
// with an actionable message (schema contractVersion pattern mirror).
func TestValidateCompatibilityRejectsMalformedContractVersion(t *testing.T) {
	for _, version := range []string{"1.0", "1", "abc", "01.0.0", "1.2.3.4"} {
		t.Run(version, func(t *testing.T) {
			md := sampleMetadata()
			md.ContractVersion = version

			result := ValidateCompatibility(md, []int{1}, "5.2.3")

			if result.Valid {
				t.Error("Valid = true, want false")
			}
			if result.ContractVersionCompatible {
				t.Error("ContractVersionCompatible = true, want false")
			}
			if !hasMessage(result.Errors, "not well-formed semver") {
				t.Errorf("Errors = %v, want a malformed-semver message", result.Errors)
			}
		})
	}
}

// TestValidateCompatibilityRejectsMissingCapability asserts an adoption
// without a capability declaration is rejected with an actionable message
// (TS-014-04-01 DoD; ADR-021 §3.2; PRD-002 §5.8).
func TestValidateCompatibilityRejectsMissingCapability(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = nil

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !hasMessage(result.Errors, "declares no capability declaration") {
		t.Errorf("Errors = %v, want a missing-capability message", result.Errors)
	}
	if !hasMessage(result.Errors, "capability.frameworkVersion") {
		t.Errorf("Errors = %v, want a resolution hint naming capability.frameworkVersion", result.Errors)
	}
}

// TestValidateCompatibilityRejectsEmptyFrameworkScope asserts a declared
// but empty framework-version support scope is rejected (schema
// capability.frameworkVersion minItems 1; ADR-023 §3).
func TestValidateCompatibilityRejectsEmptyFrameworkScope(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = []string{}

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !hasMessage(result.Errors, "declares no capability declaration") {
		t.Errorf("Errors = %v, want a missing-scope message", result.Errors)
	}
}

// TestValidateCompatibilityRejectsMalformedFrameworkVersion asserts a
// framework-version scope entry that is not well-formed semver is
// rejected with an actionable message (schema frameworkVersion item
// pattern).
func TestValidateCompatibilityRejectsMalformedFrameworkVersion(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = []string{"5.1.0", "5.2"}

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !hasMessage(result.Errors, `"5.2"`) || !hasMessage(result.Errors, "not well-formed semver") {
		t.Errorf("Errors = %v, want a malformed-framework-version message", result.Errors)
	}
}

// TestValidateCompatibilityRejectsDuplicateFrameworkVersion asserts a
// framework-version scope with a duplicate entry is rejected (schema
// uniqueItems).
func TestValidateCompatibilityRejectsDuplicateFrameworkVersion(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = []string{"5.1.0", "5.1.0"}

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !hasMessage(result.Errors, "more than once") || !hasMessage(result.Errors, "unique") {
		t.Errorf("Errors = %v, want a duplicate-entry message", result.Errors)
	}
}

// TestValidateCompatibilityRejectsUncoveredFrameworkVersion asserts an
// adoption whose project framework version is not covered by the declared
// support scope is rejected with an actionable message naming both
// (TS-014-04-01 DoD; ADR-021 §3.2).
func TestValidateCompatibilityRejectsUncoveredFrameworkVersion(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "4.9.0")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = false, want true")
	}
	if !hasMessage(result.Errors, `"4.9.0"`) {
		t.Errorf("Errors = %v, want a message naming the project framework version", result.Errors)
	}
	if !hasMessage(result.Errors, "does not cover") {
		t.Errorf("Errors = %v, want a does-not-cover message", result.Errors)
	}
}

// TestValidateCompatibilityMalformedProjectFrameworkVersion asserts a
// malformed project framework version is reported as an actionable
// rejection rather than silently accepted or misreported as uncovered.
func TestValidateCompatibilityMalformedProjectFrameworkVersion(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "not-a-version")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = false, want true")
	}
	if !hasMessage(result.Errors, "not well-formed semver") {
		t.Errorf("Errors = %v, want a malformed-project-version message", result.Errors)
	}
}

// TestValidateCompatibilityWithoutProjectFrameworkVersion asserts the
// capability declaration is validated for shape only when no project
// framework version is provided: present, non-empty, unique, well-formed
// — the framework-version coverage dimension is recorded as not checked.
func TestValidateCompatibilityWithoutProjectFrameworkVersion(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "")

	if !result.Valid {
		t.Errorf("Valid = false, want true; errors: %v", result.Errors)
	}
	if !result.CapabilityCompatible {
		t.Error("CapabilityCompatible = false, want true")
	}
	if result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = true, want false")
	}
}

// TestValidateCompatibilityMultipleReasons asserts every rejection reason
// is recorded, not just the first: a standard that declares neither a
// contract version nor a capability scope accumulates both actionable
// messages, so the installer can fix the whole declaration at once.
func TestValidateCompatibilityMultipleReasons(t *testing.T) {
	md := sampleMetadata()
	md.ContractVersion = ""
	md.Capability.FrameworkVersion = nil

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if !hasMessage(result.Errors, "declares no contract version") {
		t.Errorf("Errors = %v, want the contract-version reason", result.Errors)
	}
	if !hasMessage(result.Errors, "declares no capability declaration") {
		t.Errorf("Errors = %v, want the capability reason", result.Errors)
	}
}

// TestValidateCompatibilityEmptySupportedMajors asserts adoption is
// rejected when the runtime declares no supported contract majors — a
// runtime misconfiguration surfaced as an actionable reason, never an
// assumed-compatible pass (A2: compatibility is never assumed).
func TestValidateCompatibilityEmptySupportedMajors(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), nil, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.ContractVersionCompatible {
		t.Error("ContractVersionCompatible = true, want false")
	}
	if !hasMessage(result.Errors, "declares no supported contract majors") {
		t.Errorf("Errors = %v, want a no-supported-majors message", result.Errors)
	}
}

// TestValidateCompatibilityMultipleSupportedMajors asserts the deprecation
// window works: with supported majors [1, 2], a standard targeting either
// is accepted, and a standard targeting a major outside the window is
// rejected (ADR-024 §3.4).
func TestValidateCompatibilityMultipleSupportedMajors(t *testing.T) {
	for _, version := range []string{"1.4.2", "2.3.1"} {
		t.Run(version, func(t *testing.T) {
			md := sampleMetadata()
			md.ContractVersion = version

			result := ValidateCompatibility(md, []int{1, 2}, "5.2.3")

			if !result.Valid {
				t.Errorf("Valid = false for supported major; errors: %v", result.Errors)
			}
		})
	}

	md := sampleMetadata()
	md.ContractVersion = "3.0.0"
	result := ValidateCompatibility(md, []int{1, 2}, "5.2.3")
	if result.Valid {
		t.Error("Valid = true for major outside the deprecation window, want false")
	}
	if !hasMessage(result.Errors, "[1, 2]") {
		t.Errorf("Errors = %v, want a message naming the supported majors [1, 2]", result.Errors)
	}
}

// TestValidateCompatibilitySameMajorFrameworkCoverage asserts framework
// version coverage follows the semver compatibility convention: a project
// on a different minor/patch of a declared major is covered, a project on
// a different major is not (ADR-024 §3.1: within a major, minor and patch
// releases are backward compatible).
func TestValidateCompatibilitySameMajorFrameworkCoverage(t *testing.T) {
	t.Run("patch-difference-covered", func(t *testing.T) {
		result := ValidateCompatibility(sampleMetadata(), []int{1}, "5.2.9")
		if !result.Valid {
			t.Errorf("Valid = false for patch difference; errors: %v", result.Errors)
		}
	})
	t.Run("minor-difference-covered", func(t *testing.T) {
		result := ValidateCompatibility(sampleMetadata(), []int{1}, "5.1.0")
		if !result.Valid {
			t.Errorf("Valid = false for minor difference; errors: %v", result.Errors)
		}
	})
	t.Run("major-difference-rejected", func(t *testing.T) {
		result := ValidateCompatibility(sampleMetadata(), []int{1}, "6.0.0")
		if result.Valid {
			t.Error("Valid = true for major difference, want false")
		}
	})
}

// TestValidateCompatibilityRecordsDeclaredValues asserts the result
// carries the declared values and the checked-against values for
// auditability: the record persists what was declared, not an
// interpretation (TS-014-04-01 DoD: validation results are recorded for
// auditability; ADR-024 §3.6).
func TestValidateCompatibilityRecordsDeclaredValues(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "5.2.3")

	if result.DeclaredContractVersion != "1.0.0" {
		t.Errorf("DeclaredContractVersion = %q, want %q", result.DeclaredContractVersion, "1.0.0")
	}
	if !reflect.DeepEqual(result.DeclaredFrameworkVersions, []string{"5.1.0", "5.2.0"}) {
		t.Errorf("DeclaredFrameworkVersions = %v, want [5.1.0 5.2.0]", result.DeclaredFrameworkVersions)
	}
	if !reflect.DeepEqual(result.SupportedContractMajors, []int{1}) {
		t.Errorf("SupportedContractMajors = %v, want [1]", result.SupportedContractMajors)
	}
	if result.ProjectFrameworkVersion != "5.2.3" {
		t.Errorf("ProjectFrameworkVersion = %q, want %q", result.ProjectFrameworkVersion, "5.2.3")
	}

	// The record is persistable: it round-trips through JSON so the
	// state-recording flow (T-009) can persist it without loss.
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded CompatibilityResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Errorf("JSON round trip mismatch:\n got %+v\nwant %+v", decoded, result)
	}
}

// TestValidateCompatibilityRejectsOverflowingContractMajor asserts a
// declared contract version whose major overflows int (20+ digits —
// schema-legal) is rejected honestly with an actionable message instead
// of silently coercing to major 0. Regression: with 0 in the supported
// set, the overflowing declaration must still be rejected — coercion
// would wrongly accept it (reviewer finding 1).
func TestValidateCompatibilityRejectsOverflowingContractMajor(t *testing.T) {
	const overflowing = "99999999999999999999.0.0"

	t.Run("supported-1", func(t *testing.T) {
		md := sampleMetadata()
		md.ContractVersion = overflowing

		result := ValidateCompatibility(md, []int{1}, "5.2.3")

		if result.Valid {
			t.Error("Valid = true, want false")
		}
		if result.ContractVersionCompatible {
			t.Error("ContractVersionCompatible = true, want false")
		}
		if !hasMessage(result.Errors, "overflows the supported range") {
			t.Errorf("Errors = %v, want an overflow message", result.Errors)
		}
	})

	t.Run("zero-in-supported-set", func(t *testing.T) {
		md := sampleMetadata()
		md.ContractVersion = overflowing

		result := ValidateCompatibility(md, []int{0, 1}, "5.2.3")

		if result.Valid {
			t.Error("Valid = true for overflowing major with 0 supported, want false")
		}
		if result.ContractVersionCompatible {
			t.Error("ContractVersionCompatible = true, want false")
		}
		if !hasMessage(result.Errors, "overflows the supported range") {
			t.Errorf("Errors = %v, want an overflow message, not a silent coercion", result.Errors)
		}
	})
}

// TestValidateCompatibilityRejectsOverflowingFrameworkVersion asserts an
// overflowing major in the declared framework-version scope is rejected
// with an actionable message and the scope is never compared against the
// project's framework version (early-return path).
func TestValidateCompatibilityRejectsOverflowingFrameworkVersion(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = []string{"99999999999999999999.0.0"}

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = true, want false — the overflowing scope is never compared")
	}
	if !hasMessage(result.Errors, "overflows the supported range") {
		t.Errorf("Errors = %v, want an overflow message", result.Errors)
	}
}

// TestValidateCompatibilityRejectsOverflowingProjectFrameworkVersion
// asserts an overflowing project framework version major is rejected with
// an actionable message; the check is recorded as performed but not
// satisfied.
func TestValidateCompatibilityRejectsOverflowingProjectFrameworkVersion(t *testing.T) {
	result := ValidateCompatibility(sampleMetadata(), []int{1}, "99999999999999999999.0.0")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !result.FrameworkVersionChecked {
		t.Error("FrameworkVersionChecked = false, want true")
	}
	if !hasMessage(result.Errors, "overflows the supported range") {
		t.Errorf("Errors = %v, want an overflow message", result.Errors)
	}
}

// TestValidateCompatibilityMixedMalformedDuplicateScope asserts a scope
// with both a malformed entry and a duplicate entry reports every reason
// once, and the early-return gate keeps the coverage check from
// compounding a "does not cover" message on top of the broken
// declaration (reviewer finding 7c).
func TestValidateCompatibilityMixedMalformedDuplicateScope(t *testing.T) {
	md := sampleMetadata()
	md.Capability.FrameworkVersion = []string{"5.1.0", "5.1", "5.1.0"}

	result := ValidateCompatibility(md, []int{1}, "5.2.3")

	if result.Valid {
		t.Error("Valid = true, want false")
	}
	if result.CapabilityCompatible {
		t.Error("CapabilityCompatible = true, want false")
	}
	if !hasMessage(result.Errors, `"5.1"`) || !hasMessage(result.Errors, "not well-formed semver") {
		t.Errorf("Errors = %v, want the malformed-entry message", result.Errors)
	}
	if !hasMessage(result.Errors, "more than once") {
		t.Errorf("Errors = %v, want the duplicate-entry message", result.Errors)
	}
	if hasMessage(result.Errors, "does not cover") {
		t.Errorf("Errors = %v, want no coverage message on a broken declaration", result.Errors)
	}
}

// TestValidateCompatibilityRejectedResultJSONRoundTrip asserts a REJECTED
// result — the most commonly persisted case — round-trips through JSON
// with Errors populated, so the state-recording flow (T-009) persists the
// actionable reasons losslessly (reviewer finding 7b).
func TestValidateCompatibilityRejectedResultJSONRoundTrip(t *testing.T) {
	md := sampleMetadata()
	md.ContractVersion = "2.0.0"

	result := ValidateCompatibility(md, []int{1}, "6.0.0")

	if result.Valid {
		t.Fatal("test fixture must produce a rejected result")
	}
	if len(result.Errors) == 0 {
		t.Fatal("test fixture must populate Errors")
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(raw), `"errors"`) {
		t.Error("marshaled rejected result does not carry the errors field")
	}
	var decoded CompatibilityResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Errorf("JSON round trip mismatch:\n got %+v\nwant %+v", decoded, result)
	}
}

// TestValidateCompatibilityRecordStableAfterInputMutation asserts the
// audit record owns its slices: mutating the metadata document or the
// supported-majors input after validation cannot rewrite the recorded
// declared values (reviewer finding 5).
func TestValidateCompatibilityRecordStableAfterInputMutation(t *testing.T) {
	md := sampleMetadata()
	supported := []int{1, 2}

	result := ValidateCompatibility(md, supported, "5.2.3")

	md.Capability.FrameworkVersion[0] = "9.9.9"
	md.Capability.FrameworkVersion = append(md.Capability.FrameworkVersion, "9.9.9")
	supported[0] = 99
	supported = append(supported, 99)

	if !reflect.DeepEqual(result.DeclaredFrameworkVersions, []string{"5.1.0", "5.2.0"}) {
		t.Errorf("DeclaredFrameworkVersions mutated after validation: %v", result.DeclaredFrameworkVersions)
	}
	if !reflect.DeepEqual(result.SupportedContractMajors, []int{1, 2}) {
		t.Errorf("SupportedContractMajors mutated after validation: %v", result.SupportedContractMajors)
	}
	if !result.Valid {
		t.Errorf("Valid = false after input mutation; errors: %v", result.Errors)
	}
}

// hasMessage reports whether any message in the list contains the
// expected substring.
func hasMessage(messages []string, expected string) bool {
	for _, message := range messages {
		if strings.Contains(message, expected) {
			return true
		}
	}
	return false
}
