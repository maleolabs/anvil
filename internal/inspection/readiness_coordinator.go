// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-02, ADR-003 §8.5, ADR-006 §5.2
package inspection

import (
	"fmt"
)

// ReadinessCoordinatorResult represents the consolidated readiness
// assessment from the ReadinessCoordinator. It wraps the verification
// engine output with readiness semantics and provides a list of blockers.
//
// Reference: TS-P9-02, ADR-006 §5.2
type ReadinessCoordinatorResult struct {
	// Ready indicates whether the system is ready for deployment operations.
	// True only if ALL components passed verification.
	Ready bool `json:"ready"`

	// Components contains the inspection result for each component.
	Components []InspectionResult `json:"components"`

	// Blockers lists actionable failure descriptions for each failed check.
	// Empty when Ready=true.
	Blockers []string `json:"blockers,omitempty"`

	// Summary provides a human-readable summary of the readiness assessment.
	Summary string `json:"summary"`
}

// ReadinessCoordinator wraps the VerificationEngine with readiness semantics
// and provides a single "ready/not ready" assessment. It extracts actionable
// blockers from failed checks.
//
// Reference: TS-P9-02, ADR-003 §8.5, ADR-006 §5.2
type ReadinessCoordinator struct {
	engine *VerificationEngine
}

// NewReadinessCoordinator creates a ReadinessCoordinator that wraps the
// given VerificationEngine.
//
// Reference: TS-P9-02
func NewReadinessCoordinator(engine *VerificationEngine) *ReadinessCoordinator {
	return &ReadinessCoordinator{engine: engine}
}

// CheckReadiness runs the verification engine and produces a readiness
// assessment. If all components pass, Ready=true with no blockers. If any
// component fails, Ready=false with actionable blocker descriptions.
//
// The configResolver parameter is passed through to the verification
// engine.
//
// Reference: TS-P9-02, ADR-003 §8.5
func (rc *ReadinessCoordinator) CheckReadiness(configResolver interface{}) ReadinessCoordinatorResult {
	verificationResult := rc.engine.Verify(configResolver)

	if verificationResult.Status == HealthStatusHealthy {
		return ReadinessCoordinatorResult{
			Ready:      true,
			Components: verificationResult.ComponentResults,
			Blockers:   nil,
			Summary:    "System is ready for deployment operations",
		}
	}

	blockers := extractBlockers(verificationResult.ComponentResults)

	return ReadinessCoordinatorResult{
		Ready:      false,
		Components: verificationResult.ComponentResults,
		Blockers:   blockers,
		Summary:    fmt.Sprintf("System is not ready: %d blocker(s) found", len(blockers)),
	}
}

// extractBlockers extracts actionable failure descriptions from failed
// inspection checks. Each blocker describes a specific failure with
// component context.
func extractBlockers(components []InspectionResult) []string {
	var blockers []string

	for _, component := range components {
		if component.Passed {
			continue
		}
		for _, check := range component.Checks {
			if !check.Passed {
				blocker := fmt.Sprintf("[%s] %s: %s",
					component.Component, check.Name, check.Details)
				blockers = append(blockers, blocker)
			}
		}
	}

	return blockers
}
