// Target selection helpers tests (TS-P7-24): KnownTargets derivation from
// pipeline task metadata and ValidateTargets rejection of unknown names.
package execution

import (
	"slices"
	"strings"
	"testing"
)

// TestKnownTargets_FromMetadata verifies that KnownTargets derives the
// ordered, de-duplicated target names from task metadata targets.
func TestKnownTargets_FromMetadata(t *testing.T) {
	def := flutterTestPipeline()

	got := KnownTargets(def)
	want := []string{"web", "apk", "ios"}
	if !slices.Equal(got, want) {
		t.Errorf("KnownTargets() = %v, want %v", got, want)
	}
}

// TestKnownTargets_FallbackToTaskNames verifies the documented fallback:
// when the definition has no metadata targets, task names are the targets.
func TestKnownTargets_FallbackToTaskNames(t *testing.T) {
	def := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "ci",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{Name: "compile", Command: "go"},
						{Name: "lint", Command: "golangci-lint"},
						{Name: "compile", Command: "go-test"}, // duplicate name
					},
				},
			},
		},
	}

	got := KnownTargets(def)
	want := []string{"compile", "lint"}
	if !slices.Equal(got, want) {
		t.Errorf("KnownTargets() = %v, want %v", got, want)
	}
}

// TestKnownTargets_MixedMetadataAndNames verifies the documented behavior
// for mixed definitions: each task contributes its metadata target when
// declared, otherwise its task name.
func TestKnownTargets_MixedMetadataAndNames(t *testing.T) {
	def := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "flutter-ios",
							Command: "flutter",
							Metadata: &TaskMetadata{
								Platforms: []string{"darwin"},
								Target:    "ios",
							},
						},
						{Name: "plain", Command: "echo"},
					},
				},
			},
		},
	}

	got := KnownTargets(def)
	want := []string{"ios", "plain"}
	if !slices.Equal(got, want) {
		t.Errorf("KnownTargets() = %v, want %v", got, want)
	}
}

// TestValidateTargets_Valid verifies that known target names pass validation.
func TestValidateTargets_Valid(t *testing.T) {
	def := flutterTestPipeline()

	if err := ValidateTargets(def, []string{"web", "apk"}); err != nil {
		t.Errorf("ValidateTargets(web,apk) = %v, want nil", err)
	}
	if err := ValidateTargets(def, []string{"ios"}); err != nil {
		t.Errorf("ValidateTargets(ios) = %v, want nil", err)
	}
}

// TestValidateTargets_EmptyRequest verifies that nil/empty requests are valid
// (no filtering).
func TestValidateTargets_EmptyRequest(t *testing.T) {
	def := flutterTestPipeline()

	if err := ValidateTargets(def, nil); err != nil {
		t.Errorf("ValidateTargets(nil) = %v, want nil", err)
	}
	if err := ValidateTargets(def, []string{}); err != nil {
		t.Errorf("ValidateTargets([]) = %v, want nil", err)
	}
}

// TestValidateTargets_UnknownTarget verifies that an unknown target name is
// rejected with an error listing the unknown name and the known targets.
func TestValidateTargets_UnknownTarget(t *testing.T) {
	def := flutterTestPipeline()

	err := ValidateTargets(def, []string{"xyz"})
	if err == nil {
		t.Fatal("ValidateTargets(xyz) = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown target "xyz"`) {
		t.Errorf("error = %q, want it to identify the unknown target", msg)
	}
	if !strings.Contains(msg, "known targets: web, apk, ios") {
		t.Errorf("error = %q, want it to list the known targets", msg)
	}
}

// TestValidateTargets_MultipleUnknown verifies that several unknown targets
// are all reported.
func TestValidateTargets_MultipleUnknown(t *testing.T) {
	def := flutterTestPipeline()

	err := ValidateTargets(def, []string{"web", "xyz", "abc"})
	if err == nil {
		t.Fatal("ValidateTargets() = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown targets "xyz", "abc"`) {
		t.Errorf("error = %q, want it to list both unknown targets", msg)
	}
}
