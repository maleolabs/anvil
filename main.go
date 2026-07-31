// Anvil is a release lifecycle engine for single-server deployments.
//
// Reference: ADR-010, ADR-011, ADR-012
package main

import (
	"os"

	"maleolabs.com/anvil/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
