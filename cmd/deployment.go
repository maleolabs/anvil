// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-01, ST-P10-02, ADR-015, EPIC-010, TD-006
package cmd

import (
	"github.com/spf13/cobra"
)

// deploymentCmd represents the "anvil deployment" parent command for
// managing deployment targets and artifact delivery.
//
// Deployment commands allow operators to inspect deployment targets,
// upload artifacts, and manage the delivery context without reading
// Runtime Registry, State, release directories, or symlinks (ADR-015).
//
// The family has two surfaces (TD-006, PM decision option a):
//
//   - "anvil deployment upload" is the only SSH transport command: it
//     delivers an artifact to a remote target using credentials from
//     the environment (DEPLOY_SERVER_*) and works on a fresh runner
//     with env-only configuration — no local server state.
//   - "anvil deployment install/activate/rollback/info" are local
//     target-centric aliases of the server command surface: install,
//     activate, and rollback alias the 'server release' operations,
//     and info aliases 'server status'. They run on the local server
//     runtime through the ServerRelease coordinator and require a
//     locally initialized server.
//
// Reference: ST-P10-01, ST-P10-02, ADR-015, EPIC-010, TD-006
var deploymentCmd = &cobra.Command{
	Use:   "deployment",
	Short: "Manage deployments",
	Long: `Manage deployment targets and artifact delivery.

Deployment commands allow operators to inspect deployment target
identity and delivery context, and upload artifacts to targets
through configured transport mechanisms.

The family has two surfaces:

  - 'anvil deployment upload' is the only SSH transport command. It
    delivers an artifact to a remote target using credentials from the
    environment (DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER,
    DEPLOY_SERVER_PORT, DEPLOY_SSH_KEY) and works on a fresh runner
    with env-only configuration — no local server state is required.

  - 'anvil deployment install', 'activate', 'rollback', and 'info' are
    local target-centric aliases of the server command surface:
    install/activate/rollback alias the 'server release' operations,
    and info aliases 'server status'. They run on the local server
    runtime through the ServerRelease coordinator and require a locally
    initialized server. Their <target-id> argument is a correlation
    label echoed in output and JSON; it does NOT select a target.

Deployment context does NOT read Registry, State, release directories,
or symlinks — it is independent from Runtime filesystem layout
per ADR-015.`,
}

func init() {
	rootCmd.AddCommand(deploymentCmd)
}
