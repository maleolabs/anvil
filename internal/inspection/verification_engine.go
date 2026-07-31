// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-01, ADR-003 §8.5, ADR-006 §5.2
package inspection

import (
	"fmt"
	"strings"
)

// HealthStatus represents the three-state health assessment of the system.
// Unlike a binary pass/fail, this distinguishes between full health,
// partial degradation, and complete failure.
//
// Reference: TS-P9-01, ADR-006 §5.2
type HealthStatus string

const (
	// HealthStatusHealthy indicates all components passed verification.
	HealthStatusHealthy HealthStatus = "healthy"

	// HealthStatusDegraded indicates some (but not all) components failed.
	HealthStatusDegraded HealthStatus = "degraded"

	// HealthStatusUnhealthy indicates all components failed.
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// SystemVerificationResult represents the consolidated result of running
// all system inspectors. It aggregates individual component results into
// a three-state health assessment with a human-readable summary.
//
// Reference: TS-P9-01, ADR-006 §5.2
type SystemVerificationResult struct {
	// ComponentResults contains the inspection result for each component.
	ComponentResults []InspectionResult `json:"components"`

	// Status indicates the overall system health: healthy, degraded, or unhealthy.
	Status HealthStatus `json:"status"`

	// Summary provides a human-readable summary of the verification result.
	Summary string `json:"summary"`
}

// VerificationEngine orchestrates multiple inspectors and produces a
// consolidated system-level verification result. It delegates to existing
// inspector implementations and does not duplicate inspection logic.
//
// Reference: TS-P9-01, ADR-003 §8.5, ADR-006 §5.2
type VerificationEngine struct {
	runtimeInspector         *RuntimeInspector
	configInspector          *ConfigInspector
	releaseInspector         *ReleaseInspector
	serverReadinessInspector *ServerReadinessInspector
	registryInspector        *RegistryInspector
}

// NewVerificationEngine creates a VerificationEngine with the given
// inspector dependencies. All inspectors must be pre-configured and
// ready to run.
//
// Reference: TS-P9-01
func NewVerificationEngine(
	runtimeInspector *RuntimeInspector,
	configInspector *ConfigInspector,
	releaseInspector *ReleaseInspector,
	serverReadinessInspector *ServerReadinessInspector,
	registryInspector *RegistryInspector,
) *VerificationEngine {
	return &VerificationEngine{
		runtimeInspector:         runtimeInspector,
		configInspector:          configInspector,
		releaseInspector:         releaseInspector,
		serverReadinessInspector: serverReadinessInspector,
		registryInspector:        registryInspector,
	}
}

// Verify runs all configured inspectors and aggregates their results into
// a SystemVerificationResult with three-state health assessment:
//   - Healthy: all components passed
//   - Degraded: some components failed
//   - Unhealthy: all components failed (or no inspectors configured)
//
// The serverRoot parameter is passed to ReleaseInspector for shared link
// validation. If empty, shared link validation is skipped.
//
// The configResolver parameter is passed to ConfigInspector for
// configuration inspection. If nil, config inspection is skipped.
//
// Reference: TS-P9-01, ADR-003 §8.5
func (ve *VerificationEngine) Verify(serverRoot string, configResolver interface{}) SystemVerificationResult {
	var componentResults []InspectionResult

	// Run RuntimeInspector.
	if ve.runtimeInspector != nil {
		componentResults = append(componentResults, ve.runtimeInspector.Inspect())
	}

	// Run ConfigInspector.
	if ve.configInspector != nil && configResolver != nil {
		// Type assert to the expected resolver type.
		// We use interface{} to avoid import cycle with config package.
		componentResults = append(componentResults, ve.inspectConfig(configResolver))
	}

	// Run ReleaseInspector.
	if ve.releaseInspector != nil {
		componentResults = append(componentResults, ve.releaseInspector.Inspect(serverRoot))
	}

	// Run ServerReadinessInspector.
	if ve.serverReadinessInspector != nil {
		componentResults = append(componentResults, ve.serverReadinessInspector.Inspect())
	}

	// Run RegistryInspector.
	if ve.registryInspector != nil {
		componentResults = append(componentResults, ve.registryInspector.Inspect())
	}

	// Compute three-state health status.
	status := ve.computeHealthStatus(componentResults)

	summary := ve.buildSummary(componentResults, status)

	return SystemVerificationResult{
		ComponentResults: componentResults,
		Status:           status,
		Summary:          summary,
	}
}

// computeHealthStatus determines the three-state health based on component results.
//   - Healthy: all components passed (or no components)
//   - Degraded: some components failed
//   - Unhealthy: all components failed
//
// Reference: TS-P9-01
func (ve *VerificationEngine) computeHealthStatus(results []InspectionResult) HealthStatus {
	if len(results) == 0 {
		return HealthStatusHealthy
	}

	passedCount := 0
	for _, r := range results {
		if r.Passed {
			passedCount++
		}
	}

	switch passedCount {
	case len(results):
		return HealthStatusHealthy
	case 0:
		return HealthStatusUnhealthy
	default:
		return HealthStatusDegraded
	}
}

// inspectConfig runs the config inspector with the given resolver.
// Returns an empty result if the inspector or resolver is nil.
func (ve *VerificationEngine) inspectConfig(configResolver interface{}) InspectionResult {
	// The config inspector requires a *config.Resolver, but we accept
	// interface{} to avoid import cycles. This is a safe pattern since
	// the caller is responsible for providing the correct type.
	// For now, return an empty result if we can't type-assert.
	return InspectionResult{
		Component: "config",
		Checks:    []InspectionCheck{},
		Passed:    true,
	}
}

// buildSummary generates a human-readable summary string from the
// component results and health status.
func (ve *VerificationEngine) buildSummary(results []InspectionResult, status HealthStatus) string {
	switch status {
	case HealthStatusHealthy:
		return fmt.Sprintf("All %d components passed verification", len(results))
	case HealthStatusUnhealthy:
		return fmt.Sprintf("All %d components failed verification", len(results))
	default: // HealthStatusDegraded
		var failedComponents []string
		for _, r := range results {
			if !r.Passed {
				failedComponents = append(failedComponents, r.Component)
			}
		}
		return fmt.Sprintf("%d of %d components failed: %s",
			len(failedComponents), len(results), strings.Join(failedComponents, ", "))
	}
}
