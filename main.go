// Anvil is a release lifecycle engine for single-server deployments.
//
// Reference: ADR-010, ADR-011, ADR-012
package main

import (
	"errors"
	"os"

	"maleolabs.com/anvil/cmd"
	"maleolabs.com/anvil/internal/output"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var exitErr output.ExitCoder
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(output.ExitCodeGeneral)
	}
}
