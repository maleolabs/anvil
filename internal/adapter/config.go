// Configuration extension mechanism (TS-P7-03), Core side: the registry
// stores the configuration extensions declared by registered adapters and
// enforces the namespace isolation rules of the configuration extension
// contract. The canonical schema itself (internal/config) is not touched —
// schema integration is a later work item.
package adapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"maleolabs.com/anvil/internal/contracts"
)

// ConfigExtensionRegistry stores the configuration extensions declared by
// registered adapters and enforces the isolation rules of the
// configuration extension contract: every adapter key lives under its own
// "framework.<adapter-name>." namespace (ADR-005 §4.4, MVP-002 §3.2), so
// it can never conflict with the Core schema or with another adapter. The
// registry is thread-safe, following the repository convention for shared
// registries (see internal/runtime/registry.go).
//
// Reference: TS-P7-03 AC-1..AC-5
type ConfigExtensionRegistry struct {
	mu         sync.Mutex
	extensions map[string]contracts.ConfigExtension
}

// NewConfigExtensionRegistry returns an empty ConfigExtensionRegistry.
//
// Reference: TS-P7-03
func NewConfigExtensionRegistry() *ConfigExtensionRegistry {
	return &ConfigExtensionRegistry{extensions: make(map[string]contracts.ConfigExtension)}
}

// Register validates and stores one adapter configuration extension. It
// rejects:
//
//   - an empty or invalid Framework — must be a single dot-free,
//     space-free non-empty segment (the adapter namespace segment,
//     ADR-005 §4.4, MVP-002 §3.2);
//   - a duplicate Framework — a namespace conflict between adapters;
//   - a key with an empty name;
//   - a key that is not prefixed with the adapter's namespace
//     ("framework.<name>.");
//   - a key under the reserved top-level "framework." namespace without a
//     segment (e.g. "framework." or "framework..php_version");
//   - a key with an empty segment within the adapter namespace (e.g.
//     "framework.laravel." or "framework.laravel..php_version");
//   - a duplicate key within the extension.
//
// Reference: TS-P7-03 AC-1, AC-2, AC-3
func (r *ConfigExtensionRegistry) Register(ext contracts.ConfigExtension) error {
	if !validFrameworkName(ext.Framework) {
		return fmt.Errorf("cannot register configuration extension: framework name %q must be a single dot-free, space-free segment (e.g. \"laravel\")", ext.Framework)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.extensions[ext.Framework]; exists {
		return fmt.Errorf("cannot register configuration extension: framework %q already has a registered extension", ext.Framework)
	}

	namespace := "framework." + ext.Framework
	seen := make(map[string]struct{}, len(ext.Keys))
	for _, key := range ext.Keys {
		if key.Name == "" {
			return fmt.Errorf("cannot register configuration extension for framework %q: key name must not be empty", ext.Framework)
		}
		if reservedTopLevelKey(key.Name) {
			return fmt.Errorf("cannot register configuration extension for framework %q: key %q sits under the reserved top-level \"framework.\" namespace without a segment", ext.Framework, key.Name)
		}
		if !strings.HasPrefix(key.Name, namespace+".") {
			return fmt.Errorf("cannot register configuration extension for framework %q: key %q is not prefixed with the adapter namespace %q", ext.Framework, key.Name, namespace+".")
		}
		if emptyNamespaceSegment(key.Name, namespace+".") {
			return fmt.Errorf("cannot register configuration extension for framework %q: key %q contains an empty segment within the adapter namespace", ext.Framework, key.Name)
		}
		if _, dup := seen[key.Name]; dup {
			return fmt.Errorf("cannot register configuration extension for framework %q: duplicate key %q", ext.Framework, key.Name)
		}
		seen[key.Name] = struct{}{}
	}

	r.extensions[ext.Framework] = ext
	return nil
}

// Unregister removes the configuration extension of the given framework.
// Removing an adapter removes all of its configuration extensions
// (TS-P7-03 AC-5); the Core schema is never affected (ADR-009 §3.5).
//
// Reference: TS-P7-03 AC-5
func (r *ConfigExtensionRegistry) Unregister(framework string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.extensions, framework)
}

// Extension returns the registered configuration extension for the given
// framework, if any.
//
// Reference: TS-P7-03
func (r *ConfigExtensionRegistry) Extension(framework string) (contracts.ConfigExtension, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ext, ok := r.extensions[framework]
	return ext, ok
}

// Frameworks returns the sorted list of frameworks with a registered
// configuration extension. The ordering is deterministic.
//
// Reference: TS-P7-03
func (r *ConfigExtensionRegistry) Frameworks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.extensions))
	for name := range r.extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AllKeys returns the flattened declared keys of all registered adapters,
// in deterministic order: frameworks sorted by name, keys in declaration
// order within each extension.
//
// Reference: TS-P7-03 AC-2
func (r *ConfigExtensionRegistry) AllKeys() []contracts.ConfigKey {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.extensions))
	for name := range r.extensions {
		names = append(names, name)
	}
	sort.Strings(names)

	var keys []contracts.ConfigKey
	for _, framework := range names {
		keys = append(keys, r.extensions[framework].Keys...)
	}
	return keys
}

// validFrameworkName reports whether name is a single dot-free, space-free
// non-empty segment — the adapter namespace segment convention
// (ADR-005 §4.4, MVP-002 §3.2).
func validFrameworkName(name string) bool {
	return name != "" && !strings.ContainsAny(name, ". ")
}

// reservedTopLevelKey reports whether key sits directly under the reserved
// top-level "framework." namespace without a segment — e.g. "framework."
// or "framework..php_version". Such keys are not adapter-namespaced.
func reservedTopLevelKey(key string) bool {
	if !strings.HasPrefix(key, "framework.") {
		return false
	}
	rest := strings.TrimPrefix(key, "framework.")
	return rest == "" || strings.HasPrefix(rest, ".")
}

// emptyNamespaceSegment reports whether key contains an empty segment
// within the adapter namespace — e.g. "framework.laravel." or
// "framework.laravel..php_version". The prefix is the adapter's namespace
// including the trailing dot; the remainder must be a non-empty
// dot-separated chain of non-empty segments.
func emptyNamespaceSegment(key, prefix string) bool {
	rest := strings.TrimPrefix(key, prefix)
	if rest == "" {
		return true
	}
	for _, segment := range strings.Split(rest, ".") {
		if segment == "" {
			return true
		}
	}
	return false
}
