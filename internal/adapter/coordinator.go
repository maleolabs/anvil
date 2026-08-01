// Execution coordinator (TS-P7-08), Core side: the coordinator reads the
// capability declarations registered by adapters (TS-P7-07) and invokes
// adapter executables as subprocesses through the Process Runner
// (ADR-008) at the lifecycle extension points. It is framework-agnostic
// and contains no framework-specific values (ADR-009 §9.6).
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// adapterTimeout is the maximum wall-clock time one adapter operation may
// run before the Process Runner terminates it. Adapter operations are
// short-lived lifecycle hooks; a bounded timeout keeps a stuck adapter
// from stalling a release.
const adapterTimeout = 30 * time.Second

// buildTimeout is the maximum wall-clock time an adapter build pipeline
// may run before the Process Runner terminates it. Builds execute the
// full framework build — dependency installation and asset compilation
// routinely exceed the short adapter operation timeout, so the build
// pipeline gets a dedicated, longer bound (TS-P7-14).
const buildTimeout = 15 * time.Minute

// Coordinator invokes adapter operations at the lifecycle extension
// points: activation phases during release activation (EPIC-004 §7.2),
// verification checks during artifact verification (EPIC-003 §7.5),
// capability requests when collecting an adapter's declared capabilities
// (TS-P7-07), and the build pipeline when a release is built
// (TS-P7-14). It reads the framework's declared capabilities to
// determine what to invoke (TS-P7-08 AC-3) and dispatches each operation
// to the adapter executable as a subprocess through the Process Runner
// (TS-P7-08 AC-4, AC-5). The coordinator contains zero framework-specific
// values — it never branches on framework identity (TS-P7-08 AC-6,
// ADR-009 §9.6).
//
// Reference: TS-P7-08, TS-P7-07, TS-P7-14, ADR-009 §6.3
type Coordinator struct {
	runner       execution.Runner
	capabilities *CapabilityRegistry
}

// NewCoordinator returns a Coordinator that dispatches adapter operations
// through runner based on the capability declarations registered in
// capabilities. Nil dependencies are allowed at construction; the
// invocation methods return descriptive errors when a dependency is
// missing.
//
// Reference: TS-P7-08
func NewCoordinator(runner execution.Runner, capabilities *CapabilityRegistry) *Coordinator {
	return &Coordinator{runner: runner, capabilities: capabilities}
}

// ready reports whether the coordinator can operate. It returns a
// descriptive, actionable error when the receiver or a dependency is
// nil — a misconfigured caller, not an adapter failure.
//
// Reference: TS-P7-08
func (c *Coordinator) ready() error {
	if c == nil {
		return errors.New("coordinator: cannot dispatch adapter operations: nil Coordinator")
	}
	if c.runner == nil {
		return errors.New("coordinator: cannot dispatch adapter operations: Process Runner is nil")
	}
	if c.capabilities == nil {
		return errors.New("coordinator: cannot dispatch adapter operations: capability registry is nil")
	}
	return nil
}

// InvokeActivation invokes one activation phase operation of the adapter
// executable registered for framework. It first checks the framework's
// declared capabilities — an undeclared phase is never invoked
// (TS-P7-08 AC-3). The request is passed as a single JSON argument
// (the Process Runner has no stdin channel); the adapter's JSON result
// on stdout is authoritative for the phase outcome (005-adapter-command-
// contract §5 — exit-code/JSON agreement is the adapter's
// responsibility).
//
// Reference: TS-P7-08 AC-2, AC-3, AC-4, AC-5
func (c *Coordinator) InvokeActivation(ctx context.Context, framework, executable string, req contracts.ActivationRequest) (contracts.ActivationResult, error) {
	if err := c.ready(); err != nil {
		return contracts.ActivationResult{}, err
	}

	decl, ok := c.capabilities.Capabilities(framework)
	if !ok {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q: adapter for framework %q is not registered", req.Phase, framework)
	}
	if !containsName(decl.ActivationPhases, req.Phase) {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q: adapter for framework %q does not declare activation phase %q", req.Phase, framework, req.Phase)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q for framework %q: marshal request: %w", req.Phase, framework, err)
	}

	opts := []execution.RequestOption{
		execution.WithArgs([]string{contracts.CommandActivation, string(payload)}),
		execution.WithTimeout(adapterTimeout),
	}
	if req.Release.WorkingDir != "" {
		opts = append(opts, execution.WithWorkingDir(req.Release.WorkingDir))
	}
	request, err := execution.NewExecutionRequest(executable, opts...)
	if err != nil {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q for framework %q: %w", req.Phase, framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q for framework %q: adapter process failed: %s", req.Phase, framework, processFailure(result))
	}

	var parsed contracts.ActivationResult
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.ActivationResult{}, fmt.Errorf("cannot invoke activation phase %q for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", req.Phase, framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// InvokeVerification invokes one verification check of the adapter
// executable registered for framework. It first checks the framework's
// declared capabilities — an undeclared check is never invoked
// (TS-P7-08 AC-3). The request is passed as a single JSON argument; the
// adapter's JSON outcome on stdout is authoritative for the check
// result.
//
// Reference: TS-P7-08 AC-2, AC-3, AC-4, AC-5
func (c *Coordinator) InvokeVerification(ctx context.Context, framework, executable string, req contracts.VerificationRequest) (contracts.VerificationOutcome, error) {
	if err := c.ready(); err != nil {
		return contracts.VerificationOutcome{}, err
	}

	decl, ok := c.capabilities.Capabilities(framework)
	if !ok {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q: adapter for framework %q is not registered", req.Check, framework)
	}
	if !declaresCheck(decl.VerificationChecks, req.Check) {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q: adapter for framework %q does not declare verification check %q", req.Check, framework, req.Check)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q for framework %q: marshal request: %w", req.Check, framework, err)
	}

	request, err := execution.NewExecutionRequest(
		executable,
		execution.WithArgs([]string{contracts.CommandVerification, string(payload)}),
		execution.WithTimeout(adapterTimeout),
	)
	if err != nil {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q for framework %q: %w", req.Check, framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q for framework %q: adapter process failed: %s", req.Check, framework, processFailure(result))
	}

	var parsed contracts.VerificationOutcome
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.VerificationOutcome{}, fmt.Errorf("cannot invoke verification check %q for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", req.Check, framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// InvokeCapabilities requests the adapter executable's declared
// capabilities by dispatching the capabilities command
// (CommandCapabilities) with a CapabilityRequest payload naming the
// framework. The adapter returns a CapabilityResult JSON document on
// stdout, which the coordinator parses and returns; the caller is
// responsible for registering the returned declaration in the
// CapabilityRegistry (TS-P7-07). The request is passed as a single JSON
// argument (the Process Runner has no stdin channel); the adapter's JSON
// result on stdout is authoritative. The method contains no
// framework-specific values.
//
// Reference: TS-P7-07, TS-P7-08
func (c *Coordinator) InvokeCapabilities(ctx context.Context, framework, executable string) (contracts.CapabilityResult, error) {
	if err := c.ready(); err != nil {
		return contracts.CapabilityResult{}, err
	}

	payload, err := json.Marshal(contracts.CapabilityRequest{Framework: framework})
	if err != nil {
		return contracts.CapabilityResult{}, fmt.Errorf("cannot invoke capabilities for framework %q: marshal request: %w", framework, err)
	}

	request, err := execution.NewExecutionRequest(
		executable,
		execution.WithArgs([]string{contracts.CommandCapabilities, string(payload)}),
		execution.WithTimeout(adapterTimeout),
	)
	if err != nil {
		return contracts.CapabilityResult{}, fmt.Errorf("cannot invoke capabilities for framework %q: %w", framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.CapabilityResult{}, fmt.Errorf("cannot invoke capabilities for framework %q: adapter process failed: %s", framework, processFailure(result))
	}

	var parsed contracts.CapabilityResult
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.CapabilityResult{}, fmt.Errorf("cannot invoke capabilities for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// InvokeConfigExtension requests the adapter executable's declared
// configuration extension keys by dispatching the extension command
// (CommandConfigExtension) with a ConfigExtensionRequest payload naming
// the framework. The adapter returns a ConfigExtensionResult JSON
// document on stdout, which the coordinator parses and returns; the
// caller is responsible for registering the returned extension in the
// ConfigExtensionRegistry (TS-P7-03). The request is passed as a single
// JSON argument; the adapter's JSON result on stdout is authoritative.
// The method contains no framework-specific values.
//
// Reference: TS-P7-03, TS-P7-12
func (c *Coordinator) InvokeConfigExtension(ctx context.Context, framework, executable string) (contracts.ConfigExtensionResult, error) {
	if err := c.ready(); err != nil {
		return contracts.ConfigExtensionResult{}, err
	}

	payload, err := json.Marshal(contracts.ConfigExtensionRequest{Framework: framework})
	if err != nil {
		return contracts.ConfigExtensionResult{}, fmt.Errorf("cannot invoke config extension for framework %q: marshal request: %w", framework, err)
	}

	request, err := execution.NewExecutionRequest(
		executable,
		execution.WithArgs([]string{contracts.CommandConfigExtension, string(payload)}),
		execution.WithTimeout(adapterTimeout),
	)
	if err != nil {
		return contracts.ConfigExtensionResult{}, fmt.Errorf("cannot invoke config extension for framework %q: %w", framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.ConfigExtensionResult{}, fmt.Errorf("cannot invoke config extension for framework %q: adapter process failed: %s", framework, processFailure(result))
	}

	var parsed contracts.ConfigExtensionResult
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.ConfigExtensionResult{}, fmt.Errorf("cannot invoke config extension for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// InvokeConfigValidation dispatches the validate command
// (CommandConfigValidation) with a ConfigValidationRequest payload so the
// adapter validates its own extended configuration values (TS-P7-03 AC-4:
// the Core enforces namespace isolation and passes values through). The
// adapter's ConfigValidationResult JSON document on stdout is returned
// as-is. The method contains no framework-specific values.
//
// Reference: TS-P7-03, TS-P7-12
func (c *Coordinator) InvokeConfigValidation(ctx context.Context, framework, executable string, req contracts.ConfigValidationRequest) (contracts.ConfigValidationResult, error) {
	if err := c.ready(); err != nil {
		return contracts.ConfigValidationResult{}, err
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return contracts.ConfigValidationResult{}, fmt.Errorf("cannot invoke config validation for framework %q: marshal request: %w", framework, err)
	}

	request, err := execution.NewExecutionRequest(
		executable,
		execution.WithArgs([]string{contracts.CommandConfigValidation, string(payload)}),
		execution.WithTimeout(adapterTimeout),
	)
	if err != nil {
		return contracts.ConfigValidationResult{}, fmt.Errorf("cannot invoke config validation for framework %q: %w", framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.ConfigValidationResult{}, fmt.Errorf("cannot invoke config validation for framework %q: adapter process failed: %s", framework, processFailure(result))
	}

	var parsed contracts.ConfigValidationResult
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.ConfigValidationResult{}, fmt.Errorf("cannot invoke config validation for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// InvokeBuild invokes the adapter executable's build pipeline for
// framework: the build phases the adapter declares in its capability
// declaration (TS-P7-14). It first checks that the framework has a
// registered declaration — the Core builds only through registered
// adapters. There is no per-phase declaration check: the BuildRequest
// selects no phase, the adapter's build pipeline is invoked as a whole
// through the `build` command. The request is passed as a single JSON
// argument (the Process Runner has no stdin channel); the adapter's
// BuildResult JSON document on stdout is authoritative for the build
// outcome (005-adapter-command-contract §5).
//
// Reference: TS-P7-14, TS-P7-08
func (c *Coordinator) InvokeBuild(ctx context.Context, framework, executable string, req contracts.BuildRequest) (contracts.BuildResult, error) {
	if err := c.ready(); err != nil {
		return contracts.BuildResult{}, err
	}

	if _, ok := c.capabilities.Capabilities(framework); !ok {
		return contracts.BuildResult{}, fmt.Errorf("cannot invoke build: adapter for framework %q is not registered", framework)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return contracts.BuildResult{}, fmt.Errorf("cannot invoke build for framework %q: marshal request: %w", framework, err)
	}

	opts := []execution.RequestOption{
		execution.WithArgs([]string{contracts.CommandBuild, string(payload)}),
		execution.WithTimeout(buildTimeout),
	}
	if req.WorkingDir != "" {
		opts = append(opts, execution.WithWorkingDir(req.WorkingDir))
	}
	request, err := execution.NewExecutionRequest(executable, opts...)
	if err != nil {
		return contracts.BuildResult{}, fmt.Errorf("cannot invoke build for framework %q: %w", framework, err)
	}

	result := c.runner.Execute(ctx, request)
	if result.Status != execution.StatusSuccess {
		return contracts.BuildResult{}, fmt.Errorf("cannot invoke build for framework %q: adapter process failed: %s", framework, processFailure(result))
	}

	var parsed contracts.BuildResult
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return contracts.BuildResult{}, fmt.Errorf("cannot invoke build for framework %q: adapter returned invalid JSON result: %w (stdout=%q status=%s)", framework, err, result.Stdout, result.Status)
	}
	return parsed, nil
}

// ActivationPhases returns the activation phases the framework's adapter
// declares, in declaration order, so callers can plan which phases to
// run. ok=false when the framework has no registered capability
// declaration — a graceful "not found" (ADR-009 §9.7).
//
// Reference: TS-P7-07 AC-5, TS-P7-08
func (c *Coordinator) ActivationPhases(framework string) ([]string, bool) {
	if c == nil || c.capabilities == nil {
		return nil, false
	}
	decl, ok := c.capabilities.Capabilities(framework)
	if !ok {
		return nil, false
	}
	return decl.ActivationPhases, true
}

// VerificationChecks returns the verification checks the framework's
// adapter declares, in declaration order, so callers can plan which
// checks to run. ok=false when the framework has no registered
// capability declaration — a graceful "not found" (ADR-009 §9.7).
//
// Reference: TS-P7-07 AC-5, TS-P7-08
func (c *Coordinator) VerificationChecks(framework string) ([]contracts.VerificationCheck, bool) {
	if c == nil || c.capabilities == nil {
		return nil, false
	}
	decl, ok := c.capabilities.Capabilities(framework)
	if !ok {
		return nil, false
	}
	return decl.VerificationChecks, true
}

// BuildPhases returns the build phases the framework's adapter declares,
// in declaration order, so callers can plan the build pipeline.
// ok=false when the framework has no registered capability declaration —
// a graceful "not found" (ADR-009 §9.7).
//
// Reference: TS-P7-14, TS-P7-07 AC-5
func (c *Coordinator) BuildPhases(framework string) ([]string, bool) {
	if c == nil || c.capabilities == nil {
		return nil, false
	}
	decl, ok := c.capabilities.Capabilities(framework)
	if !ok {
		return nil, false
	}
	return decl.BuildPhases, true
}

// containsName reports whether names contains want.
func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// declaresCheck reports whether checks contains a check named want.
func declaresCheck(checks []contracts.VerificationCheck, want string) bool {
	for _, check := range checks {
		if check.Name == want {
			return true
		}
	}
	return false
}

// processFailure describes a failed subprocess execution for error
// messages: the termination status, the exit code, and stderr when the
// adapter produced any.
func processFailure(result execution.Result) string {
	detail := fmt.Sprintf("status=%s exit_code=%d", result.Status, result.ExitCode)
	if result.Stderr != "" {
		detail += fmt.Sprintf(" stderr=%q", result.Stderr)
	}
	return detail
}
