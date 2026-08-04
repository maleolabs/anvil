// Command flutter-adapter is the Flutter framework adapter executable
// (004-review-resolutions D1: adapters are standalone executables invoked
// by the Core as `<adapter-executable> <command> <json-payload>`).
//
// The binary name convention is `anvil-adapter-flutter` — the Core
// resolves it via exec.LookPath("anvil-adapter-" + framework) when a
// project selects the "flutter" adapter (005-adapter-command-contract
// §10).
//
// Supported commands: capabilities, extension, build, template
// (005-adapter-command-contract §5.2, §6.2; the template command returns
// the adapter-owned pipeline definitions, ADR-020 §1). The extension
// command returns the empty config extension scaffold required by the
// Core registration path (TS-P7-20 AC-4; real keys are TS-P7-26's
// scope). The adapter implements the hybrid deployment model — no
// activate command (TS-P7-20 AC-5); verify/validate are owned by
// TS-P7-25/26. JSON result on stdout; exit 0 on a produced result,
// non-zero on dispatch failure (ADR-010 §8.1).
//
// Reference: TS-P7-20, TS-P7-21, TS-P7-22, TS-007-038, ADR-020,
// 004-review-resolutions D1
package main

import (
	"os"

	"maleolabs.com/anvil/internal/flutter"
)

func main() {
	os.Exit(flutter.New().Run(os.Args[1:], os.Stdout, os.Stderr))
}
