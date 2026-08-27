// Package platform provides canonical platform identifiers and
// deterministic GOOS-based detection for platform-aware pipeline
// execution (ADR-018).
//
// The package is Core-owned: the pipeline engine (internal/execution)
// consumes it, and the framework standard executables (anvil-standard-*)
// mirror the identifiers for their build target tables and pipeline
// templates. Platform detection originally lived in internal/flutter
// (TS-P7-22) and was relocated here by TS-P7-23 because the Core
// pipeline engine must not depend on adapters (ADR-009 §8.1); the
// Flutter standard carries its own copy of these values since the
// repository split (TS-016-02-01).
//
// Reference: TS-P7-23, ADR-018, ADR-009 §8.1, ADR-025
package platform

import "runtime"

// Canonical platform identifiers (ADR-018 detection values). The values
// are the platform names the pipeline task metadata declares in its
// platforms list and the values Detect returns.
//
// Reference: TS-P7-22 AC-1..AC-3, TS-P7-23, ADR-018
const (
	// PlatformLinux identifies the Linux operating system (GOOS
	// "linux").
	PlatformLinux = "linux"

	// PlatformDarwin identifies macOS (GOOS "darwin"). The Flutter iOS
	// build target is the only target that requires it — iOS builds
	// need Xcode, which exists on macOS only (ADR-018).
	PlatformDarwin = "darwin"

	// PlatformWindows identifies the Windows operating system (GOOS
	// "windows").
	PlatformWindows = "windows"
)

// Detect maps a GOOS value to its canonical platform identifier
// (PlatformLinux, PlatformDarwin, or PlatformWindows). It is a pure
// function — no runtime state — so tests exercise every platform
// deterministically regardless of the host (the "mock platform"
// injection: tests call Detect("darwin") on a Linux host).
//
// Unknown GOOS values are passed through unchanged. The mapping is
// deterministic and lossless: detection never invents a value, and a
// GOOS the platform layer does not know (e.g. "freebsd") still
// identifies the real platform. The pipeline engine treats anything
// outside the known set as unsupported for every target — platform
// matching is plain string equality, so new platforms integrate without
// rework.
//
// Reference: TS-P7-22 AC-1..AC-3, TS-P7-23
func Detect(goos string) string {
	switch goos {
	case PlatformLinux, PlatformDarwin, PlatformWindows:
		return goos
	default:
		return goos
	}
}

// Current returns the platform identifier of the host the process runs
// on, using runtime.GOOS as the detection source. The pipeline engine
// calls it to plan platform-aware build execution (TS-P7-23); tests
// exercise the deterministic mapping through Detect instead of relying
// on the host.
//
// Reference: TS-P7-22 AC-4, TS-P7-23
func Current() string {
	return Detect(runtime.GOOS)
}
