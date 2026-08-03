// Package flutter implements the Flutter framework adapter: the declared
// capabilities (TS-P7-20), the build targets (TS-P7-21), the platform
// detection (TS-P7-22), and the command dispatcher that implements the
// adapter command contract (005-adapter-command-contract).
//
// Per 004-review-resolutions D1, the adapter is a standalone executable:
// the Core invokes it as `<adapter-executable> <command> <json-payload>`
// through the Process Runner and reads a structured JSON result from
// stdout. This package is the adapter side of the contract — it is never
// imported by Core server code (ADR-009 §8.1: Core never depends on
// adapters); all Flutter-specific values live here and only here
// (ADR-009 §9.6).
//
// The Flutter adapter is a hybrid deployment model adapter (ADR-016):
// releases are built and packaged for distribution (web, APK, iOS)
// rather than deployed to a server and activated in place (EPIC-007
// §7.3). It therefore declares no activation phases and implements no
// `activate` command (TS-P7-20 AC-5).
//
// The executable entrypoint is cmd/flutter-adapter/main.go; the binary
// name convention is `anvil-adapter-flutter` (see
// 005-adapter-command-contract §10).
//
// Reference: TS-P7-20, TS-P7-21, TS-P7-22, ADR-009, ADR-016, ADR-018,
// 004-review-resolutions D1
package flutter

import "maleolabs.com/anvil/internal/contracts"

// Framework is the adapter's framework name. It is the value a project
// records in its registry (ProjectSection.Adapter) to select this
// adapter, and the segment the Core uses to resolve the adapter
// executable (`anvil-adapter-flutter` via exec.LookPath,
// 005-adapter-command-contract §10).
//
// Reference: TS-P7-20 AC-2
const Framework = "flutter"

// Capabilities returns the Flutter adapter's declared capabilities: the
// hybrid deployment model (TS-P7-20 AC-3, ADR-016 — releases are built
// and packaged for distribution, no server activation), the build targets
// it supports (TS-P7-21), and the verification checks it provides
// (TS-P7-25). The declaration intentionally lists no activation phases —
// the hybrid model has none (TS-P7-20 AC-5, EPIC-007 §7.3). The Core
// reads this declaration through the `capabilities` command to determine
// what to invoke (TS-P7-07, TS-P7-08).
//
// Reference: TS-P7-20 AC-2..AC-5, TS-P7-21, TS-P7-25, TS-P7-07, ADR-016
func Capabilities() contracts.CapabilityResult {
	return contracts.CapabilityResult{
		Declaration: contracts.CapabilityDeclaration{
			DeploymentModel: string(contracts.DeploymentModelHybrid),
			BuildPhases:     buildPhaseNames(),
			VerificationChecks: []contracts.VerificationCheck{
				{
					Name:        CheckPubspecYaml,
					Description: "validates that pubspec.yaml exists in the artifact root",
				},
				{
					Name:        CheckLibDirectory,
					Description: "validates that the lib/ directory exists in the artifact",
				},
			},
		},
	}
}

// buildPhaseNames returns the build target names in target table order —
// the BuildPhases declaration mirrors the build target table exactly
// (TS-P7-21 AC-4), so the two cannot drift.
//
// Reference: TS-P7-20 AC-3, TS-P7-21 AC-4
func buildPhaseNames() []string {
	names := make([]string, 0, len(buildTargets))
	for _, target := range buildTargets {
		names = append(names, target.Name)
	}
	return names
}
