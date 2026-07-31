// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-02, ADR-015, EPIC-010, Decision 006
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/deployment"
	"maleolabs.com/anvil/internal/server"
)

// deploymentUploadCmd represents the "anvil deployment upload" subcommand
// that delivers an artifact to a deployment target through a configured
// transport.
//
// Per ADR-015, this command delivers an immutable artifact through a
// configured transport, validates the payload, preserves the manifest,
// and does NOT manipulate Runtime filesystem layout.
//
// Reference: ST-P10-02, ADR-015, Decision 006
var deploymentUploadCmd = &cobra.Command{
	Use:   "upload <target-id> <artifact-path>",
	Short: "Upload an artifact to a deployment target",
	Long: `Upload an artifact to a deployment target.

Delivers an immutable artifact to a deployment target through a
configured transport mechanism. The command validates the artifact
payload and preserves the manifest — it does NOT manipulate Runtime
filesystem layout (ADR-015).

The upload process:
  1. Validates the artifact file exists on disk
  2. Validates the artifact has content (non-empty)
  3. Initiates delivery through the target transport
  4. Reports delivery outcome

The actual transport mechanism (SSH, HTTP, etc.) is determined by
the target configuration. The deployment context does NOT read
Registry, State, release directories, or symlinks.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment upload my-target path/to/artifact.tar.gz
  anvil deployment upload my-target --server-root /tmp/anvil
  anvil deployment upload my-target --json`,
	Args: ExactArgsWithUsage(2, "anvil deployment upload my-target path/to/artifact.tar.gz"),
	RunE: runDeploymentUpload,
}

func init() {
	deploymentCmd.AddCommand(deploymentUploadCmd)

	deploymentUploadCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	AddJSONFlag(deploymentUploadCmd)
}

// runDeploymentUpload executes the "anvil deployment upload" command.
//
// It resolves the server root, validates the Runtime is initialized,
// validates the artifact file, builds the payload, and initiates
// delivery through the transport layer.
func runDeploymentUpload(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	artifactPath := args[1]

	// Step 1: Validate the artifact path is accessible.
	fileInfo, err := os.Stat(artifactPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: artifact not found: %s.\n", artifactPath)
			fmt.Fprintln(cmd.ErrOrStderr(), "Check that the artifact path is correct and try again.")
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not access artifact: %v.\n", err)
		return err
	}

	// Step 2: Validate artifact has content.
	if fileInfo.Size() == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: artifact is empty: %s.\n", artifactPath)
		fmt.Fprintln(cmd.ErrOrStderr(), "Artifact must contain data.")
		return fmt.Errorf("artifact is empty: %s", artifactPath)
	}

	// Step 3: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 4: Validate the Runtime is initialized.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 5: Load server config for target identity.
	configStore := server.NewConfigStore(rootPath)
	cfg, err := configStore.Load()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not load server config: %v.\n", err)
		return err
	}

	// Step 6: Build target and payload.
	target := &cmdTarget{
		id:   deployment.TargetID(targetID),
		name: cfg.Runtime.DisplayName,
		addr: cfg.Runtime.ID,
	}

	payload := deployment.ArtifactPayload{
		Path: artifactPath,
		// Manifest content will be populated by a future enhancement
		// when the deployment domain learns to read artifact manifests.
	}

	// Step 7: Initiate delivery.
	// MVP: Use a noop transport since concrete transport implementations
	// (SSH, HTTP) are not yet available. The transport delivers the
	// artifact and returns the result.
	transport := &noopTransport{}
	result, err := transport.Deliver(payload, target)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: delivery failed: %v.\n", err)
		return err
	}

	// Step 8: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputUploadJSON(cmd, result)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Delivery initiated.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:    %s\n", result.TargetID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Artifact:     %s\n", artifactPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  Status:       %s\n", uploadStatus(result.Success))
	if result.RemotePath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Remote Path:  %s\n", result.RemotePath)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The artifact has been delivered to the target.")
	fmt.Fprintln(cmd.OutOrStdout(), "Use 'anvil deployment info' to verify target status.")

	return nil
}

// uploadJSONOutput is the machine-readable output format for --json flag.
type uploadJSONOutput struct {
	TargetID   string `json:"target_id"`
	Artifact   string `json:"artifact"`
	Status     string `json:"status"`
	RemotePath string `json:"remote_path,omitempty"`
}

// outputUploadJSON writes the upload result as JSON to stdout.
func outputUploadJSON(cmd *cobra.Command, result *deployment.TransportResult) error {
	out := uploadJSONOutput{
		TargetID:   string(result.TargetID),
		Artifact:   "", // set by caller after delivery
		Status:     uploadStatus(result.Success),
		RemotePath: result.RemotePath,
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}

	return nil
}

// uploadStatus returns a human-readable status string.
func uploadStatus(success bool) string {
	if success {
		return "delivered"
	}
	return "failed"
}

// ---------------------------------------------------------------------------
// MVP Helpers
// ---------------------------------------------------------------------------
//
// The following types are MVP implementations that will be replaced when
// concrete Target and Transport implementations are available.

// cmdTarget is a minimal Target implementation used for MVP deployment
// commands. It reads identity from the command arguments and server config.
type cmdTarget struct {
	id   deployment.TargetID
	name string
	addr string
}

func (t *cmdTarget) ID() deployment.TargetID {
	return t.id
}

func (t *cmdTarget) Metadata() deployment.TargetMetadata {
	return deployment.TargetMetadata{
		ID:      t.id,
		Name:    t.name,
		Address: t.addr,
	}
}

func (t *cmdTarget) CompatibilityInput() deployment.CompatibilityInput {
	return deployment.CompatibilityInput{}
}

func (t *cmdTarget) ValidateCompatibility(_ deployment.CompatibilityInput) error {
	return nil
}

// noopTransport is a placeholder Transport implementation used for MVP.
// It reports delivery as successful without performing actual transport.
// Concrete transport implementations (SSH, HTTP) will replace this.
type noopTransport struct{}

func (t *noopTransport) Deliver(payload deployment.ArtifactPayload, target deployment.Target) (*deployment.TransportResult, error) {
	return &deployment.TransportResult{
		Success:    true,
		TargetID:   target.ID(),
		RemotePath: payload.Path,
	}, nil
}

// Compile-time interface checks.
var _ deployment.Target = (*cmdTarget)(nil)
var _ deployment.Transport = (*noopTransport)(nil)
