// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// ── Architectural Context Classification (ST-P9-06, ADR-015) ──────────
//
// The four-domain architecture (ADR-015) organizes Anvil into Project →
// Artifact → Deployment → Server Runtime bounded contexts. Diagnostic
// findings must be attributed to the context that owns the failure so that
// the operator knows where to act and which Epic owns the resolution.
//
// The classifier maps every DiagnosticIssue to an ArchitecturalContext
// based on the inspected component and the evidence recorded in the issue
// (component, location, description). Attribution is evidence-based: a
// failure is never attributed to repository source (Development) or
// transport (Deployment) without direct evidence from the issue record.
// Adapter-related data remains generic in MVP because no adapter component
// is inspected by the diagnostic engines.
//
// Reference: ST-P9-06, ADR-015, EPIC-009 §8.3
package inspection

import "strings"

// ArchitecturalContext identifies the bounded architectural context that
// owns a diagnostic finding, per the four-domain architecture (ADR-015).
//
// Reference: ST-P9-06, ADR-015
type ArchitecturalContext string

const (
	// ContextDevelopment identifies Development/CI configuration failures:
	// project configuration (anvil.yaml), CI configuration, and repository
	// source validation. Owned by the Project domain (EPIC-001/EPIC-002).
	ContextDevelopment ArchitecturalContext = "development"

	// ContextArtifact identifies Artifact metadata and integrity failures:
	// artifact presence, manifest identity, and verification status.
	// Owned by the Artifact domain (EPIC-003). Distinct from Release
	// identity — an artifact failure is never reported as a Release failure.
	ContextArtifact ArchitecturalContext = "artifact"

	// ContextRelease identifies Release lifecycle state failures: release
	// directories, release identity, and release infrastructure.
	// Owned by the Release domain (EPIC-004).
	ContextRelease ArchitecturalContext = "release"

	// ContextDeployment identifies Deployment orchestration failures:
	// transport, targets, and delivery orchestration. No inspector
	// produces Deployment issues in MVP — the context is reserved for
	// EPIC-010 and is never attributed without evidence.
	ContextDeployment ArchitecturalContext = "deployment"

	// ContextServerRuntime identifies Server Runtime state failures:
	// runtime environment, registry, server configuration, and installed
	// release filesystem state. Owned by the Server Runtime domain
	// (EPIC-005).
	ContextServerRuntime ArchitecturalContext = "server_runtime"
)

// ContextualIssue pairs a diagnostic issue with the architectural context
// it was classified into. The pairing preserves the issue record so that
// downstream consumers can render the context alongside the finding.
//
// Reference: ST-P9-06
type ContextualIssue struct {
	// Issue is the original diagnostic finding.
	Issue DiagnosticIssue `json:"issue"`

	// Context is the architectural context the issue was classified into.
	Context ArchitecturalContext `json:"context"`
}

// ClassifyIssues classifies every issue into its architectural context.
// The classification is deterministic and preserves the input order, so
// contextual issues align one-to-one with the original issue records.
//
// Reference: ST-P9-06
func ClassifyIssues(issues []DiagnosticIssue) []ContextualIssue {
	contextual := make([]ContextualIssue, 0, len(issues))

	for _, issue := range issues {
		contextual = append(contextual, ContextualIssue{
			Issue:   issue,
			Context: ClassifyContext(issue),
		})
	}

	return contextual
}

// ClassifyContext determines the architectural context of a single
// diagnostic issue using the inspected component and the evidence recorded
// in the issue.
//
// Mapping rules (evidence-based, per ADR-015 and EPIC-009 §8.3):
//
//   - "config" component → ContextDevelopment. The config inspector
//     examines project configuration (completeness, validity, resolution),
//     which is Development/CI configuration owned by the Project domain.
//   - "runtime" component → ContextServerRuntime. Runtime checks examine
//     the server-side runtime filesystem (active symlink, directories,
//     shared resources, runtime config).
//   - "server" component → ContextServerRuntime. Server checks examine the
//     registry store, server config, and project registries — Server
//     Runtime state owned by EPIC-005.
//   - "release" component → evidence-based split:
//   - artifact_presence findings → ContextArtifact. Artifact identity and
//     presence are owned by the Artifact domain (EPIC-003) and are kept
//     distinct from Release identity (ADR-015: Artifact and Release
//     terminology must remain distinct).
//   - other release findings (release_directory, shared_links) →
//     ContextRelease.
//   - Unknown components → ContextServerRuntime (conservative default).
//     The classifier never attributes a failure to repository source
//     (Development) or transport (Deployment) without evidence; adapter
//     data stays generic in MVP because no adapter component is inspected.
//
// Reference: ST-P9-06, ADR-015, EPIC-009 §8.3
func ClassifyContext(issue DiagnosticIssue) ArchitecturalContext {
	switch issue.Component {
	case "config":
		return ContextDevelopment

	case "runtime":
		return ContextServerRuntime

	case "server":
		return ContextServerRuntime

	case "release":
		if strings.HasPrefix(issue.Location, "artifact_presence") {
			return ContextArtifact
		}
		return ContextRelease

	default:
		return ContextServerRuntime
	}
}
