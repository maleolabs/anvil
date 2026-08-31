package registry

import _ "embed"

//go:embed embedded_compat_matrix.json
var embeddedCompatMatrix []byte

// EmbeddedCompatibilityMatrix returns the embedded matrix bytes (for tests).
func EmbeddedCompatibilityMatrix() []byte { return embeddedCompatMatrix }
