// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-004, ADR-005 §8.7, ADR-006 §5.2
package inspection

import (
	"fmt"
	"strings"
)

// IssueType categorizes a diagnostic issue so it can be mapped to the
// Epic that owns the resolution.
//
// Reference: TS-009-004 §7
type IssueType string

const (
	// IssueTypeConfiguration identifies configuration issues owned by EPIC-002.
	IssueTypeConfiguration IssueType = "configuration"

	// IssueTypeRuntime identifies Runtime environment issues owned by EPIC-005.
	IssueTypeRuntime IssueType = "runtime"

	// IssueTypeArtifact identifies artifact integrity issues owned by EPIC-003.
	IssueTypeArtifact IssueType = "artifact"

	// IssueTypeRelease identifies release state issues owned by EPIC-004.
	IssueTypeRelease IssueType = "release"

	// IssueTypeServer identifies server/registry runtime issues owned by EPIC-005.
	IssueTypeServer IssueType = "server"
)

// Recommendation is an actionable step for resolving a detected diagnostic
// issue. It references the Epic that owns the resolution for the issue
// type and provides a specific step the operator can take.
//
// Reference: TS-009-004 §3/§7, EPIC-009 §5.1/§8.5
type Recommendation struct {
	// Action is the specific step the operator can take to resolve the
	// issue, in human-readable form.
	Action string `json:"action"`

	// OwnerEpic references the Epic that owns the resolution for the
	// issue type (e.g. "EPIC-002" for configuration issues).
	OwnerEpic string `json:"owner_epic"`

	// IssueType categorizes the issue this recommendation resolves.
	IssueType IssueType `json:"issue_type"`
}

// RecommendationEngine maps diagnostic issues (TS-009-003) to actionable
// recommendations. It is read-only and never performs remediation — it
// only guides the operator toward the Epic that owns the resolution.
//
// Reference: TS-009-004 §3, ADR-005 §8.7
type RecommendationEngine struct{}

// NewRecommendationEngine creates a RecommendationEngine.
//
// Reference: TS-009-004
func NewRecommendationEngine() *RecommendationEngine {
	return &RecommendationEngine{}
}

// RecommendationsFor maps each diagnostic issue to an actionable
// recommendation. Every issue produces exactly one recommendation so
// issues remain traceable to their resolution. Returns an empty slice
// when there are no issues.
//
// Issue-to-recommendation mapping (per TS-009-004 §7 and EPIC-009 §5.2):
//   - Configuration issues → EPIC-002 (configuration management)
//   - Runtime issues → EPIC-005 (runtime lifecycle management)
//   - Artifact issues → EPIC-003 (artifact lifecycle management)
//   - Release issues → EPIC-004 (release lifecycle management)
//   - Server/registry issues → EPIC-005 (runtime lifecycle management),
//     with the action tailored to the server context.
//
// Reference: TS-009-004 §7
func (re *RecommendationEngine) RecommendationsFor(issues []DiagnosticIssue) []Recommendation {
	recommendations := make([]Recommendation, 0, len(issues))

	for _, issue := range issues {
		recommendations = append(recommendations, re.recommendationFor(issue))
	}

	return recommendations
}

// recommendationFor maps a single issue to its recommendation. The issue
// type is derived from the issue component, refined by the failed check
// location for artifact presence checks performed by the release
// inspector (the check name is the leading part of Location).
//
// Reference: TS-009-004 §7
func (re *RecommendationEngine) recommendationFor(issue DiagnosticIssue) Recommendation {
	switch issue.Component {
	case "config":
		return Recommendation{
			Action:    "Set the required configuration key in your project configuration",
			OwnerEpic: "EPIC-002",
			IssueType: IssueTypeConfiguration,
		}

	case "runtime":
		return Recommendation{
			Action:    "Ensure the Runtime is provisioned and ready",
			OwnerEpic: "EPIC-005",
			IssueType: IssueTypeRuntime,
		}

	case "release":
		// The release inspector also owns artifact presence checks.
		// Artifact issues are resolved through EPIC-003, not EPIC-004.
		if strings.HasPrefix(issue.Location, "artifact_presence") {
			return Recommendation{
				Action:    "Run artifact verification to confirm integrity",
				OwnerEpic: "EPIC-003",
				IssueType: IssueTypeArtifact,
			}
		}
		return Recommendation{
			Action:    "Check release state using the release status command",
			OwnerEpic: "EPIC-004",
			IssueType: IssueTypeRelease,
		}

	case "server":
		return re.serverRecommendation(issue)

	default:
		return Recommendation{
			Action:    fmt.Sprintf("Inspect the %s component state", issue.Component),
			OwnerEpic: "EPIC-009",
			IssueType: IssueTypeServer,
		}
	}
}

// serverRecommendation tailors the recommendation to the specific server
// context of the issue: registry integrity failures, server configuration
// failures, or generic server readiness failures. All server issues are
// resolved through EPIC-005, which owns the server runtime lifecycle.
//
// Reference: TS-009-004 §7, EPIC-009 §5.2
func (re *RecommendationEngine) serverRecommendation(issue DiagnosticIssue) Recommendation {
	switch {
	case strings.HasPrefix(issue.Location, "registry_directory"),
		strings.HasPrefix(issue.Location, "registry_files"),
		strings.HasPrefix(issue.Location, "registry_consistency"),
		strings.HasPrefix(issue.Location, "registry_store"):
		return Recommendation{
			Action:    "Verify project registries are present and valid in the server registry store",
			OwnerEpic: "EPIC-005",
			IssueType: IssueTypeServer,
		}

	case strings.HasPrefix(issue.Location, "server_config"):
		return Recommendation{
			Action:    "Ensure the server configuration is valid and the server runtime is initialized",
			OwnerEpic: "EPIC-005",
			IssueType: IssueTypeServer,
		}

	default:
		return Recommendation{
			Action:    "Ensure the server runtime is provisioned and ready",
			OwnerEpic: "EPIC-005",
			IssueType: IssueTypeServer,
		}
	}
}
