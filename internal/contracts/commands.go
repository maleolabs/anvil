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
