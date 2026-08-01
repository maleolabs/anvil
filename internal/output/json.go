package output

import (
	"encoding/json"
	"io"
)

// ── Machine-Readable JSON Envelope (TS-P8-05) ─────────────────────────

// OutputEnvelope is the standard machine-readable output wrapper for all
// Anvil CLI commands when --json is used.
//
// Every JSON response uses this envelope:
//
//	{"version":"1","status":"success","data":{...}}
//	{"version":"1","status":"error","error":"..."}
//
// Reference: TS-P8-05
type OutputEnvelope struct {
	Version string      `json:"version"`
	Status  string      `json:"status"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// WriteJSON encodes data as a machine-readable JSON envelope with
// versioning and a "success" status.
//
// Usage:
//
//	err := output.WriteJSON(w, myData)
func WriteJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	envelope := OutputEnvelope{
		Version: "1",
		Status:  "success",
		Data:    data,
	}
	return enc.Encode(envelope)
}

// WriteJSONError encodes an error response in the machine-readable JSON
// envelope format with an "error" status.
//
// Usage:
//
//	err := output.WriteJSONError(w, "could not load project")
func WriteJSONError(w io.Writer, errMsg string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	envelope := OutputEnvelope{
		Version: "1",
		Status:  "error",
		Error:   errMsg,
	}
	return enc.Encode(envelope)
}
