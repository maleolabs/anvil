package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ── PlainStepReporter Tests ──────────────────────────────────────────

func TestPlainStepReporter_Start(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.Start("Update Anvil CLI")

	got := buf.String()
	if !strings.Contains(got, "Update Anvil CLI") {
		t.Errorf("Start() should contain title, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("Start() should end with newline, got %q", got)
	}
}

func TestPlainStepReporter_StepStart(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.StepStart("Check latest version")

	got := buf.String()
	if !strings.Contains(got, "Step: Check latest version...") {
		t.Errorf("StepStart() should contain step name with '...', got %q", got)
	}
}

func TestPlainStepReporter_StepComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.StepComplete("Check latest version", 800*time.Millisecond)

	got := buf.String()
	if !strings.Contains(got, "Step: Check latest version ✓") {
		t.Errorf("StepComplete() should contain step name with ✓, got %q", got)
	}
	if !strings.Contains(got, "0.8s") {
		t.Errorf("StepComplete() should contain duration, got %q", got)
	}
}

func TestPlainStepReporter_StepFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.StepFailed("Download", 500*time.Millisecond, errTest)

	got := buf.String()
	if !strings.Contains(got, "Step: Download ✗") {
		t.Errorf("StepFailed() should contain step name with ✗, got %q", got)
	}
	if !strings.Contains(got, "test error") {
		t.Errorf("StepFailed() should contain error message, got %q", got)
	}
}

func TestPlainStepReporter_Complete(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.Complete("Updated to v0.6.0", 3300*time.Millisecond)

	got := buf.String()
	if !strings.Contains(got, "Updated to v0.6.0") {
		t.Errorf("Complete() should contain title, got %q", got)
	}
	if !strings.Contains(got, "3.3s") {
		t.Errorf("Complete() should contain duration, got %q", got)
	}
}

func TestPlainStepReporter_Failed(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)
	r.Failed("Update Anvil CLI", 1200*time.Millisecond)

	got := buf.String()
	if !strings.Contains(got, "Update Anvil CLI failed") {
		t.Errorf("Failed() should contain title with 'failed', got %q", got)
	}
	if !strings.Contains(got, "1.2s") {
		t.Errorf("Failed() should contain duration, got %q", got)
	}
}

func TestPlainStepReporter_FullWorkflow(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainStepReporter(&buf)

	r.Start("Update Anvil CLI")
	r.StepStart("Check latest version")
	r.StepComplete("Check latest version", 800*time.Millisecond)
	r.StepStart("Download v0.6.0")
	r.StepComplete("Download v0.6.0", 2100*time.Millisecond)
	r.Complete("Updated to v0.6.0", 3300*time.Millisecond)

	got := buf.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	// Should have: title, step start, step complete, step start, step complete, complete
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d: %q", len(lines), got)
	}

	if !strings.HasPrefix(lines[0], "Update Anvil CLI") {
		t.Errorf("first line should be title, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Step: Check latest version...") {
		t.Errorf("second line should be step start, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "Step: Check latest version ✓") {
		t.Errorf("third line should be step complete, got %q", lines[2])
	}
}

// ── InteractiveStepReporter Tests ────────────────────────────────────

func TestInteractiveStepReporter_Start(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveStepReporter(&buf)
	r.Start("Update Anvil CLI")

	got := buf.String()
	// Non-terminal: should contain title but not ▶
	if !strings.Contains(got, "Update Anvil CLI") {
		t.Errorf("Start() should contain title, got %q", got)
	}
}

func TestInteractiveStepReporter_StepComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveStepReporter(&buf)
	r.StepComplete("Download", 2100*time.Millisecond)

	got := buf.String()
	// Non-terminal: StepComplete should print the final status
	if !strings.Contains(got, "✓") {
		t.Errorf("StepComplete() should contain ✓, got %q", got)
	}
	if !strings.Contains(got, "Download") {
		t.Errorf("StepComplete() should contain step name, got %q", got)
	}
	if !strings.Contains(got, "2.1s") {
		t.Errorf("StepComplete() should contain duration, got %q", got)
	}
}

func TestInteractiveStepReporter_Complete(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveStepReporter(&buf)
	r.Complete("Updated to v0.6.0", 1200*time.Millisecond)

	got := buf.String()
	if !strings.Contains(got, "✓") {
		t.Errorf("Complete() should contain ✓, got %q", got)
	}
	if !strings.Contains(got, "Updated to v0.6.0") {
		t.Errorf("Complete() should contain title, got %q", got)
	}
}

func TestInteractiveStepReporter_Failed(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveStepReporter(&buf)
	r.Failed("Update Anvil CLI", 500*time.Millisecond)

	got := buf.String()
	if !strings.Contains(got, "✗") {
		t.Errorf("Failed() should contain ✗, got %q", got)
	}
	if !strings.Contains(got, "Update Anvil CLI") {
		t.Errorf("Failed() should contain title, got %q", got)
	}
}

// ── Factory Tests ────────────────────────────────────────────────────

func TestNewStepReporter_NonTerminal(t *testing.T) {
	var buf bytes.Buffer
	r := NewStepReporter(&buf)

	// Non-terminal should get PlainStepReporter
	if _, ok := r.(*PlainStepReporter); !ok {
		t.Errorf("NewStepReporter() for non-terminal should return *PlainStepReporter, got %T", r)
	}
}

// ── FormatDuration Tests ─────────────────────────────────────────────

func TestStepReporter_FormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"milliseconds", 300 * time.Millisecond, "0.3s"},
		{"seconds", 1200 * time.Millisecond, "1.2s"},
		{"whole seconds", 3 * time.Second, "3s"},
		{"sub-second", 50 * time.Millisecond, "0.0s"},
		{"large", 65400 * time.Millisecond, "1m5.4s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.d)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// ── Helper ───────────────────────────────────────────────────────────

// errTest is a reusable test error.
var errTest = &testError{"test error"}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }
