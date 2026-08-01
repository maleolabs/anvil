// Compatibility validation for discovered adapters (TS-P7-06). The
// validation engine checks an adapter against the installed Core version
// and the project's framework version before the adapter is allowed to
// participate in operations (ADR-009 §7.2).
package adapter

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrMalformedVersion is returned by Validate when a version string or a
// declared version constraint is not a valid MAJOR.MINOR.PATCH SemVer
// string. Incompatibilities are never errors — they are reported in the
// result with Compatible=false.
//
// Reference: TS-P7-06 AC-3
var ErrMalformedVersion = errors.New("adapter: malformed version")

// semverPattern matches a basic MAJOR.MINOR.PATCH SemVer 2.0.0 version.
// It mirrors internal/config/validation.go — no semver dependency is
// introduced.
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// CompatibilityResult reports whether an adapter is compatible with the
// installed Core version and the project's framework version.
//
// Reference: TS-P7-06 AC-1..AC-5
type CompatibilityResult struct {
	// Compatible reports whether every dimension is compatible. When
	// false, the adapter is blocked from participation.
	Compatible bool

	// CoreVersionCompatible reports whether the Core version falls
	// within the adapter's declared Core version range. True when the
	// adapter declares no Core constraint.
	CoreVersionCompatible bool

	// FrameworkVersionCompatible reports whether the framework version
	// falls within the adapter's declared framework version range. True
	// when the adapter declares no framework constraint.
	FrameworkVersionCompatible bool

	// Errors describes every incompatibility found, each as a clear,
	// actionable message.
	Errors []string
}

// Validate checks the adapter against the installed Core version and the
// project's framework version:
//
//  1. Core version check — when the adapter declares a Core constraint,
//     coreVersion is compared against it; a malformed coreVersion returns
//     an error wrapping ErrMalformedVersion.
//  2. Framework version check — when the adapter declares a framework
//     constraint AND frameworkVersion is non-empty, it is compared; an
//     empty frameworkVersion with declared constraints is reported as not
//     provided (incompatible, clear message). With no constraints
//     declared, the dimension is compatible.
//  3. On success (both compatible), the lifecycle advances from
//     StageDiscovered to StageReady (two Advance calls) — validation
//     advances the lifecycle state (TS-P7-06 AC-6).
//  4. On failure the lifecycle stays at its current stage and the adapter
//     is blocked from participation; Errors contains descriptive messages
//     and Compatible=false.
//
// A typed error is returned only for malformed version input (wrapping
// ErrMalformedVersion) or invalid lifecycle state; incompatibilities
// return a result with Compatible=false and a nil error.
//
// Reference: TS-P7-06 AC-1..AC-6
func Validate(a AdapterInfo, lifecycle *Lifecycle, coreVersion, frameworkVersion string) (CompatibilityResult, error) {
	if lifecycle == nil {
		return CompatibilityResult{}, errors.New("cannot validate adapter: lifecycle must not be nil")
	}

	result := CompatibilityResult{
		CoreVersionCompatible:      true,
		FrameworkVersionCompatible: true,
	}

	// 1. Core version check — only when the adapter declares a constraint.
	if a.CoreVersion.Min != "" || a.CoreVersion.Max != "" {
		ok, err := versionInRange(coreVersion, a.CoreVersion)
		if err != nil {
			return result, fmt.Errorf("%w: core version: %v", ErrMalformedVersion, err)
		}
		result.CoreVersionCompatible = ok
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"adapter %q requires Core version in range [%s, %s], got %q",
				a.Framework, a.CoreVersion.Min, a.CoreVersion.Max, coreVersion))
		}
	}

	// 2. Framework version check — only when the adapter declares a
	// constraint. A missing framework version is a reported
	// incompatibility, not an error.
	if a.FrameworkVersion.Min != "" || a.FrameworkVersion.Max != "" {
		if frameworkVersion == "" {
			result.FrameworkVersionCompatible = false
			result.Errors = append(result.Errors, fmt.Sprintf(
				"adapter %q requires a framework version in range [%s, %s], but no framework version was provided",
				a.Framework, a.FrameworkVersion.Min, a.FrameworkVersion.Max))
		} else {
			ok, err := versionInRange(frameworkVersion, a.FrameworkVersion)
			if err != nil {
				return result, fmt.Errorf("%w: framework version: %v", ErrMalformedVersion, err)
			}
			result.FrameworkVersionCompatible = ok
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"adapter %q requires framework version in range [%s, %s], got %q",
					a.Framework, a.FrameworkVersion.Min, a.FrameworkVersion.Max, frameworkVersion))
			}
		}
	}

	// 3. Both dimensions compatible: validation advances the lifecycle
	// from Discovered to Ready (TS-P7-06 AC-6).
	if result.CoreVersionCompatible && result.FrameworkVersionCompatible {
		result.Compatible = true

		if lifecycle.Stage() != StageDiscovered {
			return result, fmt.Errorf("cannot validate adapter %q: lifecycle must be in stage %q, got %q",
				a.Framework, StageDiscovered, lifecycle.Stage())
		}
		if err := lifecycle.Advance(); err != nil {
			return result, fmt.Errorf("advancing lifecycle of adapter %q to %q: %w", a.Framework, StageValidated, err)
		}
		if err := lifecycle.Advance(); err != nil {
			return result, fmt.Errorf("advancing lifecycle of adapter %q to %q: %w", a.Framework, StageReady, err)
		}
	}

	// 4. Incompatibility: the lifecycle stays at its current stage and
	// the adapter is blocked from participation.
	return result, nil
}

// versionInRange reports whether version falls within the inclusive range
// declared by r. When r declares no bounds the version is always in
// range. Malformed versions, malformed constraint bounds, and inverted
// ranges (Min greater than Max) return a descriptive error.
func versionInRange(version string, r VersionRange) (bool, error) {
	if r.Min == "" && r.Max == "" {
		return true, nil
	}
	if r.Min != "" && r.Max != "" {
		cmp, err := compareSemver(r.Min, r.Max)
		if err != nil {
			return false, err
		}
		if cmp > 0 {
			return false, fmt.Errorf("declared version range [%s, %s] is inverted: min must not exceed max", r.Min, r.Max)
		}
	}
	if r.Min != "" {
		cmp, err := compareSemver(version, r.Min)
		if err != nil {
			return false, err
		}
		if cmp < 0 {
			return false, nil
		}
	}
	if r.Max != "" {
		cmp, err := compareSemver(version, r.Max)
		if err != nil {
			return false, err
		}
		if cmp > 0 {
			return false, nil
		}
	}
	return true, nil
}

// compareSemver compares two MAJOR.MINOR.PATCH SemVer strings numerically,
// returning -1, 0, or 1. It returns a descriptive error for strings that
// do not match the basic SemVer format. The regex guarantees each part is
// numeric; Atoi errors are handled explicitly to cover components that
// overflow int (more than 19 digits).
func compareSemver(a, b string) (int, error) {
	if !semverPattern.MatchString(a) {
		return 0, fmt.Errorf("version %q is not valid SemVer (expected MAJOR.MINOR.PATCH, e.g. \"1.2.3\")", a)
	}
	if !semverPattern.MatchString(b) {
		return 0, fmt.Errorf("version %q is not valid SemVer (expected MAJOR.MINOR.PATCH, e.g. \"1.2.3\")", b)
	}

	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		na, err := strconv.Atoi(pa[i])
		if err != nil {
			return 0, fmt.Errorf("version %q is not valid SemVer: component %q overflows int", a, pa[i])
		}
		nb, err := strconv.Atoi(pb[i])
		if err != nil {
			return 0, fmt.Errorf("version %q is not valid SemVer: component %q overflows int", b, pb[i])
		}
		if na < nb {
			return -1, nil
		}
		if na > nb {
			return 1, nil
		}
	}
	return 0, nil
}
