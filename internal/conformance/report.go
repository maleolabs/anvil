package conformance

import (
	"fmt"
	"sort"
	"strings"
)

// Result is the outcome of one conformance check.
type Result struct {
	// Passed reports whether the runtime's observed behavior conforms.
	Passed bool

	// Observed describes the observed deviation when the check failed.
	// Empty on pass.
	Observed string
}

// Pass returns a passing result.
func Pass() *Result {
	return &Result{Passed: true}
}

// Fail returns a failing result describing the observed deviation from
// the contract's expected behavior.
func Fail(observed string) *Result {
	return &Result{Passed: false, Observed: observed}
}

// Check is one conformance check: an executable assertion of runtime
// behavior against one contract rule.
type Check struct {
	// ID is the stable check identifier within its contract
	// (e.g. "L-01").
	ID string

	// Contract is the corpus contract this check belongs to
	// (e.g. "lifecycle-model").
	Contract string

	// Requirement cites the contract clause this check asserts
	// (e.g. "lifecycle-model §6.3").
	Requirement string

	// Title is a short statement of the asserted behavior.
	Title string

	// Expected is the expected behavior, stated from the contract.
	Expected string

	// Run executes the check against the runtime and returns the
	// outcome.
	Run func(rt Runtime, ws Workspace) *Result
}

// CheckResult is the recorded outcome of one check.
type CheckResult struct {
	Check
	Result
}

// Failure renders one failed check with the actionable diagnostic
// triad: contract, expected behavior, observed deviation.
func (c CheckResult) Failure() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (%s)\n", c.ID, c.Title, c.Requirement)
	fmt.Fprintf(&b, "  Expected: %s\n", c.Expected)
	fmt.Fprintf(&b, "  Observed: %s\n", c.Observed)
	return b.String()
}

// PassLine renders the check's pass summary.
func (c CheckResult) PassLine() string {
	return fmt.Sprintf("%s %s: %s (%s)", passMark, c.ID, c.Title, c.Requirement)
}

const (
	passMark = "PASS"
	failMark = "FAIL"
)

// ContractResult groups the outcomes of one contract's checks.
type ContractResult struct {
	// Name is the corpus contract name (e.g. "lifecycle-model").
	Name string

	// Checks are the check outcomes, ordered by ID.
	Checks []CheckResult
}

// Passed reports whether every check of this contract passed.
func (c ContractResult) Passed() bool {
	for _, chk := range c.Checks {
		if !chk.Passed {
			return false
		}
	}
	return true
}

// Failures returns the failed checks of this contract.
func (c ContractResult) Failures() []CheckResult {
	var out []CheckResult
	for _, chk := range c.Checks {
		if !chk.Passed {
			out = append(out, chk)
		}
	}
	return out
}

// Report is the full, re-checkable conformance report for the declared
// contract version.
type Report struct {
	// ContractVersion is the declared contract version validated
	// (version-line.md; ADR-024 §3.1).
	ContractVersion string

	// Contracts are the per-contract results, ordered by contract name.
	Contracts []ContractResult
}

// Passed reports whether every check of every contract passed.
func (r *Report) Passed() bool {
	for _, c := range r.Contracts {
		if !c.Passed() {
			return false
		}
	}
	return true
}

// Failures returns every failed check across all contracts.
func (r *Report) Failures() []CheckResult {
	var out []CheckResult
	for _, c := range r.Contracts {
		out = append(out, c.Failures()...)
	}
	return out
}

// Totals returns the total and passed check counts across all
// contracts.
func (r *Report) Totals() (total, passed int) {
	for _, c := range r.Contracts {
		total += len(c.Checks)
		for _, chk := range c.Checks {
			if chk.Passed {
				passed++
			}
		}
	}
	return total, passed
}

// String renders the complete report: the declared contract version,
// every contract with its check outcomes, and a summary. Failed checks
// carry the expected/observed diagnostic.
func (r *Report) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "CONFORMANCE REPORT — delivery lifecycle specification, contract version %s\n", r.ContractVersion)
	fmt.Fprintf(&b, "Contracts covered: %d\n\n", len(r.Contracts))

	for _, contract := range r.Contracts {
		fmt.Fprintf(&b, "Contract: %s (%s)\n", contract.Name, r.ContractVersion)
		if !contract.Passed() {
			fmt.Fprintf(&b, "  FAILED — %d of %d checks failed\n", len(contract.Failures()), len(contract.Checks))
		}
		for _, chk := range contract.Checks {
			if chk.Passed {
				fmt.Fprintf(&b, "  %s\n", chk.PassLine())
			} else {
				fmt.Fprintf(&b, "  %s %s: %s (%s)\n", failMark, chk.ID, chk.Title, chk.Requirement)
				fmt.Fprintf(&b, "    Expected: %s\n", chk.Expected)
				fmt.Fprintf(&b, "    Observed: %s\n", chk.Observed)
			}
		}
		b.WriteString("\n")
	}

	total, passed := r.Totals()
	if r.Passed() {
		fmt.Fprintf(&b, "SUMMARY: PASS — %d contracts, %d checks: all passed (%d/%d)\n", len(r.Contracts), total, passed, total)
	} else {
		fmt.Fprintf(&b, "SUMMARY: FAIL — %d contracts, %d checks: %d passed, %d failed\n", len(r.Contracts), total, passed, total-passed)
	}
	return b.String()
}

// sortChecks orders checks by their stable ID.
func sortChecks(checks []Check) {
	sort.Slice(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })
}
