package output

import (
	"testing"
)

// ── Exit Code Constants ──────────────────────────────────────────────

func TestExitCodeConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"ExitCodeSuccess", ExitCodeSuccess, 0},
		{"ExitCodeGeneral", ExitCodeGeneral, 1},
		{"ExitCodeConfig", ExitCodeConfig, 2},
		{"ExitCodeRuntime", ExitCodeRuntime, 3},
		{"ExitCodePrecondition", ExitCodePrecondition, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// ── ExitCoder Interface ─────────────────────────────────────────────

// TestExitCoder_Interface verifies that AppError implements ExitCoder.
func TestExitCoder_Interface(t *testing.T) {
	var _ ExitCoder = &AppError{}
}

// TestExitCoder_AppError_DefaultCode verifies that an AppError with
// zero ExitCodeValue returns ExitCodeGeneral (1).
func TestExitCoder_AppError_DefaultCode(t *testing.T) {
	e := &AppError{Message: "test error"}
	if e.ExitCode() != ExitCodeGeneral {
		t.Errorf("AppError.ExitCode() = %d, want %d (default)", e.ExitCode(), ExitCodeGeneral)
	}
}

// TestExitCoder_AppError_ExplicitCode verifies that an AppError with
// an explicit ExitCodeValue returns that code.
func TestExitCoder_AppError_ExplicitCode(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		want     int
	}{
		{"general", ExitCodeGeneral, 1},
		{"config", ExitCodeConfig, 2},
		{"runtime", ExitCodeRuntime, 3},
		{"precondition", ExitCodePrecondition, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &AppError{Message: "test", ExitCodeValue: tt.exitCode}
			if e.ExitCode() != tt.want {
				t.Errorf("AppError.ExitCode() = %d, want %d", e.ExitCode(), tt.want)
			}
		})
	}
}
