// Tests for the Laravel adapter manifest command metadata (TS-P7-15,
// TS-P7-16). These verify the exact command strings stored in the
// artifact manifest for activation and rollback.
package laravel

import (
	"reflect"
	"strings"
	"testing"
)

// TestActivationCommands_ExactOrder verifies that ActivationCommands
// returns exactly the four activation commands in execution order:
// database migration first, then cache warming for config, routes, and
// views (TS-P7-15 AC-1, AC-3, AC-4).
func TestActivationCommands_ExactOrder(t *testing.T) {
	want := []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
	}

	got := ActivationCommands()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ActivationCommands() = %v, want %v", got, want)
	}
}

// TestActivationCommands_OrderMatchesPhaseTable verifies that the
// manifest activation commands stay consistent with the executable
// activation phase table (activation.go) for the phases they share:
// migrate first, then the cache phases, in the same relative order.
//
// The command list is a deliberate subset: TS-P7-15 AC-3 selects
// migrate, config:cache, route:cache, and view:cache, while the phase
// table (TS-P7-09) additionally declares event_cache — the manifest
// stores the commands the orchestrator executes, the phase table the
// adapter's executable behavior; the two surfaces overlap but are not
// required to be identical.
func TestActivationCommands_OrderMatchesPhaseTable(t *testing.T) {
	commands := ActivationCommands()

	// The migrate phase is the first declared phase (activation.go).
	if commands[0] != "php artisan migrate --force" {
		t.Errorf("first activation command = %q, want the migrate phase", commands[0])
	}

	// Commands shared with the phase table must use the same
	// `php artisan <args>` form and appear in the phase table order.
	// The index of each phase command in the manifest must be
	// monotonically increasing in phase table order.
	sharedPhaseNames := []string{PhaseMigrate, PhaseConfigCache, PhaseRouteCache}
	prevIndex := -1
	for _, name := range sharedPhaseNames {
		p, ok := lookupPhase(name)
		if !ok {
			t.Fatalf("phase %q missing from phase table", name)
		}
		want := "php artisan " + strings.Join(p.activateArgs, " ")
		index := -1
		for i, cmd := range commands {
			if cmd == want {
				index = i
				break
			}
		}
		if index < 0 {
			t.Errorf("phase %q command %q not present in ActivationCommands() = %v", name, want, commands)
			continue
		}
		if index <= prevIndex {
			t.Errorf("phase %q command at index %d, want after previous shared phase at index %d", name, index, prevIndex)
		}
		prevIndex = index
	}
}

// TestRollbackCommands_Exact verifies that RollbackCommands returns
// exactly the migrate:rollback command as a string array (TS-P7-16
// AC-1, AC-3).
func TestRollbackCommands_Exact(t *testing.T) {
	want := []string{
		"php artisan migrate:rollback",
	}

	got := RollbackCommands()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("RollbackCommands() = %v, want %v", got, want)
	}
}
