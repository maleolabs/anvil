// Package project provides tests for missing project error formatting.
//
// Reference: ST-P1-06
package project

import (
	"testing"
)

// --- ST-P1-06 Tests: Missing Project Error Formatting ---

// TestFormatMissingProjectError_ContainsNoProjectFound verifies that the
// error message includes a clear statement that no project was found.
//
// Acceptance Criteria: ST-P1-06 AC-1 (partial)
func TestFormatMissingProjectError_ContainsNoProjectFound(t *testing.T) {
	message := FormatMissingProjectError([]string{"/tmp"})

	if !contains(message, "no Anvil project found") {
		t.Errorf("message should contain 'no Anvil project found', got: %s", message)
	}
}

// TestFormatMissingProjectError_ListsSearchedDirectories verifies that the
// error message lists all searched directories.
//
// Acceptance Criteria: ST-P1-06 AC-2
func TestFormatMissingProjectError_ListsSearchedDirectories(t *testing.T) {
	searched := []string{"/home/user/project", "/home/user", "/home", "/"}
	message := FormatMissingProjectError(searched)

	for _, dir := range searched {
		if !contains(message, dir) {
			t.Errorf("message should contain searched directory %q, got: %s", dir, message)
		}
	}

	// The header "Searched directories:" should be present.
	if !contains(message, "Searched directories:") {
		t.Errorf("message should contain 'Searched directories:', got: %s", message)
	}
}

// TestFormatMissingProjectError_ContainsInitGuidance verifies that the
// error message includes guidance on creating a new project with
// "anvil init".
//
// Acceptance Criteria: ST-P1-06 AC-3
func TestFormatMissingProjectError_ContainsInitGuidance(t *testing.T) {
	message := FormatMissingProjectError([]string{"/tmp"})

	if !contains(message, "anvil init") {
		t.Errorf("message should contain 'anvil init' guidance, got: %s", message)
	}
	if !contains(message, "<project-name>") {
		t.Errorf("message should contain '<project-name>', got: %s", message)
	}
}

// TestFormatMissingProjectError_ContainsNavigateGuidance verifies that
// the error message includes guidance on navigating to a directory that
// contains an Anvil project.
//
// Acceptance Criteria: ST-P1-06 AC-4
func TestFormatMissingProjectError_ContainsNavigateGuidance(t *testing.T) {
	message := FormatMissingProjectError([]string{"/tmp"})

	if !contains(message, "navigate to a directory") {
		t.Errorf("message should contain navigation guidance, got: %s", message)
	}
	if !contains(message, "Anvil project") {
		t.Errorf("message should mention 'Anvil project', got: %s", message)
	}
}

// TestFormatMissingProjectError_Format verifies the overall structure
// of the error message includes all required sections.
//
// Acceptance Criteria: ST-P1-06 AC 1-4
func TestFormatMissingProjectError_Format(t *testing.T) {
	searched := []string{"/current/dir", "/parent/dir", "/"}
	message := FormatMissingProjectError(searched)

	// Must start with "Error:"
	if !contains(message, "Error:") {
		t.Errorf("message should start with 'Error:', got: %s", message)
	}

	// Must include all searched directories.
	for _, dir := range searched {
		if !contains(message, dir) {
			t.Errorf("message should include searched directory %q", dir)
		}
	}

	// Must include init guidance.
	if !contains(message, "anvil init") {
		t.Errorf("message should include 'anvil init'")
	}

	// Must include navigation guidance.
	if !contains(message, "navigate") {
		t.Errorf("message should include navigation guidance")
	}

	// Verify line format: each searched directory should be indented.
	for _, dir := range searched {
		expected := "  " + dir
		if !contains(message, expected) {
			t.Errorf("searched directory should be indented with 2 spaces, want %q in message", expected)
		}
	}
}

// TestFormatMissingProjectError_WithEmptySearched verifies the function
// handles an empty searched slice gracefully.
func TestFormatMissingProjectError_WithEmptySearched(t *testing.T) {
	message := FormatMissingProjectError([]string{})

	if message == "" {
		t.Fatal("FormatMissingProjectError should not return empty string")
	}
	if !contains(message, "no Anvil project found") {
		t.Errorf("message should contain 'no Anvil project found', got: %s", message)
	}
}

// TestFormatMissingProjectError_WithSingleDirectory verifies the function
// handles a single entry in the searched slice.
func TestFormatMissingProjectError_WithSingleDirectory(t *testing.T) {
	message := FormatMissingProjectError([]string{"/tmp"})

	if !contains(message, "/tmp") {
		t.Errorf("message should contain the searched directory, got: %s", message)
	}
}
