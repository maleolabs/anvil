// Package adapter implements the Core-side adapter core engine for
// Framework Adapters: deterministic discovery of registered adapters
// (TS-P7-04), the lifecycle state machine (TS-P7-05), compatibility
// validation (TS-P7-06), configuration extension enforcement
// (TS-P7-03), capability declaration (TS-P7-07), and execution
// coordination (TS-P7-08).
//
// The package follows ADR-009: adapters are discovered, validated, and
// tracked by the Core; the Core provides the mechanism, while adapter
// descriptors are registered by consumers/installers (ADR-009 §9.1) and
// never contain framework-specific behavior (ADR-009 §9.6). Adapters are
// stateless (ADR-009 §9.8) — lifecycle tracking is Core-side, in-memory,
// per execution context. ADR-016 extends adapter support to multiple
// frameworks; at most one adapter per framework is active per execution
// context (ADR-009 §9.9). Config extensions follow the canonical schema
// namespace convention "framework.<adapter-name>" (ADR-005 §4.4,
// MVP-002 §3.2).
//
// Reference: TS-P7-03, TS-P7-04, TS-P7-05, TS-P7-06, ADR-009, ADR-016
package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"maleolabs.com/anvil/internal/contracts"
)

// VersionRange declares an inclusive version compatibility range
// [Min, Max]. An empty Min or Max means no lower or upper constraint;
// an empty range declares no constraint at all. The pattern follows
// deployment.RuntimeVersionConstraint.
//
// Reference: TS-P7-04
type VersionRange struct {
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

// AdapterInfo is the descriptor of an available adapter. It carries
// identity and compatibility declarations only — it has no execution
// surface; capability declaration is a separate mechanism (TS-P7-07).
//
// Reference: TS-P7-04 AC-1, AC-2
type AdapterInfo struct {
	// Framework is the adapter's framework name (e.g. "laravel"). It is
	// the deterministic discovery key and the config extension namespace
	// segment.
	Framework string

	// Name is the adapter's human-readable name (e.g. "Laravel Adapter").
	Name string

	// ConfigNamespace is the adapter's configuration namespace, computed
	// as "framework." + Framework (ADR-005 §4.4, MVP-002 §3.2).
	ConfigNamespace string

	// ConfigKeys are the framework-specific configuration keys the
	// adapter declares, all prefixed with ConfigNamespace.
	ConfigKeys []contracts.ConfigKey

	// CoreVersion is the Core version range the adapter supports.
	CoreVersion VersionRange

	// FrameworkVersion is the framework version range the adapter
	// supports (e.g. Laravel >= 10.0.0 and <= 11.x).
	FrameworkVersion VersionRange
}

// Registry is a deterministic in-memory store of available adapters,
// keyed by framework name. At most one adapter may be registered per
// framework (ADR-009 §9.9 — one adapter per framework per execution
// context). The registry is thread-safe, following the repository
// convention for shared registries (see internal/runtime/registry.go).
//
// Reference: TS-P7-04
type Registry struct {
	mu       sync.Mutex
	adapters map[string]AdapterInfo
}

// NewRegistry returns an empty adapter Registry.
//
// Reference: TS-P7-04
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]AdapterInfo)}
}

// Register adds an adapter to the registry. It rejects an empty or
// malformed framework name (single dot-free, space-free segment per the
// configuration extension contract), a duplicate framework (ADR-009
// §9.9), a ConfigNamespace that does not match the namespace convention,
// and ConfigKeys that are not prefixed with the adapter's namespace.
// ConfigNamespace is validated against the configuration extension
// contract: when empty it is computed as "framework." + Framework; when
// set it must equal that value (ADR-005 §4.4, MVP-002 §3.2).
//
// Reference: TS-P7-04 AC-2, ADR-009 §9.9
func (r *Registry) Register(a AdapterInfo) error {
	if !validFrameworkName(a.Framework) {
		return fmt.Errorf("cannot register adapter: framework name %q must be a single dot-free, space-free segment (e.g. \"laravel\")", a.Framework)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.adapters[a.Framework]; exists {
		return fmt.Errorf("cannot register adapter: an adapter for framework %q is already registered (ADR-009 §9.9)", a.Framework)
	}

	namespace := "framework." + a.Framework
	if a.ConfigNamespace == "" {
		a.ConfigNamespace = namespace
	} else if a.ConfigNamespace != namespace {
		return fmt.Errorf("cannot register adapter for framework %q: ConfigNamespace %q does not match the namespace convention %q", a.Framework, a.ConfigNamespace, namespace)
	}

	for _, key := range a.ConfigKeys {
		if key.Name == "" {
			return fmt.Errorf("cannot register adapter for framework %q: config key name must not be empty", a.Framework)
		}
		if !strings.HasPrefix(key.Name, namespace+".") {
			return fmt.Errorf("cannot register adapter for framework %q: config key %q is not prefixed with the adapter namespace %q", a.Framework, key.Name, namespace+".")
		}
	}

	r.adapters[a.Framework] = a
	return nil
}

// Unregister removes the adapter registered for the given framework, if
// any.
//
// Reference: TS-P7-04
func (r *Registry) Unregister(framework string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, framework)
}

// Discover returns the adapter registered for the given framework. It
// returns ok=false when no adapter exists — a graceful "not found", not
// an error (ADR-009 §9.7: adapters are optional; the Core proceeds with
// generic operations).
//
// Reference: TS-P7-04 AC-3
func (r *Registry) Discover(framework string) (AdapterInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.adapters[framework]
	return a, ok
}

// Frameworks returns the sorted list of registered framework names. The
// ordering is deterministic.
//
// Reference: TS-P7-04
func (r *Registry) Frameworks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
