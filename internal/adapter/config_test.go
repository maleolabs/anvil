// Tests for the Core-side configuration extension registry (TS-P7-03).
package adapter

import (
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// laravelExtension returns the Laravel configuration extension used across
// registry tests. The Laravel adapter appears in tests only as a
// registered example — the Core engine itself is framework-agnostic
// (ADR-009 §9.6).
func laravelExtension() contracts.ConfigExtension {
	return contracts.ConfigExtension{
		Framework: "laravel",
		Keys: []contracts.ConfigKey{
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
	}
}

// TestConfigExtensionRegistry_RegisterValid verifies that a valid
// adapter extension registers successfully and is retrievable.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionRegistry_RegisterValid(t *testing.T) {
	r := NewConfigExtensionRegistry()

	if err := r.Register(laravelExtension()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ext, ok := r.Extension("laravel")
	if !ok {
		t.Fatal("Extension(\"laravel\") = ok=false, want ok=true")
	}
	if !reflect.DeepEqual(ext, laravelExtension()) {
		t.Errorf("Extension(\"laravel\") mismatch:\n got: %#v\nwant: %#v", ext, laravelExtension())
	}
}

// TestConfigExtensionRegistry_RegisterDuplicateFrameworkRejected verifies
// that a second extension for the same framework is rejected — a
// namespace conflict between adapters (one adapter per framework,
// ADR-009 §9.9).
//
// Reference: TS-P7-03 AC-3
func TestConfigExtensionRegistry_RegisterDuplicateFrameworkRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()
	if err := r.Register(laravelExtension()); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	dup := laravelExtension()
	dup.Keys = []contracts.ConfigKey{{Name: "framework.laravel.cache_store"}}
	if err := r.Register(dup); err == nil {
		t.Fatal("duplicate Register succeeded, want error")
	}
}

// TestConfigExtensionRegistry_RegisterKeyOutsideNamespaceRejected verifies
// that a key not prefixed with the adapter's namespace is rejected — keys
// are isolated from other adapters and from the Core schema.
//
// Reference: TS-P7-03 AC-2, AC-3
func TestConfigExtensionRegistry_RegisterKeyOutsideNamespaceRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

	tests := []struct {
		name string
		key  string
	}{
		{name: "core_schema_key", key: "project.name"},
		{name: "other_adapter_namespace", key: "framework.rails.db_adapter"},
		{name: "bare_framework_namespace", key: "framework.php_version"},
		{name: "namespace_without_trailing_dot", key: "framework.laravel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := laravelExtension()
			ext.Keys = []contracts.ConfigKey{{Name: tt.key}}
			if err := r.Register(ext); err == nil {
				t.Fatalf("Register with key %q succeeded, want error", tt.key)
			}
		})
	}
}

// TestConfigExtensionRegistry_RegisterDuplicateKeyRejected verifies that
// a duplicate key within the same extension is rejected.
//
// Reference: TS-P7-03 AC-3
func TestConfigExtensionRegistry_RegisterDuplicateKeyRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

	ext := laravelExtension()
	ext.Keys = append(ext.Keys, contracts.ConfigKey{Name: "framework.laravel.php_version"})
	if err := r.Register(ext); err == nil {
		t.Fatal("Register with duplicate key succeeded, want error")
	}
}

// TestConfigExtensionRegistry_RegisterInvalidFrameworkRejected verifies
// that an empty, dotted, or spaced framework name is rejected — the
// framework must be a single dot-free, space-free segment.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionRegistry_RegisterInvalidFrameworkRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

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
			ext := laravelExtension()
			ext.Framework = tt.framework
			if err := r.Register(ext); err == nil {
				t.Fatalf("Register with framework %q succeeded, want error", tt.framework)
			}
		})
	}
}

// TestConfigExtensionRegistry_RegisterEmptyKeyNameRejected verifies that
// an extension declaring a key with an empty name is rejected.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionRegistry_RegisterEmptyKeyNameRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

	ext := laravelExtension()
	ext.Keys = []contracts.ConfigKey{{Name: ""}}
	if err := r.Register(ext); err == nil {
		t.Fatal("Register with empty key name succeeded, want error")
	}
}

// TestConfigExtensionRegistry_RegisterReservedTopLevelKeyRejected verifies
// that keys under the reserved top-level "framework." namespace without a
// segment are rejected.
//
// Reference: TS-P7-03 AC-2
func TestConfigExtensionRegistry_RegisterReservedTopLevelKeyRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

	tests := []struct {
		name string
		key  string
	}{
		{name: "bare_reserved_prefix", key: "framework."},
		{name: "empty_segment", key: "framework..php_version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := laravelExtension()
			ext.Keys = []contracts.ConfigKey{{Name: tt.key}}
			err := r.Register(ext)
			if err == nil {
				t.Fatalf("Register with key %q succeeded, want error", tt.key)
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Errorf("error %q does not mention the reserved namespace", err)
			}
		})
	}
}

// TestConfigExtensionRegistry_RegisterEmptyNamespaceSegmentRejected
// verifies that keys with an empty segment within the adapter namespace
// are rejected — the key shape must be well-formed, not just prefixed.
//
// Reference: TS-P7-03 AC-1, AC-2
func TestConfigExtensionRegistry_RegisterEmptyNamespaceSegmentRejected(t *testing.T) {
	r := NewConfigExtensionRegistry()

	tests := []struct {
		name string
		key  string
	}{
		{name: "namespace_trailing_dot", key: "framework.laravel."},
		{name: "empty_segment_inside", key: "framework.laravel..php_version"},
		{name: "empty_segment_middle", key: "framework.laravel.php..version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := laravelExtension()
			ext.Keys = []contracts.ConfigKey{{Name: tt.key}}
			if err := r.Register(ext); err == nil {
				t.Fatalf("Register with key %q succeeded, want error", tt.key)
			}
		})
	}
}

// TestConfigExtensionRegistry_UnregisterRemovesAllKeys verifies that
// Unregister removes the framework's extension — removing an adapter
// removes all of its configuration extensions.
//
// Reference: TS-P7-03 AC-5
func TestConfigExtensionRegistry_UnregisterRemovesAllKeys(t *testing.T) {
	r := NewConfigExtensionRegistry()
	if err := r.Register(laravelExtension()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	r.Unregister("laravel")

	if _, ok := r.Extension("laravel"); ok {
		t.Error("Extension(\"laravel\") after Unregister = ok=true, want ok=false")
	}
	if got := r.AllKeys(); len(got) != 0 {
		t.Errorf("AllKeys() after Unregister = %v, want empty", got)
	}
}

// TestConfigExtensionRegistry_AllKeysFlattening verifies that AllKeys
// returns the flattened declared keys of all registered adapters in
// deterministic order.
//
// Reference: TS-P7-03 AC-2
func TestConfigExtensionRegistry_AllKeysFlattening(t *testing.T) {
	r := NewConfigExtensionRegistry()

	laravel := laravelExtension()
	if err := r.Register(laravel); err != nil {
		t.Fatalf("Register(laravel) failed: %v", err)
	}

	flutter := contracts.ConfigExtension{
		Framework: "flutter",
		Keys: []contracts.ConfigKey{
			{Name: "framework.flutter.targets", Default: "web,apk"},
			{Name: "framework.flutter.build_args"},
		},
	}
	if err := r.Register(flutter); err != nil {
		t.Fatalf("Register(flutter) failed: %v", err)
	}

	want := []string{
		"framework.flutter.targets",
		"framework.flutter.build_args",
		"framework.laravel.php_version",
		"framework.laravel.composer_flags",
	}
	got := r.AllKeys()
	if len(got) != len(want) {
		t.Fatalf("AllKeys() = %v, want %v", got, want)
	}
	for i, key := range got {
		if key.Name != want[i] {
			t.Errorf("AllKeys()[%d].Name = %q, want %q (keys: %v)", i, key.Name, want[i], got)
		}
	}
}

// TestConfigExtensionRegistry_FrameworksSorted verifies Frameworks()
// returns the registered frameworks in sorted order.
//
// Reference: TS-P7-03
func TestConfigExtensionRegistry_FrameworksSorted(t *testing.T) {
	r := NewConfigExtensionRegistry()

	flutter := contracts.ConfigExtension{
		Framework: "flutter",
		Keys:      []contracts.ConfigKey{{Name: "framework.flutter.targets"}},
	}
	django := contracts.ConfigExtension{
		Framework: "django",
		Keys:      []contracts.ConfigKey{{Name: "framework.django.secret_key"}},
	}
	for _, ext := range []contracts.ConfigExtension{flutter, django, laravelExtension()} {
		if err := r.Register(ext); err != nil {
			t.Fatalf("Register(%q) failed: %v", ext.Framework, err)
		}
	}

	want := []string{"django", "flutter", "laravel"}
	got := r.Frameworks()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks() = %v, want %v", got, want)
	}
}

// TestConfigExtensionRegistry_ExtensionMissing verifies that looking up an
// unregistered framework returns ok=false without error.
//
// Reference: TS-P7-03
func TestConfigExtensionRegistry_ExtensionMissing(t *testing.T) {
	r := NewConfigExtensionRegistry()

	if _, ok := r.Extension("rails"); ok {
		t.Error("Extension(\"rails\") = ok=true, want ok=false")
	}
}

// TestConfigExtensionRegistry_RegisterAfterUnregister verifies that a
// framework can be re-registered after being unregistered — removal does
// not poison the registry.
//
// Reference: TS-P7-03 AC-5
func TestConfigExtensionRegistry_RegisterAfterUnregister(t *testing.T) {
	r := NewConfigExtensionRegistry()
	if err := r.Register(laravelExtension()); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	r.Unregister("laravel")

	if err := r.Register(laravelExtension()); err != nil {
		t.Fatalf("Register after Unregister failed: %v", err)
	}
	if _, ok := r.Extension("laravel"); !ok {
		t.Error("Extension(\"laravel\") after re-register = ok=false, want ok=true")
	}
}
