// Package deployment defines the Deployment bounded context for Anvil.
//
// The Coordinator orchestrates deployment workflows by delegating to
// Transport for delivery and to the Server Runtime for installation,
// activation, and rollback. It does not maintain independent Release
// State or manipulate Runtime paths (ADR-015).
//
// Reference: TS-P10-04, EPIC-010, ADR-015
package deployment

import (
	"encoding/json"
	"fmt"
)

// DeploymentError reports a failure during a deployment workflow step.
// It preserves the step name and wraps the underlying cause so callers
// can determine which operation failed without parsing error messages.
//
// Reference: TS-P10-04 AC-5
type DeploymentError struct {
	// Step identifies which workflow step failed (negotiate, deliver,
	// install, activate, rollback).
	Step string `json:"step"`

	// Reason describes the failure.
	Reason string `json:"reason"`

	// Err is the underlying error that caused the failure.
	Err error `json:"-"`
}

// Error implements the error interface for DeploymentError.
func (e *DeploymentError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("deployment step %q failed: %s: %v", e.Step, e.Reason, e.Err)
	}
	return fmt.Sprintf("deployment step %q failed: %s", e.Step, e.Reason)
}

// Unwrap returns the underlying error for use with errors.Is/errors.As.
func (e *DeploymentError) Unwrap() error {
	return e.Err
}

// DeploymentStepResult reports the outcome of an individual deployment
// workflow step. This enables partial outcome reporting — callers can
// inspect which steps succeeded and which failed without parsing the
// error type.
//
// Reference: TS-P10-04 AC-5
type DeploymentStepResult struct {
	// Step identifies the workflow step.
	Step string `json:"step"`

	// Success indicates whether this step completed successfully.
	Success bool `json:"success"`

	// Message provides a human-readable description of the outcome.
	Message string `json:"message,omitempty"`
}

// Coordinator orchestrates a deployment workflow: negotiate compatibility,
// deliver the artifact via Transport, and delegate lifecycle operations
// to the Server Runtime through published contracts.
//
// Coordinator does NOT maintain independent Release State or manipulate
// Runtime paths directly (ADR-015). It coordinates; it does not own
// lifecycle semantics.
//
// Reference: TS-P10-04, ADR-015
type Coordinator struct {
	// transport delivers artifacts to deployment targets.
	transport Transport
}

// NewCoordinator creates a Coordinator with the given Transport.
// The Coordinator uses the Transport to deliver artifacts and
// delegates lifecycle operations to the Server Runtime through
// published contracts.
//
// Reference: TS-P10-04
func NewCoordinator(transport Transport) *Coordinator {
	return &Coordinator{
		transport: transport,
	}
}

// DeployResult aggregates the results of a full deployment workflow.
// Each step result is reported independently, enabling partial outcome
// reporting without duplicating Runtime lifecycle semantics.
//
// Reference: TS-P10-04 AC-5
type DeployResult struct {
	// Steps contains the outcome of each workflow step.
	Steps []DeploymentStepResult `json:"steps"`

	// TransportResult is the result from the artifact delivery step,
	// if it was attempted. Nil if delivery was not reached due to a
	// prior step failure.
	TransportResult *TransportResult `json:"transport_result,omitempty"`

	// OverallFailure describes the first failure that caused the
	// deployment to stop, if any. Steps after the failure are not
	// attempted.
	OverallFailure string `json:"overall_failure,omitempty"`
}

// addStep records a step result in the DeployResult.
func (r *DeployResult) addStep(step string, success bool, message string) {
	r.Steps = append(r.Steps, DeploymentStepResult{
		Step:    step,
		Success: success,
		Message: message,
	})
}

// manifestProjectID is a minimal projection of the artifact manifest
// containing only the project identity field. The Coordinator reads
// only what it needs to delegate to Runtime — it does not import the
// full artifact package to preserve domain boundaries (ADR-015).
type manifestProjectID struct {
	ProjectID string `json:"project_id"`
}

// Deploy executes the full deployment workflow:
//
//  1. Negotiate — Check target compatibility with the artifact manifest.
//  2. Deliver — Transport the artifact to the target (via Transport).
//  3. Install — Delegated to Server Runtime (caller-provided function).
//  4. Activate — Delegated to Server Runtime (caller-provided function).
//
// Each step is independent. If a step fails, subsequent steps are not
// attempted, and the result reports the failure point.
//
// The installFn and activateFn parameters allow callers to inject their
// Runtime-specific implementations, keeping the Coordinator free of
// Runtime dependencies (ADR-015). This is the published contract boundary.
//
// Reference: TS-P10-04 AC-1, AC-2, AC-4, AC-5, ADR-015
func (c *Coordinator) Deploy(
	payload ArtifactPayload,
	target Target,
	installFn func(projectID, artifactPath string) error,
	activateFn func(projectID, releaseID string) error,
) *DeployResult {
	result := &DeployResult{}

	// Step 1: Negotiate — check target compatibility with manifest.
	negResult, err := Negotiate(payload, target)
	if err != nil {
		result.addStep("negotiate", false, fmt.Sprintf("negotiation error: %v", err))
		result.OverallFailure = fmt.Sprintf("negotiate: %v", err)
		return result
	}
	if !negResult.Compatible {
		result.addStep("negotiate", false, negResult.Reason)
		result.OverallFailure = fmt.Sprintf("negotiate: %s", negResult.Reason)
		return result
	}
	result.addStep("negotiate", true, negResult.Reason)

	// Step 2: Deliver — transport the artifact to the target.
	transportResult, err := c.transport.Deliver(payload, target)
	if err != nil {
		result.addStep("deliver", false, fmt.Sprintf("transport failed: %v", err))
		result.OverallFailure = fmt.Sprintf("deliver: %v", err)
		return result
	}
	result.TransportResult = transportResult
	result.addStep("deliver", true, "artifact delivered successfully")

	// Step 3: Install — delegate to Server Runtime via caller-provided function.
	// The Runtime validates the artifact, stores it, and creates a Ready Release.
	if installFn != nil {
		projectID, manifestErr := extractProjectID(payload.ManifestContent)
		if manifestErr != nil {
			result.addStep("install", false, fmt.Sprintf("parse manifest: %v", manifestErr))
			result.OverallFailure = fmt.Sprintf("install: %v", manifestErr)
			return result
		}

		if err := installFn(projectID, payload.Path); err != nil {
			result.addStep("install", false, fmt.Sprintf("runtime install failed: %v", err))
			result.OverallFailure = fmt.Sprintf("install: %v", err)
			return result
		}
		result.addStep("install", true, "artifact installed as Ready release")
	} else {
		result.addStep("install", true, "install skipped (no install function provided)")
	}

	// Step 4: Activate — delegate to Server Runtime via caller-provided function.
	if activateFn != nil && result.OverallFailure == "" {
		projectID, manifestErr := extractProjectID(payload.ManifestContent)
		if manifestErr != nil {
			result.addStep("activate", false, fmt.Sprintf("parse manifest: %v", manifestErr))
			result.OverallFailure = fmt.Sprintf("activate: %v", manifestErr)
			return result
		}

		// The releaseID would come from the install result in a real workflow.
		// Per AC-2, activation targets an existing Release identity. The caller
		// is responsible for passing the correct releaseID via activateFn closure.
		if err := activateFn(projectID, ""); err != nil {
			result.addStep("activate", false, fmt.Sprintf("runtime activate failed: %v", err))
			result.OverallFailure = fmt.Sprintf("activate: %v", err)
			return result
		}
		result.addStep("activate", true, "release activated successfully")
	} else if activateFn == nil {
		result.addStep("activate", true, "activate skipped (no activate function provided)")
	}

	return result
}

// Rollback executes a rollback via the caller-provided function. This is
// a separate method because rollback is a distinct workflow that does not
// follow the negotiate → deliver → install → activate sequence.
//
// Reference: TS-P10-04 AC-3, AC-4, AC-5
func (c *Coordinator) Rollback(
	payload ArtifactPayload,
	target Target,
	rollbackFn func(projectID string) error,
) *DeployResult {
	result := &DeployResult{}

	// Step 1: Negotiate (pre-check).
	negResult, err := Negotiate(payload, target)
	if err != nil {
		result.addStep("negotiate", false, fmt.Sprintf("negotiation error: %v", err))
		result.OverallFailure = fmt.Sprintf("negotiate: %v", err)
		return result
	}
	if !negResult.Compatible {
		result.addStep("negotiate", false, negResult.Reason)
		result.OverallFailure = fmt.Sprintf("negotiate: %s", negResult.Reason)
		return result
	}
	result.addStep("negotiate", true, negResult.Reason)

	// Step 2: Rollback — delegate to Server Runtime.
	if rollbackFn != nil {
		projectID, manifestErr := extractProjectID(payload.ManifestContent)
		if manifestErr != nil {
			result.addStep("rollback", false, fmt.Sprintf("parse manifest: %v", manifestErr))
			result.OverallFailure = fmt.Sprintf("rollback: %v", manifestErr)
			return result
		}

		if err := rollbackFn(projectID); err != nil {
			result.addStep("rollback", false, fmt.Sprintf("runtime rollback failed: %v", err))
			result.OverallFailure = fmt.Sprintf("rollback: %v", err)
			return result
		}
		result.addStep("rollback", true, "release rolled back successfully")
	} else {
		result.addStep("rollback", true, "rollback skipped (no rollback function provided)")
	}

	return result
}

// extractProjectID unmarshals the manifest JSON content to extract the
// project identity field. It uses a minimal projection to avoid importing
// the full artifact package (ADR-015). The Coordinator only needs project
// identity for Runtime delegation — it does not need the full Manifest type.
//
// Reference: ADR-015, Decision 006
func extractProjectID(manifestContent []byte) (string, error) {
	var m manifestProjectID
	if err := json.Unmarshal(manifestContent, &m); err != nil {
		return "", fmt.Errorf("parse manifest for project identity: %w", err)
	}
	if m.ProjectID == "" {
		return "", fmt.Errorf("manifest missing project_id field")
	}
	return m.ProjectID, nil
}
