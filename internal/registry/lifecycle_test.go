package registry

import (
	"strings"
	"testing"
)

// lifecycleTestMetadata returns a Metadata with the given lifecycle state
// and removal date; the remaining fields are irrelevant to lifecycle
// behavior and left at their zero values (TS-014-01-03).
func lifecycleTestMetadata(state, removalDate string) Metadata {
	return Metadata{Lifecycle: Lifecycle{State: state, RemovalDate: removalDate}}
}

// TestLifecycleAdoptable_Published asserts a published standard is
// discoverable and installable — resolvable for fresh adoption
// (TS-014-01-03 DoD; ADR-023 §3; ADR-027 §3).
func TestLifecycleAdoptable_Published(t *testing.T) {
	if !LifecycleAdoptable(LifecycleStatePublished) {
		t.Error("LifecycleAdoptable(published) = false, want true")
	}
}

// TestLifecycleAdoptable_Deprecated asserts a deprecated standard remains
// installable (with warning — surfaced separately via LifecycleWarning),
// so it stays resolvable for fresh adoption (ADR-023 §3; ADR-027 §3).
func TestLifecycleAdoptable_Deprecated(t *testing.T) {
	if !LifecycleAdoptable(LifecycleStateDeprecated) {
		t.Error("LifecycleAdoptable(deprecated) = false, want true")
	}
}

// TestLifecycleAdoptable_Retired asserts a retired standard is removed
// from the registry and not resolvable for fresh adoption (TS-014-01-03
// DoD; ADR-027 §3).
func TestLifecycleAdoptable_Retired(t *testing.T) {
	if LifecycleAdoptable(LifecycleStateRetired) {
		t.Error("LifecycleAdoptable(retired) = true, want false")
	}
}

// TestLifecycleAdoptable_UnknownState asserts the helper guards unknown
// state strings via the lifecycle constants: not adoptable. Rejecting
// unknown states at the document level is the parse layer's job
// (TS-014-01-02); this is defense in depth, not validation.
func TestLifecycleAdoptable_UnknownState(t *testing.T) {
	for _, state := range []string{"", "Published", "archived", "draft"} {
		if LifecycleAdoptable(state) {
			t.Errorf("LifecycleAdoptable(%q) = true, want false", state)
		}
	}
}

// TestLifecycleUpdateAllowed_Published asserts only published standards
// receive updates (TS-014-01-03 DoD; ADR-023 §3; ADR-027 §3).
func TestLifecycleUpdateAllowed_Published(t *testing.T) {
	if !LifecycleUpdateAllowed(LifecycleStatePublished) {
		t.Error("LifecycleUpdateAllowed(published) = false, want true")
	}
}

// TestLifecycleUpdateAllowed_Deprecated asserts a deprecated standard
// receives no updates (TS-014-01-03 DoD; ADR-023 §3; ADR-027 §3). The
// rule is defined here for the update flow (TS-014-01-08) to consume; the
// update flow itself is out of this work item's scope.
func TestLifecycleUpdateAllowed_Deprecated(t *testing.T) {
	if LifecycleUpdateAllowed(LifecycleStateDeprecated) {
		t.Error("LifecycleUpdateAllowed(deprecated) = true, want false")
	}
}

// TestLifecycleUpdateAllowed_Retired asserts a retired standard receives
// no updates (ADR-027 §3: removed from the registry).
func TestLifecycleUpdateAllowed_Retired(t *testing.T) {
	if LifecycleUpdateAllowed(LifecycleStateRetired) {
		t.Error("LifecycleUpdateAllowed(retired) = true, want false")
	}
}

// TestLifecycleUpdateAllowed_UnknownState asserts the helper guards
// unknown state strings via the lifecycle constants: not updatable
// (defense in depth; the parse layer rejects unknown states).
func TestLifecycleUpdateAllowed_UnknownState(t *testing.T) {
	for _, state := range []string{"", "Published", "archived", "draft"} {
		if LifecycleUpdateAllowed(state) {
			t.Errorf("LifecycleUpdateAllowed(%q) = true, want false", state)
		}
	}
}

// TestLifecycleWarning_DeprecatedWithRemovalDate asserts a deprecated
// standard surfaces a warning that carries the announced removal date and
// notes the release receives no updates (TS-014-01-03 DoD; ADR-023 §3;
// ADR-027 §3). Surrounding whitespace on the removal date is trimmed
// before rendering.
func TestLifecycleWarning_DeprecatedWithRemovalDate(t *testing.T) {
	for _, removalDate := range []string{"2026-12-31T00:00:00Z", "  2026-12-31T00:00:00Z  "} {
		trimmed := "2026-12-31T00:00:00Z"
		warning, ok := LifecycleWarning(lifecycleTestMetadata(LifecycleStateDeprecated, removalDate).Lifecycle)
		if !ok {
			t.Fatalf("LifecycleWarning(deprecated with removalDate %q) ok = false, want true", removalDate)
		}
		if !strings.Contains(warning, trimmed) {
			t.Errorf("warning %q does not surface the removal date %q", warning, trimmed)
		}
		if !strings.Contains(warning, "no updates") {
			t.Errorf("warning %q does not note the release receives no updates", warning)
		}
	}
}

// TestLifecycleWarning_DeprecatedWithoutRemovalDate asserts a deprecated
// standard without an announced removal date still surfaces a warning,
// with a "no removal date announced" note (PM decision D-03: removalDate
// is optional, SHOULD be present once announced; the client must handle
// deprecated-without-removalDate gracefully). Whitespace-only removal
// dates are treated as absent.
func TestLifecycleWarning_DeprecatedWithoutRemovalDate(t *testing.T) {
	for _, removalDate := range []string{"", "   ", "\t\n"} {
		warning, ok := LifecycleWarning(lifecycleTestMetadata(LifecycleStateDeprecated, removalDate).Lifecycle)
		if !ok {
			t.Fatalf("LifecycleWarning(deprecated, removalDate %q) ok = false, want true", removalDate)
		}
		if !strings.Contains(warning, "no removal date announced") {
			t.Errorf("warning %q does not note that no removal date was announced", warning)
		}
		if !strings.Contains(warning, "no updates") {
			t.Errorf("warning %q does not note the release receives no updates", warning)
		}
		if strings.Contains(warning, "removal announced for") {
			t.Errorf("warning %q surfaces a removal date despite removalDate %q being absent", warning, removalDate)
		}
	}
}

// TestLifecycleWarning_Published asserts a published standard surfaces no
// deprecation warning (ADR-027 §3: published is installable and
// validated).
func TestLifecycleWarning_Published(t *testing.T) {
	warning, ok := LifecycleWarning(lifecycleTestMetadata(LifecycleStatePublished, "").Lifecycle)
	if ok {
		t.Errorf("LifecycleWarning(published) ok = true with warning %q, want false", warning)
	}
	if warning != "" {
		t.Errorf("LifecycleWarning(published) = %q, want empty", warning)
	}
}

// TestLifecycleWarning_Retired asserts a retired standard surfaces no
// deprecation warning — retired is not resolvable for fresh adoption and
// is not a deprecation-with-warning state (ADR-027 §3).
func TestLifecycleWarning_Retired(t *testing.T) {
	warning, ok := LifecycleWarning(lifecycleTestMetadata(LifecycleStateRetired, "").Lifecycle)
	if ok {
		t.Errorf("LifecycleWarning(retired) ok = true with warning %q, want false", warning)
	}
	if warning != "" {
		t.Errorf("LifecycleWarning(retired) = %q, want empty", warning)
	}
}

// TestLifecycleWarning_UnknownState asserts the helper guards unknown
// state strings via the lifecycle constants: no warning surfaces
// (defense in depth; the parse layer rejects unknown states).
func TestLifecycleWarning_UnknownState(t *testing.T) {
	for _, state := range []string{"", "Published", "archived", "draft"} {
		warning, ok := LifecycleWarning(lifecycleTestMetadata(state, "2026-12-31T00:00:00Z").Lifecycle)
		if ok {
			t.Errorf("LifecycleWarning(%q) ok = true with warning %q, want false", state, warning)
		}
		if warning != "" {
			t.Errorf("LifecycleWarning(%q) = %q, want empty", state, warning)
		}
	}
}
