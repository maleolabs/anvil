// Capability declaration mechanism (TS-P7-07), Core side: the registry
// stores the capability declarations registered by adapters and enforces
// the declaration rules of the capability contract. The execution
// coordinator (TS-P7-08) reads these declarations to determine what to
// invoke.
package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"maleolabs.com/anvil/internal/contracts"
)

// CapabilityRegistry stores the capability declarations registered by
// adapters, keyed by framework name. The registry is thread-safe,
// following the repository convention for shared registries (see
// internal/runtime/registry.go).
//
// Reference: TS-P7-07
type CapabilityRegistry struct {
	mu           sync.Mutex
	capabilities map[string]contracts.CapabilityDeclaration
}

// NewCapabilityRegistry returns an empty CapabilityRegistry.
//
// Reference: TS-P7-07
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{capabilities: make(map[string]contracts.CapabilityDeclaration)}
}

// Register validates and stores one adapter capability declaration. It
// rejects:
//
//   - an empty or invalid framework — must be a single dot-free,
//     space-free non-empty segment (the adapter namespace segment,
//     ADR-005 §4.4, MVP-002 §3.2);
//   - a duplicate framework — one adapter per framework (ADR-009 §9.9);
//   - an empty or whitespace-only activation phase name;
//   - an empty or whitespace-only build phase name (TS-P7-14);
//   - a verification check with an empty or whitespace-only name;
//   - an empty or whitespace-only diagnostic command name;
//   - a duplicate name within the same category — two identical phases,
//     two identical commands, or two checks with the same name;
//   - a deployment model that is not one of the known models
//     (server/hybrid/package, ADR-016) when a model is declared
//     (TS-P7-13). An empty deployment model is valid — a generic
//     adapter declares no model.
//
// Names are compared exactly (no trimming); whitespace-only names are
// rejected because they are not meaningful capability identifiers. An
// empty declaration — no phases, no checks, no commands, no model —
// registers fine: an adapter that declares no capabilities is handled
// gracefully (TS-P7-07 AC-4).
//
// Reference: TS-P7-07 AC-1, AC-2, AC-3, AC-4, TS-P7-13, TS-P7-14
func (r *CapabilityRegistry) Register(framework string, decl contracts.CapabilityDeclaration) error {
	if !validFrameworkName(framework) {
		return fmt.Errorf("cannot register capability declaration: framework name %q must be a single dot-free, space-free segment (e.g. \"laravel\")", framework)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.capabilities[framework]; exists {
		return fmt.Errorf("cannot register capability declaration: framework %q already has a registered declaration (ADR-009 §9.9)", framework)
	}

	if !validDeploymentModel(decl.DeploymentModel) {
		return fmt.Errorf(
			"cannot register capability declaration for framework %q: unknown deployment model %q (expected one of %q, %q, %q)",
			framework, decl.DeploymentModel,
			contracts.DeploymentModelServer, contracts.DeploymentModelHybrid, contracts.DeploymentModelPackage,
		)
	}

	seenPhases := make(map[string]struct{}, len(decl.ActivationPhases))
	for _, phase := range decl.ActivationPhases {
		if strings.TrimSpace(phase) == "" {
			return fmt.Errorf("cannot register capability declaration for framework %q: activation phase name must not be empty", framework)
		}
		if _, dup := seenPhases[phase]; dup {
			return fmt.Errorf("cannot register capability declaration for framework %q: duplicate activation phase %q", framework, phase)
		}
		seenPhases[phase] = struct{}{}
	}

	seenBuildPhases := make(map[string]struct{}, len(decl.BuildPhases))
	for _, phase := range decl.BuildPhases {
		if strings.TrimSpace(phase) == "" {
			return fmt.Errorf("cannot register capability declaration for framework %q: build phase name must not be empty", framework)
		}
		if _, dup := seenBuildPhases[phase]; dup {
			return fmt.Errorf("cannot register capability declaration for framework %q: duplicate build phase %q", framework, phase)
		}
		seenBuildPhases[phase] = struct{}{}
	}

	seenChecks := make(map[string]struct{}, len(decl.VerificationChecks))
	for _, check := range decl.VerificationChecks {
		if strings.TrimSpace(check.Name) == "" {
			return fmt.Errorf("cannot register capability declaration for framework %q: verification check name must not be empty", framework)
		}
		if _, dup := seenChecks[check.Name]; dup {
			return fmt.Errorf("cannot register capability declaration for framework %q: duplicate verification check %q", framework, check.Name)
		}
		seenChecks[check.Name] = struct{}{}
	}

	seenCommands := make(map[string]struct{}, len(decl.DiagnosticCommands))
	for _, command := range decl.DiagnosticCommands {
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("cannot register capability declaration for framework %q: diagnostic command name must not be empty", framework)
		}
		if _, dup := seenCommands[command]; dup {
			return fmt.Errorf("cannot register capability declaration for framework %q: duplicate diagnostic command %q", framework, command)
		}
		seenCommands[command] = struct{}{}
	}

	r.capabilities[framework] = decl
	return nil
}

// Unregister removes the capability declaration of the given framework,
// if any. Removing an adapter removes all of its declared capabilities.
//
// Reference: TS-P7-07
func (r *CapabilityRegistry) Unregister(framework string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.capabilities, framework)
}

// Capabilities returns the registered capability declaration for the
// given framework. It returns ok=false when the framework has no
// registered declaration — a graceful "not found", not an error
// (ADR-009 §9.7: adapters are optional; the Core proceeds with generic
// operations).
//
// Reference: TS-P7-07 AC-3
func (r *CapabilityRegistry) Capabilities(framework string) (contracts.CapabilityDeclaration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	decl, ok := r.capabilities[framework]
	return decl, ok
}

// Frameworks returns the sorted list of frameworks with a registered
// capability declaration. The ordering is deterministic.
//
// Reference: TS-P7-07
func (r *CapabilityRegistry) Frameworks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.capabilities))
	for name := range r.capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validDeploymentModel reports whether the given deployment model is one
// of the known models (server/hybrid/package, ADR-016). An empty model is
// valid — a generic adapter declares no deployment model (TS-P7-13 AC-2).
//
// Reference: TS-P7-13 AC-2, ADR-016
func validDeploymentModel(model string) bool {
	switch model {
	case "", string(contracts.DeploymentModelServer), string(contracts.DeploymentModelHybrid), string(contracts.DeploymentModelPackage):
		return true
	default:
		return false
	}
}
