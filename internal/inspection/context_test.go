// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: ST-P9-06, ADR-015
package inspection

import (
	"testing"
)

// issue is a test helper building a DiagnosticIssue with the given
// component and location.
func issue(component, location string) DiagnosticIssue {
	return DiagnosticIssue{
		Component:   component,
		Severity:    SeverityMajor,
		Description: "test finding",
		Location:    location,
		LikelyCause: "test cause",
	}
}

// TestClassifyContext_ComponentMapping verifies the component → context
// mapping rules (ADR-015, EPIC-009 §8.3):
//   - config → development (Development/CI configuration)
//   - runtime → server_runtime (Runtime environment state)
//   - server → server_runtime (registry/server state)
//
// Reference: ST-P9-06
func TestClassifyContext_ComponentMapping(t *testing.T) {
	tests := []struct {
		name  string
		issue DiagnosticIssue
		want  ArchitecturalContext
	}{
		{
			name:  "config issues are Development/CI configuration",
			issue: issue("config", "completeness"),
			want:  ContextDevelopment,
		},
		{
			name:  "config load failures are Development configuration",
			issue: issue("config", "config_load"),
			want:  ContextDevelopment,
		},
		{
			name:  "runtime issues are Server Runtime state",
			issue: issue("runtime", "active_symlink (.anvil/active)"),
			want:  ContextServerRuntime,
		},
		{
			name:  "runtime shared resources are Server Runtime state",
			issue: issue("runtime", "shared_resources"),
			want:  ContextServerRuntime,
		},
		{
			name:  "server registry issues are Server Runtime state",
			issue: issue("server", "registry_consistency"),
			want:  ContextServerRuntime,
		},
		{
			name:  "server config issues are Server Runtime state",
			issue: issue("server", "server_config"),
			want:  ContextServerRuntime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyContext(tt.issue); got != tt.want {
				t.Errorf("ClassifyContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClassifyContext_ReleaseEvidenceBased verifies that release component
// findings are split by evidence:
//   - artifact_presence findings belong to the Artifact context (EPIC-003)
//   - release infrastructure findings (directory, shared links) belong to
//     the Release context (EPIC-004)
//
// This is the Artifact identity vs Release identity separation (ADR-015,
// ST-009-006 AC3).
//
// Reference: ST-P9-06
func TestClassifyContext_ReleaseEvidenceBased(t *testing.T) {
	tests := []struct {
		name  string
		issue DiagnosticIssue
		want  ArchitecturalContext
	}{
		{
			name:  "artifact presence is Artifact identity",
			issue: issue("release", "artifact_presence (/srv/app/releases/r1)"),
			want:  ContextArtifact,
		},
		{
			name:  "release directory is Release infrastructure",
			issue: issue("release", "release_directory (/srv/app/releases)"),
			want:  ContextRelease,
		},
		{
			name:  "shared links are Release infrastructure",
			issue: issue("release", "shared_links"),
			want:  ContextRelease,
		},
		{
			name:  "unprefixed release findings stay in Release context",
			issue: issue("release", "release_state"),
			want:  ContextRelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyContext(tt.issue); got != tt.want {
				t.Errorf("ClassifyContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClassifyContext_NoMisattributionWithoutEvidence verifies the
// evidence-based attribution rule (ST-009-006 AC2): Runtime failures are
// never attributed to repository source (Development context) or
// Deployment transport without evidence.
//
// Reference: ST-P9-06
func TestClassifyContext_NoMisattributionWithoutEvidence(t *testing.T) {
	runtimeFindings := []DiagnosticIssue{
		issue("runtime", "active_symlink"),
		issue("runtime", "release_directories"),
		issue("runtime", "shared_resources"),
		issue("runtime", "runtime_config"),
		issue("server", "registry_directory"),
		issue("server", "project_registries"),
	}

	for _, finding := range runtimeFindings {
		if got := ClassifyContext(finding); got == ContextDevelopment {
			t.Errorf("ClassifyContext(%q) attributed a %s finding to Development without evidence", finding.Component, finding.Location)
		}
		if got := ClassifyContext(finding); got == ContextDeployment {
			t.Errorf("ClassifyContext(%q) attributed a %s finding to Deployment without evidence", finding.Component, finding.Location)
		}
	}
}

// TestClassifyContext_GenericForUnknownComponents verifies that adapter
// and other unknown component data remains generic in MVP: the classifier
// falls back to the conservative Server Runtime context and never
// attributes unknown findings to repository source or transport.
//
// Reference: ST-P9-06 (ST-009-006 AC5)
func TestClassifyContext_GenericForUnknownComponents(t *testing.T) {
	unknown := issue("adapter", "compatibility")

	got := ClassifyContext(unknown)
	if got != ContextServerRuntime {
		t.Errorf("ClassifyContext(adapter) = %q, want %q (generic fallback)", got, ContextServerRuntime)
	}
	if got == ContextDevelopment || got == ContextDeployment {
		t.Errorf("ClassifyContext(adapter) = %q: unknown components must not be attributed to Development/Deployment", got)
	}
}

// TestClassifyIssues_PreservesOrderAndCount verifies that ClassifyIssues
// produces exactly one contextual entry per issue, preserving the input
// order so contextual entries align one-to-one with the issues.
//
// Reference: ST-P9-06
func TestClassifyIssues_PreservesOrderAndCount(t *testing.T) {
	issues := []DiagnosticIssue{
		issue("config", "completeness"),
		issue("runtime", "active_symlink"),
		issue("release", "artifact_presence"),
		issue("release", "release_directory"),
		issue("server", "registry_consistency"),
	}

	contextual := ClassifyIssues(issues)

	if len(contextual) != len(issues) {
		t.Fatalf("ClassifyIssues() returned %d entries, want %d", len(contextual), len(issues))
	}

	wantContexts := []ArchitecturalContext{
		ContextDevelopment,
		ContextServerRuntime,
		ContextArtifact,
		ContextRelease,
		ContextServerRuntime,
	}

	for i, entry := range contextual {
		if entry.Issue != issues[i] {
			t.Errorf("entry %d: issue mismatch (order not preserved)", i)
		}
		if entry.Context != wantContexts[i] {
			t.Errorf("entry %d: context = %q, want %q", i, entry.Context, wantContexts[i])
		}
	}
}

// TestClassifyIssues_Empty verifies that classifying an empty issue set
// produces an empty (non-nil) classification result.
//
// Reference: ST-P9-06
func TestClassifyIssues_Empty(t *testing.T) {
	contextual := ClassifyIssues(nil)

	if contextual == nil || len(contextual) != 0 {
		t.Errorf("ClassifyIssues(nil) = %v, want empty non-nil slice", contextual)
	}
}
