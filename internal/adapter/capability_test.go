// Tests for the Core-side capability registry (TS-P7-07).
package adapter

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// laravelCapabilities returns the Laravel capability declaration used
// across registry tests. The Laravel adapter appears in tests only as a
// registered example — the Core engine itself is framework-agnostic
// (ADR-009 §9.6).
func laravelCapabilities() contracts.CapabilityDeclaration {
	return contracts.CapabilityDeclaration{
		ActivationPhases: []string{"migrate", "config_cache"},
		VerificationChecks: []contracts.VerificationCheck{
			{Name: "vendor_present", Description: "validates that the vendor directory exists in the artifact"},
			{Name: "artisan_ok", Description: "validates that artisan boots"},
		},
		DiagnosticCommands: []string{"routes:list", "config:show"},
	}
}

// TestCapabilityRegistry_RegisterAndQuery verifies that a valid
// declaration registers successfully and is retrievable with all three
// categories intact.
//
// Reference: TS-P7-07 AC-1, AC-2, AC-3
func TestCapabilityRegistry_RegisterAndQuery(t *testing.T) {
	r := NewCapabilityRegistry()

	if err := r.Register("laravel", laravelCapabilities()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	decl, ok := r.Capabilities("laravel")
	if !ok {
		t.Fatal("Capabilities(\"laravel\") = ok=false, want ok=true")
	}
	if !reflect.DeepEqual(decl, laravelCapabilities()) {
		t.Errorf("Capabilities(\"laravel\") mismatch:\n got: %#v\nwant: %#v", decl, laravelCapabilities())
	}
}

// TestCapabilityRegistry_RegisterEmptyDeclarationAllowed verifies that an
// empty declaration registers fine and queries back with ok=true — an
// adapter that declares no capabilities is handled gracefully.
//
// Reference: TS-P7-07 AC-4
func TestCapabilityRegistry_RegisterEmptyDeclarationAllowed(t *testing.T) {
	r := NewCapabilityRegistry()

	if err := r.Register("laravel", contracts.CapabilityDeclaration{}); err != nil {
		t.Fatalf("Register(empty declaration) failed: %v", err)
	}

	decl, ok := r.Capabilities("laravel")
	if !ok {
		t.Fatal("Capabilities(\"laravel\") = ok=false, want ok=true")
	}
	if len(decl.ActivationPhases) != 0 || len(decl.VerificationChecks) != 0 || len(decl.DiagnosticCommands) != 0 {
		t.Errorf("Capabilities(\"laravel\") = %#v, want empty declaration", decl)
	}
}

// TestCapabilityRegistry_RegisterInvalidFrameworkRejected verifies that
// an empty, dotted, or spaced framework name is rejected — the framework
// must be a single dot-free, space-free segment.
//
// Reference: TS-P7-07 AC-1
func TestCapabilityRegistry_RegisterInvalidFrameworkRejected(t *testing.T) {
	r := NewCapabilityRegistry()

	tests := []struct {
		name      string
		framework string
	}{
		{name: "empty", framework: ""},
		{name: "dotted", framework: "laravel.core"},
		{name: "spaced", framework: "laravel php"},
		{name: "trailing_dot", framework: "laravel."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.Register(tt.framework, laravelCapabilities()); err == nil {
				t.Fatalf("Register with framework %q succeeded, want error", tt.framework)
			}
		})
	}
}

// TestCapabilityRegistry_RegisterDuplicateFrameworkRejected verifies that
// a second declaration for the same framework is rejected — one adapter
// per framework (ADR-009 §9.9).
//
// Reference: TS-P7-07 AC-3
func TestCapabilityRegistry_RegisterDuplicateFrameworkRejected(t *testing.T) {
	r := NewCapabilityRegistry()
	if err := r.Register("laravel", laravelCapabilities()); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	other := laravelCapabilities()
	other.ActivationPhases = []string{"cache_clear"}
	if err := r.Register("laravel", other); err == nil {
		t.Fatal("duplicate Register succeeded, want error")
	}
}

// TestCapabilityRegistry_RegisterEmptyNamesRejected verifies that empty
// phase, check, and command names are rejected, even when the rest of
// the declaration is valid.
//
// Reference: TS-P7-07 AC-2
func TestCapabilityRegistry_RegisterEmptyNamesRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(decl *contracts.CapabilityDeclaration)
	}{
		{
			name: "empty_phase",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.ActivationPhases = []string{"migrate", ""}
			},
		},
		{
			name: "empty_check",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.VerificationChecks = []contracts.VerificationCheck{
					{Name: "vendor_present"},
					{Name: ""},
				}
			},
		},
		{
			name: "empty_command",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.DiagnosticCommands = []string{"routes:list", ""}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCapabilityRegistry()
			decl := laravelCapabilities()
			tt.mutate(&decl)
			if err := r.Register("laravel", decl); err == nil {
				t.Fatalf("Register with %s succeeded, want error", tt.name)
			}
		})
	}
}

// TestCapabilityRegistry_RegisterWhitespaceOnlyNameRejected verifies that
// whitespace-only names are rejected — they are non-empty strings but
// not meaningful capability identifiers.
//
// Reference: TS-P7-07 AC-2
func TestCapabilityRegistry_RegisterWhitespaceOnlyNameRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(decl *contracts.CapabilityDeclaration)
	}{
		{
			name: "space_phase",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.ActivationPhases = []string{"   "}
			},
		},
		{
			name: "tab_check",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.VerificationChecks = []contracts.VerificationCheck{{Name: "\t"}}
			},
		},
		{
			name: "space_command",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.DiagnosticCommands = []string{" "}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCapabilityRegistry()
			decl := laravelCapabilities()
			tt.mutate(&decl)
			if err := r.Register("laravel", decl); err == nil {
				t.Fatalf("Register with %s succeeded, want error", tt.name)
			}
		})
	}
}

// TestCapabilityRegistry_RegisterDuplicateNamesRejected verifies that
// duplicate names within the same category are rejected.
//
// Reference: TS-P7-07 AC-2
func TestCapabilityRegistry_RegisterDuplicateNamesRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(decl *contracts.CapabilityDeclaration)
	}{
		{
			name: "duplicate_phase",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.ActivationPhases = append(decl.ActivationPhases, "migrate")
			},
		},
		{
			name: "duplicate_check",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.VerificationChecks = append(decl.VerificationChecks,
					contracts.VerificationCheck{Name: "vendor_present"})
			},
		},
		{
			name: "duplicate_command",
			mutate: func(decl *contracts.CapabilityDeclaration) {
				decl.DiagnosticCommands = append(decl.DiagnosticCommands, "routes:list")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewCapabilityRegistry()
			decl := laravelCapabilities()
			tt.mutate(&decl)
			if err := r.Register("laravel", decl); err == nil {
				t.Fatalf("Register with %s succeeded, want error", tt.name)
			}
		})
	}
}

// TestCapabilityRegistry_UnknownFramework verifies that querying an
// unregistered framework returns ok=false without error.
//
// Reference: TS-P7-07 AC-3
func TestCapabilityRegistry_UnknownFramework(t *testing.T) {
	r := NewCapabilityRegistry()

	if _, ok := r.Capabilities("rails"); ok {
		t.Error("Capabilities(\"rails\") = ok=true, want ok=false")
	}
}

// TestCapabilityRegistry_Unregister verifies that Unregister removes the
// framework's declaration and that the framework disappears from
// Frameworks().
//
// Reference: TS-P7-07
func TestCapabilityRegistry_Unregister(t *testing.T) {
	r := NewCapabilityRegistry()
	if err := r.Register("laravel", laravelCapabilities()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	r.Unregister("laravel")

	if _, ok := r.Capabilities("laravel"); ok {
		t.Error("Capabilities(\"laravel\") after Unregister = ok=true, want ok=false")
	}
	if got := r.Frameworks(); len(got) != 0 {
		t.Errorf("Frameworks() after Unregister = %v, want empty", got)
	}

	// Re-register after unregister — removal must not poison the registry.
	if err := r.Register("laravel", laravelCapabilities()); err != nil {
		t.Fatalf("Register after Unregister failed: %v", err)
	}
}

// TestCapabilityRegistry_FrameworksSorted verifies Frameworks() returns
// the registered frameworks in sorted order.
//
// Reference: TS-P7-07
func TestCapabilityRegistry_FrameworksSorted(t *testing.T) {
	r := NewCapabilityRegistry()

	for _, framework := range []string{"flutter", "django", "laravel"} {
		if err := r.Register(framework, contracts.CapabilityDeclaration{
			ActivationPhases: []string{framework + "_phase"},
		}); err != nil {
			t.Fatalf("Register(%q) failed: %v", framework, err)
		}
	}

	want := []string{"django", "flutter", "laravel"}
	got := r.Frameworks()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks() = %v, want %v", got, want)
	}
}

// TestCapabilityRegistry_ConcurrentAccess verifies that the registry is
// safe for concurrent registration and reads by multiple goroutines.
//
// Reference: TS-P7-07
func TestCapabilityRegistry_ConcurrentAccess(t *testing.T) {
	r := NewCapabilityRegistry()

	var wg sync.WaitGroup
	n := 8
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			framework := fmt.Sprintf("fw%d", idx)
			if err := r.Register(framework, contracts.CapabilityDeclaration{
				ActivationPhases: []string{fmt.Sprintf("phase%d", idx)},
			}); err != nil {
				t.Errorf("Register(%q) failed: %v", framework, err)
			}
		}(i)
	}
	wg.Wait()

	if got := len(r.Frameworks()); got != n {
		t.Fatalf("Frameworks() has %d entries after concurrent register, want %d", got, n)
	}

	var readWG sync.WaitGroup
	for i := 0; i < n; i++ {
		readWG.Add(1)
		go func(idx int) {
			defer readWG.Done()
			framework := fmt.Sprintf("fw%d", idx)
			decl, ok := r.Capabilities(framework)
			if !ok {
				t.Errorf("Capabilities(%q) = ok=false, want ok=true", framework)
				return
			}
			if len(decl.ActivationPhases) != 1 {
				t.Errorf("Capabilities(%q).ActivationPhases = %v, want 1 entry", framework, decl.ActivationPhases)
			}
		}(i)
	}
	readWG.Wait()
}
