// The template command contract payloads (TS-007-038) are defined in
// this file. They are data payloads only, consistent with the rest of
// the package: through the `template` command (contracts.CommandTemplate)
// the Core fetches the pipeline definitions the adapter owns — the build
// pipeline and the CI pipeline — at generation time, replacing the
// Core-embedded template functions (ADR-020 §1: framework knowledge
// moves OUT of the Core binary INTO the adapter binaries).
//
// The definitions are carried as execution.PipelineDefinition — the same
// type the Core's pipeline loader (internal/execution) parses and
// validates — so the Core can validate adapter output through the
// existing loader before writing it to .anvil/pipelines/. The import is
// one-directional: internal/execution does not import internal/contracts
// (it imports internal/platform, internal/output, internal/envvar only),
// so no import cycle exists. Adapters build the definitions with the
// same type and the Core validates and writes them unchanged.
//
// Reference: TS-007-038, ADR-020 §1, 005-adapter-command-contract §5.2
package contracts

import "maleolabs.com/anvil/internal/execution"

// TemplateRequest is the structured JSON payload the Core sends to an
// adapter to request its pipeline definitions. The payload is generic —
// it selects the adapter at invocation time and contains no
// framework-specific structure. The Framework field mirrors
// CapabilityRequest for symmetry and future use (e.g. versioned
// templates); the Core currently needs no per-call parameters.
//
// Reference: TS-007-038, ADR-020 §1
type TemplateRequest struct {
	// Framework names the adapter whose pipeline definitions are
	// requested (e.g. "laravel").
	Framework string `json:"framework"`
}

// TemplateResult is the structured JSON payload the adapter returns with
// its pipeline definitions. Build and CI are the definitions the Core
// writes to .anvil/pipelines/build.yaml and .anvil/pipelines/ci.yaml
// respectively after validating them through the pipeline loader (ADR-020
// §1). Both fields are optional: an adapter may return the build
// definition only, in which case no ci.yaml is written — the Core owns no
// default pipeline template data and no longer falls back to Core-owned
// definitions (TS-015-01-02, ADR-026 decision 1; the additive-field rule
// of 005 §8 keeps old and new adapters interoperable).
//
// Reference: TS-007-038, ADR-020 §1
type TemplateResult struct {
	// Build is the adapter-owned build pipeline definition. Nil means
	// the adapter provides no build definition and no build.yaml is
	// written.
	Build *execution.PipelineDefinition `json:"build,omitempty"`

	// CI is the adapter-owned CI pipeline definition. Nil means the
	// adapter provides no CI definition and no ci.yaml is written.
	CI *execution.PipelineDefinition `json:"ci,omitempty"`
}
