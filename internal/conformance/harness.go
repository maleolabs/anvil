package conformance

import (
	"fmt"
	"sort"
)

// RuntimeFactory creates a fresh, isolated runtime under test for one
// conformance check. Every check runs against its own runtime instance,
// so checks are independent and re-checkable: no fixture state leaks
// between them, and any check can be re-run alone with the same result.
type RuntimeFactory func() (Runtime, error)

// Harness runs the conformance checks of the published contracts against
// a runtime's observable behavior, for the declared contract version.
//
// The checks are the corpus's contracts as behavior assertions
// (TS-013-05-03 §2): lifecycle-model (release state machine semantics),
// artifact-manifest (packaging, identity, embedded manifest,
// determinism), verification-contract (gate semantics, evidence
// requirements), and command-contract (runtime–standard exchange
// boundaries). The check set is bounded to the published contracts — no
// conformance requirement is invented (TS-013-05-03 §4).
type Harness struct {
	// factory creates the runtime under test per check.
	factory RuntimeFactory

	// checks holds every check, grouped by contract.
	checks map[string][]Check
}

// NewHarness creates a harness validating runtime behavior against the
// published contracts for the declared contract version
// (DeclaredContractVersion). factory must return a fresh, isolated
// runtime per invocation.
func NewHarness(factory RuntimeFactory) *Harness {
	h := &Harness{
		factory: factory,
		checks:  make(map[string][]Check),
	}

	h.addLifecycleModelChecks()
	h.addArtifactManifestChecks()
	h.addVerificationContractChecks()
	h.addCommandContractChecks()

	return h
}

// Run executes every check against the runtime and returns the full
// conformance report. ws provides the scratch space the checks use for
// their fixtures (source trees, artifact output, runtime roots).
func (h *Harness) Run(ws Workspace) *Report {
	report := &Report{ContractVersion: DeclaredContractVersion}

	names := make([]string, 0, len(h.checks))
	for name := range h.checks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		checks := h.checks[name]
		sortChecks(checks)

		contract := ContractResult{Name: name}
		for _, check := range checks {
			rt, err := h.factory()
			if err != nil {
				contract.Checks = append(contract.Checks, CheckResult{
					Check: check,
					Result: Result{
						Passed:   false,
						Observed: fmt.Sprintf("runtime under test could not be provisioned: %v", err),
					},
				})
				continue
			}

			// A panic from the runtime under test is recorded as a
			// failure — "runtime panicked: ..." — so one crashing check
			// cannot abort the rest of the report.
			res := runSafely(check, rt, ws)
			contract.Checks = append(contract.Checks, CheckResult{
				Check:  check,
				Result: *res,
			})
		}
		report.Contracts = append(report.Contracts, contract)
	}

	return report
}

// add registers one check under its contract.
func (h *Harness) add(c Check) {
	h.checks[c.Contract] = append(h.checks[c.Contract], c)
}

// runSafely executes one check against the runtime, converting a panic
// from the runtime under test into a failed result so the full report
// is still produced ("runtime panicked: ...").
func runSafely(check Check, rt Runtime, ws Workspace) (res *Result) {
	defer func() {
		if p := recover(); p != nil {
			res = Fail(fmt.Sprintf("runtime panicked during the check: %v", p))
		}
	}()
	return check.Run(rt, ws)
}
