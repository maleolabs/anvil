// Package contractstability provides the per-step contract-stability
// verification of the ordered extraction sequence (TS-017-03-02, T-010).
//
// At every extraction step the engine keeps working against extracted
// standard content, and the subprocess contract — activation, verification,
// config extension, capability declaration — is preserved (ADR-028 §3;
// ANVIL_V2_EXTRACTION_SEQUENCE §4–§5; Transition Plan §6.5). The
// verification is the re-checkable evidence for the governance review: it
// drives the engine's public exchange machinery (adapter.Coordinator over
// the real Process Runner, ADR-008) against a standard executable that
// carries the extracted standard content — the stand-in for the
// anvil-standard-* repositories (ADR-025 §6.3) — and fails the step when
// any exchange regresses.
//
// The verification runs as part of `go test ./...`
// (TestContractStability) and through the documented entry point
// scripts/contract-stability-check.sh. A regression at any step fails the
// verification, which blocks the next step (ANVIL_V2_EXTRACTION_SEQUENCE
// §3.1, §5; TS-017-03-02 DoD).
package contractstability

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// Step identifies one extraction step of the ordered sequence
// (ANVIL_V2_EXTRACTION_SEQUENCE §3; Transition Plan §6.5): framework
// packages, then template content, then config knowledge, then adapter
// binaries.
type Step int

const (
	// StepFrameworkPackages is extraction step 1: the framework packages
	// (internal/laravel, internal/flutter) leave Core.
	StepFrameworkPackages Step = 1

	// StepTemplateContent is extraction step 2: framework build templates
	// leave Core; template generation flows through the installed
	// standard.
	StepTemplateContent Step = 2

	// StepConfigKnowledge is extraction step 3: framework config defaults
	// leave Core; config keys and defaults resolve from the installed
	// standard.
	StepConfigKnowledge Step = 3

	// StepAdapterBinaries is extraction step 4: in-repo adapter binary
	// builds leave Core; standards execute through standard executables
	// resolved per the executable resolution contract.
	StepAdapterBinaries Step = 4
)

// String returns the step's canonical name (step number plus content
// class), as named by the extraction sequence (ANVIL_V2_EXTRACTION_SEQUENCE
// §4).
func (s Step) String() string {
	switch s {
	case StepFrameworkPackages:
		return "step 1: framework packages"
	case StepTemplateContent:
		return "step 2: template content"
	case StepConfigKnowledge:
		return "step 3: config knowledge"
	case StepAdapterBinaries:
		return "step 4: adapter binaries"
	default:
		return fmt.Sprintf("unknown extraction step %d", int(s))
	}
}

// AllSteps returns the four extraction steps in their fixed order
// (ANVIL_V2_EXTRACTION_SEQUENCE §3; Transition Plan §6.5).
func AllSteps() []Step {
	return []Step{
		StepFrameworkPackages,
		StepTemplateContent,
		StepConfigKnowledge,
		StepAdapterBinaries,
	}
}

// VerifyStep verifies the engine-working condition of one extraction step
// against extracted standard content, carried by the standard executable
// at executable — the stand-in for the anvil-standard-<framework>
// repositories (ADR-025 §6.3). framework names the standard whose content
// is under verification.
//
// Every step verifies the full subprocess contract — capability
// declaration, activation (including rollback through the same
// declared-capability rule), verification, config extension, and config
// validation — so a regression in any exchange at any step fails the step
// and blocks the next step (ANVIL_V2_EXTRACTION_SEQUENCE §3.1, §5). The
// step-specific engine-working conditions are verified additionally:
//
//   - StepTemplateContent: template generation flows through the installed
//     standard — the template exchange returns pipeline definitions the
//     Core's pipeline loader validates (TS-015-01-02, A10).
//   - StepConfigKnowledge: framework config keys and defaults resolve from
//     the installed standard — the config extension exchange returns
//     namespace-isolated keys and the validation exchange accepts the
//     extended values (TS-015-01-03, ADR-026 decision 1).
//   - StepAdapterBinaries: standards execute through standard executables
//     — the caller supplies the executable resolved per the executable
//     resolution contract (ADR-025 §3.4; ResolveStandardExecutable), and
//     every exchange happens over that executable's subprocess boundary.
//
// It returns nil when the step's verification passes, or an error listing
// every violation.
func VerifyStep(ctx context.Context, step Step, executable, framework string) error {
	if step < StepFrameworkPackages || step > StepAdapterBinaries {
		return fmt.Errorf("contract-stability verification: %s", step)
	}

	var errs []error
	if err := verifyContractSurface(ctx, executable, framework); err != nil {
		errs = append(errs, fmt.Errorf("%s: subprocess contract regression: %w", step, err))
	}
	switch step {
	case StepTemplateContent:
		if err := verifyTemplateExchange(ctx, executable, framework); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step, err))
		}
	case StepConfigKnowledge:
		if err := verifyConfigKnowledgeExchange(ctx, executable, framework); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step, err))
		}
	}
	return errors.Join(errs...)
}

// verifyContractSurface runs the full subprocess contract exchange against
// the standard executable for framework: the capability declaration, the
// activation exchange (declared phases, in the declared sequence, plus the
// rollback operation through the same declared-capability rule), the
// verification exchange (declared checks), the config extension exchange
// and the config validation exchange. It also asserts the contract's
// invocation rule: undeclared capability is never invoked (C1) — an
// undeclared phase is rejected before any subprocess invocation.
//
// The exchange machinery is the engine's public one (adapter.Coordinator
// over execution.NewRunner), so the verification observes the same
// subprocess boundary the lifecycle commands use (command-contract.md §5;
// ADR-021 §3.4).
func verifyContractSurface(ctx context.Context, executable, framework string) error {
	runner := execution.NewRunner()
	capabilities := adapter.NewCapabilityRegistry()
	coord := adapter.NewCoordinator(runner, capabilities)

	// 1. Capability declaration exchange: the standard declares its
	// capability surface before any of it is invoked
	// (command-contract.md §4.1).
	capResult, err := coord.InvokeCapabilities(ctx, framework, executable)
	if err != nil {
		return fmt.Errorf("capability declaration exchange: %w", err)
	}
	decl := capResult.Declaration
	if err := capabilities.Register(framework, decl); err != nil {
		return fmt.Errorf("capability declaration exchange: register the declared capabilities: %w", err)
	}

	// 2. Activation exchange: lifecycle commands keep working against the
	// extracted standard content — every declared activation phase is
	// invoked, in the declared sequence, and completes successfully
	// (command-contract.md §4.2, C2).
	if len(decl.ActivationPhases) == 0 {
		return fmt.Errorf("activation exchange: standard %q declares no activation phases — extracted lifecycle content must declare its lifecycle phases (ADR-021 §5.4)", framework)
	}
	for _, phase := range decl.ActivationPhases {
		result, err := coord.InvokeActivation(ctx, framework, executable, contracts.ActivationRequest{
			Phase:     phase,
			Operation: contracts.PhaseOperationActivate,
			Release: contracts.ReleaseContext{
				ProjectID: "contract-stability-fixture",
			},
		})
		if err != nil {
			return fmt.Errorf("activation exchange phase %q: %w", phase, err)
		}
		if !result.Success {
			return fmt.Errorf("activation exchange phase %q: standard returned success=false: %s", phase, result.Error)
		}
	}

	// 3. Rollback through the same declared-capability rule (C4): the
	// first declared phase receives its rollback operation
	// (command-contract.md §4.3).
	rollbackPhase := decl.ActivationPhases[0]
	rollbackResult, err := coord.InvokeActivation(ctx, framework, executable, contracts.ActivationRequest{
		Phase:     rollbackPhase,
		Operation: contracts.PhaseOperationRollback,
		Release: contracts.ReleaseContext{
			ProjectID: "contract-stability-fixture",
		},
	})
	if err != nil {
		return fmt.Errorf("rollback exchange phase %q: %w", rollbackPhase, err)
	}
	if !rollbackResult.Success {
		return fmt.Errorf("rollback exchange phase %q: standard returned success=false: %s", rollbackPhase, rollbackResult.Error)
	}

	// 4. Verification exchange: every declared verification check is
	// invoked and passes (command-contract.md §4.4).
	if len(decl.VerificationChecks) == 0 {
		return fmt.Errorf("verification exchange: standard %q declares no verification checks — extracted lifecycle content must declare its verification checks (ADR-021 §5.4)", framework)
	}
	for _, check := range decl.VerificationChecks {
		outcome, err := coord.InvokeVerification(ctx, framework, executable, contracts.VerificationRequest{
			Check:        check.Name,
			ArtifactPath: "contract-stability-fixture",
		})
		if err != nil {
			return fmt.Errorf("verification exchange check %q: %w", check.Name, err)
		}
		if !outcome.Passed {
			return fmt.Errorf("verification exchange check %q: standard returned passed=false: %s", check.Name, outcome.Details)
		}
	}

	// 5. Config extension exchange: the standard declares its
	// framework-specific config keys under its own namespace
	// (command-contract.md §4.5; ADR-005 §4.4).
	extResult, err := coord.InvokeConfigExtension(ctx, framework, executable)
	if err != nil {
		return fmt.Errorf("config extension exchange: %w", err)
	}
	if err := verifyNamespaceIsolation(extResult.Extension, framework); err != nil {
		return err
	}

	// 6. Config validation exchange: the standard validates its own
	// extended values; the runtime passes values through
	// (command-contract.md §4.5 C6).
	var values []contracts.ConfigValue
	for _, key := range extResult.Extension.Keys {
		values = append(values, contracts.ConfigValue{Key: key.Name, Value: key.Default})
	}
	if len(values) == 0 {
		return fmt.Errorf("config validation exchange: standard %q declares no configuration extension keys — extracted lifecycle content must declare its config keys (ADR-021 §5.4)", framework)
	}
	valResult, err := coord.InvokeConfigValidation(ctx, framework, executable, contracts.ConfigValidationRequest{Values: values})
	if err != nil {
		return fmt.Errorf("config validation exchange: %w", err)
	}
	if !valResult.Valid {
		return fmt.Errorf("config validation exchange: standard rejected its own extended values: %s", strings.Join(valResult.Errors, "; "))
	}

	// 7. Only declared capability is invoked (C1): an undeclared phase is
	// rejected before any subprocess invocation — the declared surface is
	// the bound, not an open door (command-contract.md §4.6 C1; Manifesto
	// §7).
	if _, err := coord.InvokeActivation(ctx, framework, executable, contracts.ActivationRequest{
		Phase:     "undeclared-phase-" + framework,
		Operation: contracts.PhaseOperationActivate,
		Release: contracts.ReleaseContext{
			ProjectID: "contract-stability-fixture",
		},
	}); err == nil {
		return fmt.Errorf("an undeclared activation phase was invoked — only declared capability may be invoked (C1)")
	}

	return nil
}

// verifyTemplateExchange verifies the step-2 engine-working condition:
// template generation flows through the installed standard (TS-015-01-02,
// A10; ANVIL_V2_EXTRACTION_SEQUENCE §4.2). The template exchange returns
// the standard-owned pipeline definitions, and the definitions pass the
// Core's pipeline loader validation — the same validation the CLI applies
// before writing them to .anvil/pipelines/ (ADR-020 §1).
func verifyTemplateExchange(ctx context.Context, executable, framework string) error {
	runner := execution.NewRunner()
	coord := adapter.NewCoordinator(runner, adapter.NewCapabilityRegistry())

	result, err := coord.InvokeTemplate(ctx, framework, executable, contracts.TemplateRequest{Framework: framework})
	if err != nil {
		return fmt.Errorf("template exchange: %w", err)
	}
	if result.Build == nil && result.CI == nil {
		return fmt.Errorf("template exchange: standard %q returned no pipeline definitions — template generation must flow through the installed standard (TS-015-01-02, A10)", framework)
	}
	if result.Build != nil {
		if err := result.Build.Validate(); err != nil {
			return fmt.Errorf("template exchange: build pipeline definition is invalid: %w", err)
		}
	}
	if result.CI != nil {
		if err := result.CI.Validate(); err != nil {
			return fmt.Errorf("template exchange: CI pipeline definition is invalid: %w", err)
		}
	}
	return nil
}

// verifyConfigKnowledgeExchange verifies the step-3 engine-working
// condition: framework config keys and defaults resolve from the installed
// standard, not from the runtime (TS-015-01-03, ADR-026 decision 1;
// ANVIL_V2_EXTRACTION_SEQUENCE §4.3). The extension keys are
// namespace-isolated under the framework's own namespace, and the standard
// validates its own extended values (command-contract.md §4.5, C6).
func verifyConfigKnowledgeExchange(ctx context.Context, executable, framework string) error {
	runner := execution.NewRunner()
	coord := adapter.NewCoordinator(runner, adapter.NewCapabilityRegistry())

	extResult, err := coord.InvokeConfigExtension(ctx, framework, executable)
	if err != nil {
		return fmt.Errorf("config knowledge exchange: %w", err)
	}
	if err := verifyNamespaceIsolation(extResult.Extension, framework); err != nil {
		return err
	}

	var values []contracts.ConfigValue
	for _, key := range extResult.Extension.Keys {
		values = append(values, contracts.ConfigValue{Key: key.Name, Value: key.Default})
	}
	if len(values) == 0 {
		return fmt.Errorf("config knowledge exchange: standard %q declares no configuration extension keys — config keys and defaults must resolve from the installed standard (TS-015-01-03)", framework)
	}
	valResult, err := coord.InvokeConfigValidation(ctx, framework, executable, contracts.ConfigValidationRequest{Values: values})
	if err != nil {
		return fmt.Errorf("config knowledge exchange: %w", err)
	}
	if !valResult.Valid {
		return fmt.Errorf("config knowledge exchange: standard rejected its own extended config values: %s", strings.Join(valResult.Errors, "; "))
	}
	return nil
}

// verifyNamespaceIsolation asserts that every declared config key is
// prefixed with the framework's own namespace segment
// (command-contract.md §4.5; ADR-005 §4.4): extended configuration lives
// under "framework.<framework>." and the runtime enforces the isolation.
func verifyNamespaceIsolation(ext contracts.ConfigExtension, framework string) error {
	wantPrefix := "framework." + framework + "."
	if len(ext.Keys) == 0 {
		return fmt.Errorf("config extension exchange: standard %q declares no configuration extension keys — extracted lifecycle content must declare its config keys (ADR-021 §5.4)", framework)
	}
	for _, key := range ext.Keys {
		if !strings.HasPrefix(key.Name, wantPrefix) {
			return fmt.Errorf("config extension exchange: key %q violates namespace isolation — extended keys must be prefixed %q (ADR-005 §4.4, command-contract.md §4.5)", key.Name, wantPrefix)
		}
	}
	return nil
}

// StandardExecutableName returns the executable name of a delivery
// lifecycle standard per the executable naming contract (ADR-025 §3.4,
// §12.2): "anvil-standard-<framework>". The name is the default that
// preserves the v1.x executable resolution contract across the repository
// split; the framework packages that built in-repo adapter binaries left
// Core in TS-016-01-01 / TS-016-02-01, so the standard repositories build
// the executables now (ANVIL_V2_EXTRACTION_SEQUENCE §4.4).
func StandardExecutableName(framework string) string {
	return "anvil-standard-" + framework
}

// ResolveStandardExecutable resolves the standard executable for framework
// on PATH per the executable resolution contract (ADR-025 §3.4). It is the
// step-4 resolution surface: standards execute through the resolved
// standard executable, never in-process (command-contract.md §5; ADR-021
// §3.4).
func ResolveStandardExecutable(framework string) (string, error) {
	return exec.LookPath(StandardExecutableName(framework))
}
