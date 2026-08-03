// The manifest command payload (TS-P7-15, TS-P7-16) is defined in this
// file. It is a data payload only, consistent with the rest of the
// package: the artifact manifest stores the full activation and rollback
// command strings as deployment metadata (ADR-017), and the Core obtains
// them from the framework adapter at packaging time through the manifest
// command. The Core-side invocation lives in internal/adapter (execution
// coordinator) and the CLI wiring in cmd/artifact.go — the Core never
// imports framework adapter packages (ADR-009 §8.1).
//
// Reference: TS-P7-15, TS-P7-16, ADR-017, 005-adapter-command-contract §10.10
package contracts

// ManifestCommandResult is the payload returned by the manifest command:
// the full activation and rollback command strings stored in the artifact
// manifest at packaging time (ADR-017, 005 §10.10).
//
// The values are supplied by the framework adapter (e.g.
// internal/laravel.ActivationCommands) through the generic packaging
// mechanism (artifact.PackageOptions) — the Core stays framework-agnostic
// and never imports adapter packages (ADR-009 §8.1). An adapter without
// server activation (the hybrid deployment model, ADR-016 — Flutter)
// returns empty slices, which the packaging layer omits from the manifest
// (omitempty — backward compatible with old artifacts).
//
// Reference: TS-P7-15, TS-P7-16, ADR-017, 005-adapter-command-contract §10.10
type ManifestCommandResult struct {
	// ActivationCommands are the full activation command strings (e.g.
	// "php artisan migrate --force") in execution order, stored in
	// artifact.Manifest.ActivationCommands. Empty when the framework has
	// no server activation (omitted by omitempty).
	ActivationCommands []string `json:"activation_commands,omitempty"`

	// RollbackCommands are the full rollback command strings (e.g.
	// "php artisan migrate:rollback") in execution order, stored in
	// artifact.Manifest.RollbackCommands. Empty when the framework has
	// no server activation (omitted by omitempty).
	RollbackCommands []string `json:"rollback_commands,omitempty"`
}
