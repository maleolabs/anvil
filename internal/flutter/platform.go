// Platform detection of the Flutter adapter (TS-P7-22).
//
// The canonical platform identifiers (PlatformLinux, PlatformDarwin,
// PlatformWindows) and the deterministic GOOS detection (Detect/Current)
// moved to the Core-owned internal/platform package in TS-P7-23, because
// the Core pipeline engine (internal/execution) consumes them for
// platform-aware build execution and Core must not depend on adapters
// (ADR-009 §8.1). This file re-exports them so existing Flutter
// consumers (build target table, adapter tests) keep compiling
// unchanged.
//
// Reference: TS-P7-22, TS-P7-23, ADR-018, ADR-009 §8.1
package flutter

import anvilplatform "maleolabs.com/anvil/internal/platform"

// PlatformLinux identifies the Linux operating system (GOOS "linux").
// Re-exported from internal/platform (TS-P7-23 relocation).
//
// Reference: TS-P7-22 AC-1, TS-P7-23, ADR-018
const PlatformLinux = anvilplatform.PlatformLinux

// PlatformDarwin identifies macOS (GOOS "darwin"). The Flutter iOS
// build target is the only target that requires it — iOS builds
// need Xcode, which exists on macOS only (ADR-018).
// Re-exported from internal/platform (TS-P7-23 relocation).
//
// Reference: TS-P7-22 AC-2, TS-P7-23, ADR-018
const PlatformDarwin = anvilplatform.PlatformDarwin

// PlatformWindows identifies the Windows operating system (GOOS
// "windows"). Re-exported from internal/platform (TS-P7-23 relocation).
//
// Reference: TS-P7-22 AC-3, TS-P7-23, ADR-018
const PlatformWindows = anvilplatform.PlatformWindows

// Detect maps a GOOS value to its canonical platform identifier
// (PlatformLinux, PlatformDarwin, or PlatformWindows). It is a pure
// function — no runtime state — so tests exercise every platform
// deterministically regardless of the host.
//
// Unknown GOOS values are passed through unchanged. The pipeline engine
// treats anything outside the known set as unsupported for every target.
//
// Re-exported from internal/platform (TS-P7-23 relocation); the
// implementation and semantics live in the Core-owned package so the
// pipeline engine can consume detection without importing the adapter
// (ADR-009 §8.1).
//
// Reference: TS-P7-22 AC-1..AC-3, TS-P7-23
func Detect(goos string) string {
	return anvilplatform.Detect(goos)
}

// Current returns the platform identifier of the host the adapter runs
// on, using runtime.GOOS as the detection source. The pipeline engine
// calls it to plan platform-aware build execution (TS-P7-23).
//
// Re-exported from internal/platform (TS-P7-23 relocation).
//
// Reference: TS-P7-22 AC-4, TS-P7-23
func Current() string {
	return anvilplatform.Current()
}
