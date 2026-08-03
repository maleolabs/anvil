// Tests for the Core platform detection (TS-P7-23): the deterministic
// GOOS-to-platform mapping for linux, darwin, and windows, the
// deterministic handling of unknown GOOS values, and the Current
// wrapper that reads runtime.GOOS. The pure Detect function is the
// mockable surface — tests exercise every platform regardless of the
// host.
//
// The behavior is relocated from internal/flutter (TS-P7-22) unchanged:
// the pipeline engine (internal/execution) consumes this package
// because Core must not depend on adapters (ADR-009 §8.1).
package platform

import (
	"runtime"
	"testing"
)

// TestDetect_AllPlatforms verifies the canonical platform identifiers
// for the three known GOOS values (TS-P7-22 AC-1..AC-3, ADR-018). The
// mapping is exercised for every platform on any host — the "mock
// platform" injection — so CI on Linux covers darwin and windows too.
func TestDetect_AllPlatforms(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{goos: "linux", want: PlatformLinux},
		{goos: "darwin", want: PlatformDarwin},
		{goos: "windows", want: PlatformWindows},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := Detect(tt.goos); got != tt.want {
				t.Errorf("Detect(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

// TestDetect_UnknownGOOS verifies the deterministic handling of unknown
// GOOS values: they pass through unchanged. Detection is deterministic
// and lossless — it never invents a value, and the pipeline engine
// treats anything outside the known set as unsupported for every target
// (TS-P7-22, ADR-018).
func TestDetect_UnknownGOOS(t *testing.T) {
	for _, goos := range []string{"freebsd", "openbsd", "android", ""} {
		if got := Detect(goos); got != goos {
			t.Errorf("Detect(%q) = %q, want the raw value %q", goos, got, goos)
		}
	}
}

// TestDetect_IsDeterministic verifies the mapping is a pure function:
// repeated calls return the same result and no runtime state influences
// the outcome (TS-P7-22).
func TestDetect_IsDeterministic(t *testing.T) {
	for i := 0; i < 3; i++ {
		if got, want := Detect("darwin"), PlatformDarwin; got != want {
			t.Fatalf("Detect(\"darwin\") = %q, want %q (iteration %d)", got, want, i)
		}
	}
}

// TestCurrent_UsesRuntimeGOOS verifies the Current wrapper reads
// runtime.GOOS through Detect: on the test host it must equal
// runtime.GOOS and be one of the canonical identifiers. This is the
// public API the pipeline engine consumes for platform-aware execution
// (TS-P7-22 AC-4, TS-P7-23).
func TestCurrent_UsesRuntimeGOOS(t *testing.T) {
	got := Current()
	want := Detect(runtime.GOOS)
	if got != want {
		t.Errorf("Current() = %q, want Detect(runtime.GOOS) = %q", got, want)
	}
	switch got {
	case PlatformLinux, PlatformDarwin, PlatformWindows:
		// Known platform — the pipeline engine can match it against
		// build target metadata.
	default:
		t.Errorf("Current() = %q, want a canonical platform identifier", got)
	}
}
