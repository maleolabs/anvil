// The deployment model contract (TS-P7-13) is defined in this file.
//
// ADR-016 defines the deployment models an Anvil release can use:
//
//   - "server":  deploy to a server and activate the release in place
//     (e.g. Laravel — the release runs on the target host);
//   - "hybrid":  build and package the release for distribution
//     (e.g. Flutter — an app artifact that runs outside the server);
//   - "package": build and distribute the release without server-side
//     activation (reserved for future use).
//
// The deployment model is a data payload only, consistent with the rest
// of the package: adapters declare the model they support through the
// capability declaration (contracts.CapabilityDeclaration), and the Core
// reads the declaration to plan deployment without inspecting adapter
// internals (ADR-009 §4.1, §7.3).
//
// Reference: TS-P7-13, ADR-016
package contracts

// DeploymentModel identifies the deployment model an adapter supports
// (ADR-016). The value is declared in the adapter's capability
// declaration; an empty model — no model declared — is valid for generic
// adapters that are not deployment-bound.
//
// Reference: TS-P7-13 AC-3
type DeploymentModel string

const (
	// DeploymentModelServer deploys releases to a server and activates
	// them in place: the server runs the release after activation
	// (e.g. Laravel).
	DeploymentModelServer DeploymentModel = "server"

	// DeploymentModelHybrid builds and packages releases for
	// distribution outside the server (e.g. Flutter).
	DeploymentModelHybrid DeploymentModel = "hybrid"

	// DeploymentModelPackage builds and distributes releases without
	// server-side activation. Reserved for future use.
	DeploymentModelPackage DeploymentModel = "package"
)
