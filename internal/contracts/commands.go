// The stable command names of the adapter command contract (ADR-009 §3.4)
// are defined in this file. The command name is the first CLI argument
// the Core passes when invoking an adapter executable:
// <adapter-executable> <command> [<json-payload>]. Command names are part
// of the stable contract — they never change between Core versions
// without a documented deprecation path (ADR-010 §9.5).
//
// Reference: TS-P7-07, TS-P7-08, ADR-009 §3.4
package contracts

// CommandCapabilities requests an adapter's declared capabilities. The
// Core invokes it with a CapabilityRequest JSON payload as the trailing
// argument and expects a CapabilityResult JSON document on stdout.
//
// Reference: TS-P7-07, ADR-009 §3.4
const CommandCapabilities = "capabilities"

// CommandActivation invokes one activation phase operation. The Core
// invokes it with an ActivationRequest JSON payload as the trailing
// argument and expects an ActivationResult JSON document on stdout.
//
// Reference: TS-P7-01, TS-P7-08, ADR-009 §3.4
const CommandActivation = "activate"

// CommandVerification invokes one verification check. The Core invokes it
// with a VerificationRequest JSON payload as the trailing argument and
// expects a VerificationOutcome JSON document on stdout.
//
// Reference: TS-P7-02, TS-P7-08, ADR-009 §3.4
const CommandVerification = "verify"

// CommandConfigExtension requests an adapter's declared configuration
// extension keys. The Core invokes it with a ConfigExtensionRequest JSON
// payload as the trailing argument and expects a ConfigExtensionResult
// JSON document on stdout. The command name is documented in
// 005-adapter-command-contract §6.2 and was added as a stable constant in
// this batch (TS-P7-12) — an additive extension of the command set that
// leaves the pre-existing command names untouched (ADR-010 §9.5).
//
// Reference: TS-P7-03, TS-P7-12, ADR-009 §6.3
const CommandConfigExtension = "extension"

// CommandConfigValidation requests validation of extended configuration
// values. The Core invokes it with a ConfigValidationRequest JSON payload
// as the trailing argument and expects a ConfigValidationResult JSON
// document on stdout. The command name is documented in
// 005-adapter-command-contract §6.2 and was added as a stable constant in
// this batch (TS-P7-12) — an additive extension of the command set that
// leaves the pre-existing command names untouched (ADR-010 §9.5).
//
// Reference: TS-P7-03, TS-P7-12, ADR-009 §6.3
const CommandConfigValidation = "validate"

// CommandBuild invokes an adapter's build pipeline: the build phases the
// adapter declares in its capability declaration (TS-P7-14). The Core
// invokes it with a BuildRequest JSON payload as the trailing argument
// and expects a BuildResult JSON document on stdout. The command name is
// documented in 005-adapter-command-contract §6.2 and was added as a
// stable constant in this batch (TS-P7-14) — an additive extension of the
// command set that leaves the pre-existing command names untouched
// (ADR-010 §9.5).
//
// Reference: TS-P7-14, ADR-009 §6.3
const CommandBuild = "build"

// CommandManifest requests the activation and rollback command strings
// stored in the artifact manifest at packaging time (ADR-017). Unlike
// the other commands, the Core invokes it with NO JSON payload — the
// manifest command carries no request data (005-adapter-command-contract
// §10.10); it expects a ManifestCommandResult JSON document on stdout.
// The command name is documented in 005-adapter-command-contract §10.10
// and was added as a stable constant in this batch (TS-P7-15, TS-P7-16)
// — an additive extension of the command set that leaves the
// pre-existing command names untouched (ADR-010 §9.5).
//
// Reference: TS-P7-15, TS-P7-16, ADR-009 §6.3
const CommandManifest = "manifest"

// CommandTemplate requests an adapter's pipeline definitions: the build
// and CI pipeline definitions the adapter owns (ADR-020 §1 — framework
// knowledge moves OUT of the Core binary INTO the adapter binaries). The
// Core invokes it with a TemplateRequest JSON payload as the trailing
// argument and expects a TemplateResult JSON document on stdout, then
// validates the returned definitions through the pipeline loader and
// writes them to .anvil/pipelines/ at generation time. The command name
// is documented in 005-adapter-command-contract §5.2 and was added as a
// stable constant in this batch (TS-007-038) — an additive extension of
// the command set that leaves the pre-existing command names untouched
// (ADR-010 §9.5); an old adapter without the command degrades to the
// Core's generic template fallback ("unknown command" exit per 005
// §10.2).
//
// Reference: TS-007-038, ADR-020 §1, ADR-009 §6.3
const CommandTemplate = "template"
