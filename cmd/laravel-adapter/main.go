// Command laravel-adapter is the Laravel framework adapter executable
// (004-review-resolutions D1: adapters are standalone executables invoked
// by the Core as `<adapter-executable> <command> <json-payload>`).
//
// The binary name convention is `anvil-adapter-laravel` — the Core
// resolves it via exec.LookPath("anvil-adapter-" + framework) when a
// project selects the "laravel" adapter (005-adapter-command-contract
// §10).
//
// Supported commands: capabilities, activate, verify, extension, validate
// (005-adapter-command-contract §5.2, §6.2). JSON result on stdout; exit
// 0 on a produced result, non-zero on dispatch failure (ADR-010 §8.1).
//
// Reference: TS-P7-09, TS-P7-10, TS-P7-11, TS-P7-12,
// 004-review-resolutions D1
package main

import (
	"os"

	"maleolabs.com/anvil/internal/laravel"
)

func main() {
	os.Exit(laravel.New().Run(os.Args[1:], os.Stdout, os.Stderr))
}
