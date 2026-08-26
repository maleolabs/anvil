package conformance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// conformanceFramework is the fixture framework identity the
// command-contract checks declare capability for. It is a fixture
// constant shared with the Anvil runtime binding.
const conformanceFramework = "conformance-framework"

// addCommandContractChecks registers the standard command-contract
// checks (command-contract.md; command-contract.schema.json).
//
// The checks assert the runtime's observable behavior at the
// runtime–standard exchange boundary: only declared capability is
// invoked (C1), declared order is the executed order (C2), the exchange
// is structured JSON over a subprocess boundary (§5), rollback executes
// through the same declared-capability rule (C4), and an empty
// declaration is legal — the runtime proceeds with its generic
// operations (§4.1).
func (h *Harness) addCommandContractChecks() {
	const contract = "command-contract"

	// C-01: Only declared capability is invoked (C1): the runtime
	// invokes only declared capability; undeclared capability is never
	// called (command-contract.md §4.6 C1; Manifesto §7) — an
	// invocation that is not declared is rejected and never reaches the
	// subprocess boundary.
	h.add(Check{
		ID:          "C-01",
		Contract:    contract,
		Requirement: "command-contract.md §4.6 C1",
		Title:       "only declared capability is invoked",
		Expected:    "Invoking an activation phase or verification check that the standard did not declare is rejected with an error, and no subprocess invocation occurs for it — undeclared capability is never called.",
		Run: func(rt Runtime, ws Workspace) *Result {
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{
				ActivationPhases:   []string{"migrate"},
				VerificationChecks: []string{"vendor-present"},
			}); err != nil {
				return Fail(fmt.Sprintf("fixture: registering the declared capability: %v", err))
			}

			if err := rt.InvokePhase(conformanceFramework, "undeclared-phase", "activate"); err == nil {
				return Fail("invoking an undeclared activation phase succeeded — undeclared capability must never be called (C1)")
			}
			if err := rt.InvokeCheck(conformanceFramework, "undeclared-check"); err == nil {
				return Fail("invoking an undeclared verification check succeeded — undeclared capability must never be called (C1)")
			}

			for _, call := range rt.SubprocessCalls() {
				payload := lastArg(call.Args)
				if strings.Contains(payload, "undeclared-phase") || strings.Contains(payload, "undeclared-check") {
					return Fail(fmt.Sprintf("a subprocess invocation occurred for undeclared capability: %q %q (C1)", call.Command, call.Args))
				}
			}
			return Pass()
		},
	})

	// C-02: Declared order is the executed order (C2): declared phases
	// run in the declared sequence (command-contract.md §4.6 C2; §4.2).
	// The check asserts both halves: the runtime exposes the declared
	// sequence, and the sequence actually executed at the subprocess
	// boundary matches it.
	h.add(Check{
		ID:          "C-02",
		Contract:    contract,
		Requirement: "command-contract.md §4.6 C2",
		Title:       "declared order is the executed order",
		Expected:    "The runtime exposes the declared activation phases in the declared sequence, and invoking them executes them in exactly that declared order — the subprocess boundary records the declared sequence.",
		Run: func(rt Runtime, ws Workspace) *Result {
			declared := []string{"migrate", "cache-warm", "queue-restart"}
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{ActivationPhases: declared}); err != nil {
				return Fail(fmt.Sprintf("fixture: registering the declared capability: %v", err))
			}

			phases, ok := rt.DeclaredPhases(conformanceFramework)
			if !ok {
				return Fail("the declared capability surface is not exposed by the runtime")
			}
			if len(phases) != len(declared) {
				return Fail(fmt.Sprintf("declared phases = %v, want %v (C2)", phases, declared))
			}
			for i := range declared {
				if phases[i] != declared[i] {
					return Fail(fmt.Sprintf("declared phases = %v, want %v — the declared sequence must be preserved (C2)", phases, declared))
				}
			}

			// Execute the declared phases and verify the executed order
			// recorded at the subprocess boundary matches the declared
			// order (C2: declared order is the executed order).
			for _, phase := range declared {
				if err := rt.InvokePhase(conformanceFramework, phase, "activate"); err != nil {
					return Fail(fmt.Sprintf("invoking declared phase %q returned an error: %v", phase, err))
				}
			}

			var executed []string
			for _, call := range rt.SubprocessCalls() {
				if call.Command != "activate" {
					continue
				}
				var payload map[string]any
				if err := json.Unmarshal([]byte(lastArg(call.Args)), &payload); err != nil {
					continue
				}
				if phase, _ := payload["phase"].(string); phase != "" {
					executed = append(executed, phase)
				}
			}
			if len(executed) != len(declared) {
				return Fail(fmt.Sprintf("executed phases = %v, want %v — every declared phase must execute exactly once (C2)", executed, declared))
			}
			for i := range declared {
				if executed[i] != declared[i] {
					return Fail(fmt.Sprintf("executed phases = %v, want %v — declared order must be the executed order (C2)", executed, declared))
				}
			}
			return Pass()
		},
	})

	// C-03: The exchange is structured JSON over a subprocess boundary:
	// the runtime invokes the standard as a standalone executable with a
	// JSON payload and parses the standard's JSON result from its
	// output (command-contract.md §5; ADR-021 §3.4).
	h.add(Check{
		ID:          "C-03",
		Contract:    contract,
		Requirement: "command-contract.md §5 (subprocess JSON contract)",
		Title:       "the exchange is structured JSON over a subprocess boundary",
		Expected:    "A declared phase invocation happens at the subprocess boundary: the invocation carries the contract command name and a JSON payload naming the phase, and the runtime accepts the JSON result the subprocess returned.",
		Run: func(rt Runtime, ws Workspace) *Result {
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{ActivationPhases: []string{"migrate"}}); err != nil {
				return Fail(fmt.Sprintf("fixture: registering the declared capability: %v", err))
			}

			if err := rt.InvokePhase(conformanceFramework, "migrate", "activate"); err != nil {
				return Fail(fmt.Sprintf("invoking a declared phase returned an error: %v", err))
			}

			calls := rt.SubprocessCalls()
			if len(calls) == 0 {
				return Fail("no subprocess invocation was observed at the exchange boundary (§5)")
			}
			call := calls[len(calls)-1]
			if call.Command != "activate" {
				return Fail(fmt.Sprintf("subprocess command = %q, want %q (the lifecycle-phase exchange command)", call.Command, "activate"))
			}
			payloadArg := lastArg(call.Args)
			if payloadArg == "" {
				return Fail(fmt.Sprintf("subprocess args = %v, want the command plus a JSON payload (the structured exchange, §5)", call.Args))
			}

			var payload map[string]any
			if err := json.Unmarshal([]byte(payloadArg), &payload); err != nil {
				return Fail(fmt.Sprintf("the payload passed to the subprocess is not valid JSON: %v", err))
			}
			if phase, _ := payload["phase"].(string); phase != "migrate" {
				return Fail(fmt.Sprintf("the JSON payload names phase %q, want %q", phase, "migrate"))
			}

			var result map[string]any
			if err := json.Unmarshal([]byte(call.Stdout), &result); err != nil {
				return Fail(fmt.Sprintf("the runtime did not accept the subprocess's JSON result (stdout %q): %v", call.Stdout, err))
			}
			if ok, _ := result["success"].(bool); !ok {
				return Fail(fmt.Sprintf("the JSON result does not carry success=true: %v", result))
			}
			return Pass()
		},
	})

	// C-04: Rollback executes the standard's declared rollback semantics
	// through the same declared-capability rule as activation
	// (command-contract.md §4.3): only declared phases receive the
	// rollback operation.
	h.add(Check{
		ID:          "C-04",
		Contract:    contract,
		Requirement: "command-contract.md §4.3 (rollback exchange)",
		Title:       "rollback uses the same declared-capability rule as activation",
		Expected:    "The rollback operation is exchanged for declared phases through the same declared-capability rule as activation; an undeclared phase is rejected for rollback exactly as for activation.",
		Run: func(rt Runtime, ws Workspace) *Result {
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{ActivationPhases: []string{"migrate"}}); err != nil {
				return Fail(fmt.Sprintf("fixture: registering the declared capability: %v", err))
			}

			if err := rt.InvokePhase(conformanceFramework, "migrate", "rollback"); err != nil {
				return Fail(fmt.Sprintf("the declared phase received no rollback operation: %v", err))
			}
			if err := rt.InvokePhase(conformanceFramework, "undeclared", "rollback"); err == nil {
				return Fail("an undeclared phase received a rollback operation — rollback must use the same declared-capability rule as activation (§4.3)")
			}
			return Pass()
		},
	})

	// C-06: Irreversibility never blocks rollback (C4): a phase may be
	// declared irreversible — the declaration documents the
	// irreversibility — and rollback proceeds regardless
	// (command-contract.md §4.3, §4.6 C4; lifecycle-model.md §5.2).
	h.add(Check{
		ID:          "C-06",
		Contract:    contract,
		Requirement: "command-contract.md §4.3, §4.6 C4",
		Title:       "irreversibility never blocks rollback",
		Expected:    "A phase declared irreversible still receives its rollback operation: the runtime performs the rollback for the declared phase — irreversibility never blocks rollback (C4).",
		Run: func(rt Runtime, ws Workspace) *Result {
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{
				ActivationPhases:   []string{"migrate"},
				IrreversiblePhases: []string{"migrate"},
			}); err != nil {
				return Fail(fmt.Sprintf("fixture: registering the declared capability: %v", err))
			}

			if err := rt.InvokePhase(conformanceFramework, "migrate", "rollback"); err != nil {
				return Fail(fmt.Sprintf("rollback was blocked for a phase declared irreversible — irreversibility never blocks rollback (C4): %v", err))
			}

			calls := rt.SubprocessCalls()
			if len(calls) == 0 {
				return Fail("no rollback invocation was observed for the irreversible phase (C4)")
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(lastArg(calls[len(calls)-1].Args)), &payload); err != nil {
				return Fail(fmt.Sprintf("the rollback invocation carries no JSON payload: %v", err))
			}
			if op, _ := payload["operation"].(string); op != "rollback" {
				return Fail(fmt.Sprintf("the last invocation for the irreversible phase carries operation %q, want %q — the rollback must be performed (C4)", op, "rollback"))
			}
			return Pass()
		},
	})

	// C-05: A standard may declare nothing in a category, or nothing at
	// all; the runtime proceeds with its generic operations
	// (command-contract.md §4.1; ADR-026 §3): an empty declaration is
	// legal and invokes nothing (declared-surface bound, no-op not open
	// door).
	h.add(Check{
		ID:          "C-05",
		Contract:    contract,
		Requirement: "command-contract.md §4.1 (declared-surface bound)",
		Title:       "an empty declaration is legal and invokes nothing",
		Expected:    "Registering an empty capability declaration succeeds, exposes no phases, and triggers no capability invocation — the runtime proceeds with its generic operations.",
		Run: func(rt Runtime, ws Workspace) *Result {
			if err := rt.RegisterCapability(conformanceFramework, CapabilityDeclaration{}); err != nil {
				return Fail(fmt.Sprintf("an empty declaration was rejected: %v — declaring nothing is legal (§4.1)", err))
			}

			phases, ok := rt.DeclaredPhases(conformanceFramework)
			if !ok {
				return Fail("the declared capability surface is not exposed by the runtime")
			}
			if len(phases) != 0 {
				return Fail(fmt.Sprintf("an empty declaration exposed phases %v — declaring nothing must declare nothing", phases))
			}

			for _, call := range rt.SubprocessCalls() {
				return Fail(fmt.Sprintf("an empty declaration still produced a subprocess invocation: %q %q", call.Command, call.Args))
			}
			return Pass()
		},
	})
}

// lastArg returns the trailing argument of the invocation, or "".
func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
