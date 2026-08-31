package output

import (
	"fmt"
	"io"
	"strings"
)

// ── Summary ───────────────────────────────────────────────────────────

// Summary renders key-value pairs with consistent alignment, matching the
// existing PrintKeyValue convention from cmd/conventions.go.
//
// Usage:
//
//	s := output.NewSummary()
//	s.Add("Project", "my-app")
//	s.Add("Version", "1.0.0")
//	s.Format(cmd.OutOrStdout())
//	// Output:
//	//   Project: my-app
//	//   Version: 1.0.0
type Summary struct {
	keys   []string
	values []string
	keyLen int // width of the longest key, for alignment
}

// NewSummary creates an empty Summary.
func NewSummary() *Summary {
	return &Summary{}
}

// Add appends a key-value pair. The key is used to determine column
// alignment — all keys are padded to the width of the longest key.
func (s *Summary) Add(key, value string) {
	s.keys = append(s.keys, key)
	s.values = append(s.values, value)
	if len(key) > s.keyLen {
		s.keyLen = len(key)
	}
}

// Format writes the summary to w.
func (s *Summary) Format(w io.Writer) {
	for i := range s.keys {
		padded := s.keys[i] + strings.Repeat(" ", s.keyLen-len(s.keys[i]))
		fmt.Fprintf(w, "  %s : %s\n", padded, s.values[i])
	}
}
