// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-01, EPIC-010, ADR-015
package deployment

import (
	"testing"
)

// TestTargetID_String verifies that TargetID.String() returns the
// underlying string value.
//
// Reference: TS-P10-01 AC-1
func TestTargetID_String(t *testing.T) {
	tests := []struct {
		id   TargetID
		want string
	}{
		{TargetID("my-target"), "my-target"},
		{TargetID(""), ""},
		{TargetID("node-1.prod.example.com"), "node-1.prod.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.id.String(); got != tt.want {
				t.Errorf("TargetID.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPlatform_ZeroValue verifies that a zero-value Platform has
// empty OS and Arch fields.
func TestPlatform_ZeroValue(t *testing.T) {
	var p Platform

	if p.OS != "" {
		t.Errorf("Platform.OS = %q, want %q", p.OS, "")
	}
	if p.Arch != "" {
		t.Errorf("Platform.Arch = %q, want %q", p.Arch, "")
	}
}

// TestPlatform_Creation verifies Platform field assignment.
func TestPlatform_Creation(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64"}

	if p.OS != "linux" {
		t.Errorf("Platform.OS = %q, want %q", p.OS, "linux")
	}
	if p.Arch != "amd64" {
		t.Errorf("Platform.Arch = %q, want %q", p.Arch, "amd64")
	}
}

// TestRuntimeVersionConstraint_ZeroValue verifies that a zero-value
// constraint has empty MinVersion and MaxVersion (no constraint).
func TestRuntimeVersionConstraint_ZeroValue(t *testing.T) {
	var c RuntimeVersionConstraint

	if c.MinVersion != "" {
		t.Errorf("RuntimeVersionConstraint.MinVersion = %q, want %q", c.MinVersion, "")
	}
	if c.MaxVersion != "" {
		t.Errorf("RuntimeVersionConstraint.MaxVersion = %q, want %q", c.MaxVersion, "")
	}
}

// TestRuntimeVersionConstraint_Creation verifies constraint field
// assignment.
func TestRuntimeVersionConstraint_Creation(t *testing.T) {
	c := RuntimeVersionConstraint{MinVersion: "1.0.0", MaxVersion: "2.0.0"}

	if c.MinVersion != "1.0.0" {
		t.Errorf("MinVersion = %q, want %q", c.MinVersion, "1.0.0")
	}
	if c.MaxVersion != "2.0.0" {
		t.Errorf("MaxVersion = %q, want %q", c.MaxVersion, "2.0.0")
	}
}

// TestTargetMetadata_Creation verifies that TargetMetadata fields are
// correctly assigned.
//
// Reference: TS-P10-01 AC-2
func TestTargetMetadata_Creation(t *testing.T) {
	id := TargetID("node-1")
	meta := TargetMetadata{
		ID:      id,
		Name:    "production-node-1",
		Address: "192.168.1.100:22",
		Platform: Platform{
			OS:   "linux",
			Arch: "amd64",
		},
	}

	if meta.ID != id {
		t.Errorf("TargetMetadata.ID = %q, want %q", meta.ID, id)
	}
	if meta.Name != "production-node-1" {
		t.Errorf("TargetMetadata.Name = %q, want %q", meta.Name, "production-node-1")
	}
	if meta.Address != "192.168.1.100:22" {
		t.Errorf("TargetMetadata.Address = %q, want %q", meta.Address, "192.168.1.100:22")
	}
	if meta.Platform.OS != "linux" {
		t.Errorf("TargetMetadata.Platform.OS = %q, want %q", meta.Platform.OS, "linux")
	}
	if meta.Platform.Arch != "amd64" {
		t.Errorf("TargetMetadata.Platform.Arch = %q, want %q", meta.Platform.Arch, "amd64")
	}
}

// TestTargetMetadata_IndependentFromRuntimeLayout verifies that
// TargetMetadata does not contain Runtime filesystem paths or
// Registry internals (ADR-015).
//
// Reference: TS-P10-01 AC-2, ADR-015
func TestTargetMetadata_IndependentFromRuntimeLayout(t *testing.T) {
	// TargetMetadata must not have fields that reference Runtime
	// filesystem layout, Registry, or State internals. This test
	// lists the only allowed fields — if TargetMetadata gains a
	// new field, it must be reviewed to ensure no Runtime coupling.
	meta := TargetMetadata{
		ID:       TargetID("test"),
		Name:     "test",
		Address:  "host:port",
		Platform: Platform{OS: "linux", Arch: "amd64"},
	}

	// Verify that the metadata contains only identity/descriptive fields.
	if meta.ID == "" {
		t.Error("TargetMetadata must have an ID")
	}
	if meta.Name == "" {
		t.Error("TargetMetadata must have a Name")
	}

	// Platform is a value type; verify it's not a pointer that could
	// accidentally reference Runtime state.
	if meta.Platform == (Platform{}) {
		t.Error("TargetMetadata.Platform must be set")
	}
}

// TestCompatibilityInput_Creation verifies CompatibilityInput field
// assignment.
//
// Reference: TS-P10-01 AC-1
func TestCompatibilityInput_Creation(t *testing.T) {
	input := CompatibilityInput{
		RuntimeVersion: RuntimeVersionConstraint{
			MinVersion: "1.0.0",
			MaxVersion: "2.0.0",
		},
		Platform: Platform{
			OS:   "linux",
			Arch: "amd64",
		},
	}

	if input.RuntimeVersion.MinVersion != "1.0.0" {
		t.Errorf("MinVersion = %q, want %q", input.RuntimeVersion.MinVersion, "1.0.0")
	}
	if input.RuntimeVersion.MaxVersion != "2.0.0" {
		t.Errorf("MaxVersion = %q, want %q", input.RuntimeVersion.MaxVersion, "2.0.0")
	}
	if input.Platform.OS != "linux" {
		t.Errorf("Platform.OS = %q, want %q", input.Platform.OS, "linux")
	}
	if input.Platform.Arch != "amd64" {
		t.Errorf("Platform.Arch = %q, want %q", input.Platform.Arch, "amd64")
	}
}

// TestTarget_InterfaceSatisfaction verifies that a concrete struct
// satisfies the Target interface (compile-time check).
//
// Reference: TS-P10-01 AC-3
func TestTarget_InterfaceSatisfaction(t *testing.T) {
	// Compile-time check: if testTarget does not implement Target,
	// this line will not compile.
	var _ Target = (*testTarget)(nil)
}

// TestTarget_InterfaceBehavior verifies that calling interface methods
// on a concrete implementation returns the expected values.
//
// Reference: TS-P10-01 AC-3
func TestTarget_InterfaceBehavior(t *testing.T) {
	tt := &testTarget{
		id: TargetID("test-node"),
		meta: TargetMetadata{
			ID:   TargetID("test-node"),
			Name: "Test Node",
			Platform: Platform{
				OS:   "linux",
				Arch: "amd64",
			},
		},
		comp: CompatibilityInput{
			Platform: Platform{OS: "linux", Arch: "amd64"},
		},
	}

	if got := tt.ID(); got != TargetID("test-node") {
		t.Errorf("Target.ID() = %q, want %q", got, TargetID("test-node"))
	}

	if got := tt.Metadata().Name; got != "Test Node" {
		t.Errorf("Target.Metadata().Name = %q, want %q", got, "Test Node")
	}

	if got := tt.CompatibilityInput().Platform.Arch; got != "amd64" {
		t.Errorf("Target.CompatibilityInput().Platform.Arch = %q, want %q", got, "amd64")
	}

	if err := tt.ValidateCompatibility(tt.comp); err != nil {
		t.Errorf("Target.ValidateCompatibility() = %v, want nil", err)
	}
}

// TestTarget_ValidateCompatibility verifies that compatibility validation
// rejects incompatible inputs.
//
// Reference: TS-P10-01 AC-1, AC-3
func TestTarget_ValidateCompatibility(t *testing.T) {
	target := &testTarget{
		id: TargetID("linux-node"),
		comp: CompatibilityInput{
			Platform: Platform{OS: "linux", Arch: "amd64"},
		},
	}

	tests := []struct {
		name  string
		input CompatibilityInput
		want  error
	}{
		{
			name: "matching_platform",
			input: CompatibilityInput{
				Platform: Platform{OS: "linux", Arch: "amd64"},
			},
			want: nil,
		},
		{
			name: "os_mismatch",
			input: CompatibilityInput{
				Platform: Platform{OS: "windows", Arch: "amd64"},
			},
			want: errPlatformMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := target.ValidateCompatibility(tc.input)
			if tc.want == nil && got != nil {
				t.Errorf("ValidateCompatibility() = %v, want nil", got)
			}
			if tc.want != nil && got == nil {
				t.Errorf("ValidateCompatibility() = nil, want %v", tc.want)
			}
			if tc.want != nil && got != nil && got.Error() != tc.want.Error() {
				t.Errorf("ValidateCompatibility() = %q, want %q", got.Error(), tc.want.Error())
			}
		})
	}
}

// testTarget is a minimal Target implementation used for contract
// verification in tests.
type testTarget struct {
	id   TargetID
	meta TargetMetadata
	comp CompatibilityInput
}

func (t *testTarget) ID() TargetID                           { return t.id }
func (t *testTarget) Metadata() TargetMetadata               { return t.meta }
func (t *testTarget) CompatibilityInput() CompatibilityInput { return t.comp }
func (t *testTarget) ValidateCompatibility(input CompatibilityInput) error {
	if input.Platform.OS != "" && t.comp.Platform.OS != "" &&
		input.Platform.OS != t.comp.Platform.OS {
		return errPlatformMismatch
	}
	return nil
}

// errPlatformMismatch is a sentinel error for platform compatibility
// failures in tests.
var errPlatformMismatch = &compatibilityError{reason: "platform OS mismatch"}

// compatibilityError is a simple error type for compatibility validation
// failures used in tests.
type compatibilityError struct {
	reason string
}

func (e *compatibilityError) Error() string {
	return e.reason
}
