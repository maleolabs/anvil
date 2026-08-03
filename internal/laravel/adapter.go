// Package laravel implements the Laravel framework adapter: the declared
// capabilities (TS-P7-09, TS-P7-10, TS-P7-11, TS-P7-13, TS-P7-14) and
// configuration extension (TS-P7-12) of the Laravel adapter executable,
// plus the command dispatcher that implements the adapter command
// contract (005-adapter-command-contract).
//
// Per 004-review-resolutions D1, the adapter is a standalone executable:
// the Core invokes it as `<adapter-executable> <command> <json-payload>`
// through the Process Runner and reads a structured JSON result from
// stdout. This package is the adapter side of the contract — it is never
// imported by Core server code (ADR-009 §8.1: Core never depends on
// adapters); all Laravel-specific values live here and only here
// (ADR-009 §9.6).
//
// The executable entrypoint is cmd/laravel-adapter/main.go; the binary
// name convention is `anvil-adapter-laravel` (see
// 005-adapter-command-contract §10).
//
// Reference: TS-P7-09, TS-P7-10, TS-P7-11, TS-P7-12, TS-P7-13, TS-P7-14,
// ADR-009, ADR-016, 004-review-resolutions D1
package laravel

import "maleolabs.com/anvil/internal/contracts"

// Framework is the adapter's framework name. It is the namespace segment
// for configuration extensions ("framework.<framework>." per ADR-005
// §4.4) and the value a project records in its registry
// (ProjectSection.Adapter) to select this adapter.
//
// Reference: TS-P7-12, ADR-005 §4.4
const Framework = "laravel"

// Capabilities returns the Laravel adapter's declared capabilities: the
// activation phases it supports (TS-P7-09, TS-P7-10), the verification
// checks it provides (TS-P7-11, TS-P7-17), the deployment model it
// implements (TS-P7-13, ADR-016 — "server": releases deploy to a server
// and are activated in place), and the build phases it supports
// (TS-P7-14). The Core reads this declaration through the `capabilities`
// command to determine what to invoke (TS-P7-07, TS-P7-08).
//
// Reference: TS-P7-09, TS-P7-10, TS-P7-11, TS-P7-17, TS-P7-13, TS-P7-14,
// TS-P7-07, ADR-016
func Capabilities() contracts.CapabilityResult {
	return contracts.CapabilityResult{
		Declaration: contracts.CapabilityDeclaration{
			DeploymentModel: string(contracts.DeploymentModelServer),
			ActivationPhases: []string{
				PhaseMigrate,
				PhaseConfigCache,
				PhaseRouteCache,
				PhaseEventCache,
			},
			BuildPhases: []string{
				PhaseComposer,
				PhaseNpm,
				PhaseConfigCache,
				PhaseRouteCache,
				PhaseViewCache,
			},
			VerificationChecks: []contracts.VerificationCheck{
				{
					Name:        CheckVendorPresent,
					Description: "validates that vendor/autoload.php exists in the artifact",
				},
				{
					Name:        CheckBootstrapStructure,
					Description: "validates that bootstrap/app.php exists in the artifact",
				},
				{
					Name:        CheckConfigFiles,
					Description: "validates that required config files (config/app.php, .env.example) exist in the artifact",
				},
				{
					Name:        CheckArtisanFile,
					Description: "validates that the artisan CLI entrypoint exists in the artifact root",
				},
				{
					Name:        CheckComposerJSON,
					Description: "validates that composer.json exists in the artifact root",
				},
				{
					Name:        CheckEnvFile,
					Description: "validates that .env or .env.example exists in the artifact root",
				},
				{
					Name:        CheckAppDirectory,
					Description: "validates that the app/ directory exists in the artifact",
				},
				{
					Name:        CheckRoutesDirectory,
					Description: "validates that the routes/ directory exists in the artifact",
				},
			},
		},
	}
}
