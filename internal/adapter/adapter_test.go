// Tests for the adapter discovery mechanism (TS-P7-04).
package adapter

import (
	"reflect"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// laravelInfo returns the Laravel adapter descriptor used across
// discovery tests. The Laravel adapter appears in tests only as a
// registered example — the Core engine itself is framework-agnostic
// (ADR-009 §9.6).
func laravelInfo() AdapterInfo {
	return AdapterInfo{
		Framework:       "laravel",
		Name:            "Laravel Adapter",
		ConfigNamespace: "framework.laravel",
		ConfigKeys: []contracts.ConfigKey{
			{
				Name:        "framework.laravel.php_version",
				Description: "PHP version used to build the artifact",
				Required:    true,
			},
			{
				Name:        "framework.laravel.composer_flags",
				Description: "Extra flags passed to composer",
				Default:     "--no-dev",
			},
		},
		CoreVersion:      VersionRange{Min: "1.0.0", Max: "2.0.0"},
		FrameworkVersion: VersionRange{Min: "10.0.0", Max: "11.99.99"},
	}
}

// TestRegistry_DiscoverLaravel verifies that the Laravel adapter is
// discovered when the framework is configured as "laravel".
//
// Reference: TS-P7-04 AC-1
func TestRegistry_DiscoverLaravel(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(laravelInfo()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := r.Discover("laravel")
	if !ok {
		t.Fatal("Discover(\"laravel\") = ok=false, want ok=true")
	}
	want := laravelInfo()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover(\"laravel\") mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

// TestRegistry_DiscoverDeterministic verifies that discovery is
// deterministic — repeated lookups return the same result.
//
// Reference: TS-P7-04 AC-2
func TestRegistry_DiscoverDeterministic(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(laravelInfo()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	first, ok1 := r.Discover("laravel")
	second, ok2 := r.Discover("laravel")

	if !ok1 || !ok2 {
		t.Fatalf("Discover(\"laravel\") = ok(%v, %v), want both true", ok1, ok2)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("repeated Discover returned different results:\n first: %#v\nsecond: %#v", first, second)
	}
}

// TestRegistry_DiscoverUnknownFramework verifies that discovering an
// unregistered framework returns ok=false and no error — a graceful "not
// found" (ADR-009 §9.7), not an error.
//
// Reference: TS-P7-04 AC-3
func TestRegistry_DiscoverUnknownFramework(t *testing.T) {
	r := NewRegistry()

	got, ok := r.Discover("rails")
	if ok {
		t.Errorf("Discover(\"rails\") = ok=true with %#v, want ok=false", got)
	}
	if !reflect.DeepEqual(got, AdapterInfo{}) {
		t.Errorf("Discover(\"rails\") = %#v, want zero AdapterInfo", got)
	}
}

// TestRegistry_RegisterDuplicateRejected verifies that registering a
// second adapter for the same framework is rejected — one adapter per
// framework (ADR-009 §9.9).
//
// Reference: TS-P7-04 AC-2, ADR-009 §9.9
func TestRegistry_RegisterDuplicateRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(laravelInfo()); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	dup := laravelInfo()
	dup.Name = "Another Laravel Adapter"
	if err := r.Register(dup); err == nil {
		t.Fatal("duplicate Register succeeded, want error")
	} else if got := err.Error(); got == "" {
		t.Fatal("duplicate Register error is empty")
	}
}

// TestRegistry_RegisterEmptyFrameworkRejected verifies that an adapter
// with an empty framework name is rejected.
//
// Reference: TS-P7-04
func TestRegistry_RegisterEmptyFrameworkRejected(t *testing.T) {
	r := NewRegistry()

	a := laravelInfo()
	a.Framework = ""
	if err := r.Register(a); err == nil {
		t.Fatal("Register with empty Framework succeeded, want error")
	}
	if len(r.Frameworks()) != 0 {
		t.Errorf("registry has %v after rejected Register, want empty", r.Frameworks())
	}
}

// TestRegistry_RegisterComputesNamespace verifies that an empty
// ConfigNamespace is computed as "framework." + Framework, consistent
// with the configuration extension contract (ADR-005 §4.4, MVP-002 §3.2).
//
// Reference: TS-P7-04
func TestRegistry_RegisterComputesNamespace(t *testing.T) {
	r := NewRegistry()

	a := laravelInfo()
	a.ConfigNamespace = ""
	if err := r.Register(a); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := r.Discover("laravel")
	if !ok {
		t.Fatal("Discover(\"laravel\") = ok=false, want ok=true")
	}
	if want := "framework.laravel"; got.ConfigNamespace != want {
		t.Errorf("computed ConfigNamespace = %q, want %q", got.ConfigNamespace, want)
	}
}

// TestRegistry_RegisterRejectsMismatchedNamespace verifies that a
// ConfigNamespace inconsistent with the "framework." + Framework
// convention is rejected.
//
// Reference: TS-P7-04, ADR-005 §4.4
func TestRegistry_RegisterRejectsMismatchedNamespace(t *testing.T) {
	r := NewRegistry()

	a := laravelInfo()
	a.ConfigNamespace = "framework.laravel.custom"
	if err := r.Register(a); err == nil {
		t.Fatal("Register with mismatched ConfigNamespace succeeded, want error")
	}
}

// TestRegistry_RegisterRejectsMalformedFramework verifies that a dotted
// or spaced framework name is rejected — the framework must be a single
// dot-free, space-free segment per the configuration extension contract.
//
// Reference: TS-P7-04, ADR-005 §4.4
func TestRegistry_RegisterRejectsMalformedFramework(t *testing.T) {
	r := NewRegistry()

	for _, fw := range []string{"laravel.core", "laravel php", "laravel."} {
		a := laravelInfo()
		a.Framework = fw
		a.ConfigNamespace = "framework." + fw
		if err := r.Register(a); err == nil {
			t.Fatalf("Register with framework %q succeeded, want error", fw)
		}
	}
}

// TestRegistry_RegisterRejectsKeyOutsideNamespace verifies that ConfigKeys
// not prefixed with the adapter's namespace are rejected at registration,
// consistent with the configuration extension contract.
//
// Reference: TS-P7-04, TS-P7-03 AC-2
func TestRegistry_RegisterRejectsKeyOutsideNamespace(t *testing.T) {
	r := NewRegistry()

	a := laravelInfo()
	a.ConfigKeys = []contracts.ConfigKey{{Name: "project.name"}}
	if err := r.Register(a); err == nil {
		t.Fatal("Register with key outside namespace succeeded, want error")
	}
}

// TestRegistry_UnregisterRemoves verifies that Unregister removes the
// adapter for the given framework.
//
// Reference: TS-P7-04
func TestRegistry_UnregisterRemoves(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(laravelInfo()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	r.Unregister("laravel")

	if _, ok := r.Discover("laravel"); ok {
		t.Error("Discover(\"laravel\") after Unregister = ok=true, want ok=false")
	}
}

// TestRegistry_FrameworksSorted verifies Frameworks() returns the
// registered framework names in sorted order.
//
// Reference: TS-P7-04
func TestRegistry_FrameworksSorted(t *testing.T) {
	r := NewRegistry()
	for _, fw := range []string{"flutter", "laravel", "django"} {
		a := laravelInfo()
		a.Framework = fw
		a.Name = fw
		a.ConfigNamespace = "framework." + fw
		a.ConfigKeys = nil
		if err := r.Register(a); err != nil {
			t.Fatalf("Register(%q) failed: %v", fw, err)
		}
	}

	want := []string{"django", "flutter", "laravel"}
	got := r.Frameworks()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks() = %v, want %v", got, want)
	}
}

// TestRegistry_DiscoveredAdapterValidates verifies the discovery →
// validation integration (TS-P7-04 AC-4): an adapter returned by
// Discover can be passed to Validate, which advances its lifecycle to
// StageReady when compatible.
//
// Reference: TS-P7-04 AC-4, TS-P7-06 AC-6
func TestRegistry_DiscoveredAdapterValidates(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(laravelInfo()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	a, ok := r.Discover("laravel")
	if !ok {
		t.Fatal("Discover(\"laravel\") = ok=false, want ok=true")
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "1.4.2", "11.0.0")
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if !result.Compatible {
		t.Errorf("Validate = %#v, want Compatible=true", result)
	}
	if got := lc.Stage(); got != StageReady {
		t.Errorf("lifecycle stage after Validate = %q, want %q", got, StageReady)
	}
}
