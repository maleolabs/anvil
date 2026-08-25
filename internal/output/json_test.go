package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// ── WriteJSON Tests ───────────────────────────────────────────────────

func TestWriteJSON_EnvelopeStructure(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	err := WriteJSON(&buf, data)
	if err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Version != "1" {
		t.Errorf("version = %q, want %q", envelope.Version, "1")
	}
	if envelope.Status != "success" {
		t.Errorf("status = %q, want %q", envelope.Status, "success")
	}
	if envelope.Error != "" {
		t.Errorf("error should be empty, got %q", envelope.Error)
	}
	if envelope.Data == nil {
		t.Error("data should not be nil")
	}
}

func TestWriteJSON_DataPreserved(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"name":    "my-app",
		"version": "1.0.0",
		"active":  true,
	}
	err := WriteJSON(&buf, data)
	if err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, `"name": "my-app"`) {
		t.Errorf("output should contain data field 'name', got: %s", got)
	}
	if !strings.Contains(got, `"version": "1.0.0"`) {
		t.Errorf("output should contain data field 'version', got: %s", got)
	}
	if !strings.Contains(got, `"active": true`) {
		t.Errorf("output should contain data field 'active', got: %s", got)
	}
}

func TestWriteJSON_PrettyPrint(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"a": "b"}
	err := WriteJSON(&buf, data)
	if err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	got := buf.String()
	// Pretty-printed JSON should have newlines and indentation.
	if !strings.Contains(got, "\n") {
		t.Errorf("output should be pretty-printed with newlines, got: %s", got)
	}
	if !strings.Contains(got, "  ") {
		t.Errorf("output should be pretty-printed with indentation, got: %s", got)
	}
}

func TestWriteJSON_EnvelopeVersion(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, "test")
	if err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Version != "1" {
		t.Errorf("version field = %q, want %q", envelope.Version, "1")
	}
}

func TestWriteJSON_NilData(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, nil)
	if err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Status != "success" {
		t.Errorf("status = %q, want %q", envelope.Status, "success")
	}
}

// ── WriteJSONError Tests ──────────────────────────────────────────────

func TestWriteJSONError_EnvelopeStructure(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "something went wrong")
	if err != nil {
		t.Fatalf("WriteJSONError returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Version != "1" {
		t.Errorf("version = %q, want %q", envelope.Version, "1")
	}
	if envelope.Status != "error" {
		t.Errorf("status = %q, want %q", envelope.Status, "error")
	}
	if envelope.Data != nil {
		t.Errorf("data should be nil for error responses, got %v", envelope.Data)
	}
	if envelope.Error != "something went wrong" {
		t.Errorf("error = %q, want %q", envelope.Error, "something went wrong")
	}
}

func TestWriteJSONError_ErrorMessage(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "could not load project")
	if err != nil {
		t.Fatalf("WriteJSONError returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, `"error": "could not load project"`) {
		t.Errorf("output should contain the error message, got: %s", got)
	}
}

func TestWriteJSONError_StatusIsError(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "fail")
	if err != nil {
		t.Fatalf("WriteJSONError returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Status != "error" {
		t.Errorf("status = %q, want %q", envelope.Status, "error")
	}
}

func TestWriteJSONError_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, "")
	if err != nil {
		t.Fatalf("WriteJSONError returned error: %v", err)
	}

	var envelope OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if envelope.Status != "error" {
		t.Errorf("status = %q, want %q", envelope.Status, "error")
	}
	if envelope.Error != "" {
		t.Errorf("error = %q, want empty string", envelope.Error)
	}
}

// ── OutputEnvelope Tests ──────────────────────────────────────────────

func TestOutputEnvelope_RoundTrip(t *testing.T) {
	original := OutputEnvelope{
		Version: "1",
		Status:  "success",
		Data:    map[string]int{"count": 42},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded OutputEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("version mismatch: %q != %q", decoded.Version, original.Version)
	}
	if decoded.Status != original.Status {
		t.Errorf("status mismatch: %q != %q", decoded.Status, original.Status)
	}
}

func TestOutputEnvelope_ErrorRoundTrip(t *testing.T) {
	original := OutputEnvelope{
		Version: "1",
		Status:  "error",
		Error:   "something failed",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded OutputEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Status != "error" {
		t.Errorf("status = %q, want %q", decoded.Status, "error")
	}
	if decoded.Error != "something failed" {
		t.Errorf("error = %q, want %q", decoded.Error, "something failed")
	}
}
