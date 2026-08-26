// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-02, TS-P11-05, ADR-015, ADR-019, EPIC-010, EPIC-011,
// Decision 006, TD-006
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/deployment"
	"maleolabs.com/anvil/internal/output"
)

// deploymentUploadCmd represents the "anvil deployment upload" subcommand
// that delivers an artifact to a deployment target through a configured
// transport.
//
// Per ADR-015, this command delivers an immutable artifact through a
// configured transport, validates the payload, preserves the manifest,
// and does NOT manipulate Runtime filesystem layout.
//
// The transport is SSH (EPIC-011). Connection credentials are read from
// environment variables (DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER,
// DEPLOY_SERVER_PORT, DEPLOY_SSH_KEY) per ADR-019 — they are never
// stored in Anvil configuration. Host key verification is opt-in via
// DEPLOY_SSH_KNOWN_HOSTS / DEPLOY_SSH_KNOWN_HOSTS_MODE (TD-004).
//
// Upload is the only SSH transport command in the deployment family
// (TD-006, PM decision option a): it is decoupled from local server
// state and works on a fresh runner with env-only configuration. The
// other deployment commands (install/activate/rollback/info) are local
// target-centric aliases of the 'server release' operations and run on
// the local server runtime.
//
// Reference: ST-P10-02, TS-P11-05, ST-P11-01, ADR-015, ADR-019, EPIC-011, TD-004, TD-006
var deploymentUploadCmd = &cobra.Command{
	Use:   "upload <target-id> <artifact-path>",
	Short: "Upload an artifact to a deployment target",
	Long: `Upload an artifact to a deployment target over SSH.

Delivers an immutable artifact to a deployment target through the SSH
transport. The command validates the artifact payload and preserves the
manifest — it does NOT manipulate Runtime filesystem layout (ADR-015).

The upload process:
  1. Validates the artifact file exists on disk
  2. Validates the artifact has content (non-empty)
  3. Reads SSH credentials from environment variables
  4. Reads the artifact manifest into the delivery payload
  5. Initiates delivery through the SSH transport
  6. Reports delivery outcome

<target-id> is a correlation label: it is echoed in the output and in
--json output so automation can correlate the delivery, but it does NOT
select a target. The destination is the SSH endpoint configured via
environment variables below. No local server state is required: upload
works on a fresh runner with env-only configuration (MVP-002 §3.7,
EPIC-011 §8). It is the only SSH transport command in the deployment
family; install/activate/rollback/info are local target-centric aliases
of the 'server release' operations and run on the local server runtime.

SSH credentials are read from the environment (ADR-019), never from
Anvil configuration:

  DEPLOY_SERVER_HOST   server hostname or IP (required)
  DEPLOY_SERVER_USER   SSH username (required)
  DEPLOY_SERVER_PORT   SSH port (optional, default: 22)
  DEPLOY_SSH_KEY       path to the SSH private key file (required)

Host key verification is opt-in (TD-004): set DEPLOY_SSH_KNOWN_HOSTS to
a known_hosts file to verify the server's identity before connecting.
Verification fails closed (unknown or changed host keys are rejected).
DEPLOY_SSH_KNOWN_HOSTS_MODE selects the mode: strict (default) or
accept-new (records an unknown key on first contact; changed keys are
still rejected). When DEPLOY_SSH_KNOWN_HOSTS is unset the transport does
not verify the server's host key (legacy behavior) — configure it in
production so the server identity is verified.

The command does NOT read Registry, State, release directories,
symlinks, or local server configuration.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment upload my-target path/to/artifact.tar.gz
  anvil deployment upload my-target path/to/artifact.tar.gz --json`,
	Args: ExactArgsWithUsage(2, "anvil deployment upload my-target path/to/artifact.tar.gz"),
	RunE: runDeploymentUpload,
}

func init() {
	deploymentCmd.AddCommand(deploymentUploadCmd)

	// Upload deliberately has no --server-root flag: it does not depend
	// on local server state (TD-006, PM decision option a). Target
	// identity comes from the environment (DEPLOY_SERVER_*) and the
	// <target-id> correlation label.
	AddJSONFlag(deploymentUploadCmd)
}

// runDeploymentUpload executes the "anvil deployment upload" command.
//
// It validates the artifact file, reads SSH credentials from the
// environment, builds the payload, and initiates delivery through the
// transport layer — without reading any local server state (TD-006).
func runDeploymentUpload(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	artifactPath := args[1]

	// Step 1: Validate the artifact path is accessible.
	fileInfo, err := os.Stat(artifactPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("artifact not found: %s", artifactPath),
				Reason:     "The specified artifact path does not exist",
				Resolution: "Check that the artifact path is correct and try again",
				Err:        err,
			})
		}
		return ReportPlainErrorf(cmd, err, "could not access artifact: %v", err)
	}

	// Step 2: Validate artifact has content.
	if fileInfo.Size() == 0 {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("artifact is empty: %s", artifactPath),
			Reason:     "The artifact file exists but contains no data",
			Resolution: "Re-package the artifact with 'anvil artifact package'",
			Err:        fmt.Errorf("artifact is empty: %s", artifactPath),
		})
	}

	// Step 3: Read SSH credentials from the environment (TS-P11-03).
	// Credentials are injected by CI/CD at runtime and are never stored
	// in Anvil configuration (ADR-019). The error names every missing
	// variable so operators know exactly what to set (ST-P11-01 §3).
	// Missing env prerequisites are a precondition class → exit 4
	// (TS-019-03-02, D-07).
	creds, err := deployment.ReadSSHCredentialsFromEnv()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "SSH credentials are not configured",
			Reason:     err.Error(),
			Resolution: "Set the required environment variables in your CI/CD pipeline (DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER, DEPLOY_SSH_KEY)",
			Err:        err,
			// Precondition category (D-07): the SSH credentials are a
			// required prerequisite of the delivery.
			ExitCodeValue: output.ExitCodePrecondition,
		})
	}

	// Step 4: Build target and payload. Target identity is derived from
	// the SSH credentials (env-only, ADR-019): <target-id> is a
	// correlation label echoed in output for automation, not a selector
	// (TD-006, PM decision option a). No server config is read.
	target := &cmdTarget{
		id:   deployment.TargetID(targetID),
		name: creds.Host,
		addr: creds.Host,
	}

	// The manifest is the transport contract's metadata carrier
	// (ADR-017): every payload must carry it so receivers can rely on
	// it instead of re-reading the transported file (TD-011).
	payload, err := buildUploadPayload(artifactPath)
	if err != nil {
		if errors.Is(err, errArtifactZipFormat) {
			// zip artifacts are valid Anvil packages, but upload reads
			// manifests from tar.gz archives only — the resolution
			// names the actual requirement instead of claiming the
			// artifact was not created by packaging (TD-011 review).
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("could not read artifact manifest: %v", err),
				Reason:     "The artifact is a zip archive, but upload reads manifests from tar.gz archives",
				Resolution: "Upload currently requires a .tar.gz artifact; re-package with 'anvil artifact package' and upload the .tar.gz file",
				Err:        err,
			})
		}
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not read artifact manifest: %v", err),
			Reason:     "The artifact may not be a valid Anvil package",
			Resolution: "Ensure the artifact was created with 'anvil artifact package' and contains a manifest.json",
			Err:        err,
		})
	}

	// Step 5: Deliver the artifact through the SSH transport (TS-P11-05).
	// Host key verification is opt-in (TD-004): it is enabled only when
	// DEPLOY_SSH_KNOWN_HOSTS is configured, so existing CI flows that
	// rely on the legacy behavior keep working.
	var transportOpts []deployment.Option
	if creds.KnownHostsPath != "" {
		transportOpts = append(transportOpts, deployment.WithKnownHosts(creds.KnownHostsPath, creds.KnownHostsMode))
	}
	transport := deployment.NewSSHTransport(creds.Host, creds.User, creds.KeyPath, creds.Port, transportOpts...)
	result, err := transport.Deliver(payload, target)
	if err != nil {
		var transportErr *deployment.TransportError
		if errors.As(err, &transportErr) {
			// Actionable failure reporting per TS-P11-04: the message
			// names the target, the reason explains the failure, and
			// the resolution gives the concrete next step (EPIC-011
			// §7.6).
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("artifact delivery to target %q failed", targetID),
				Reason:     transportErr.Reason,
				Resolution: transportErr.Guidance(),
				Err:        err,
			})
		}
		return ReportPlainErrorf(cmd, err, "delivery failed: %v", err)
	}

	// Step 6: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputUploadJSON(cmd, result, artifactPath)
	}

	PrintSuccess(cmd, "Delivery initiated.")
	fmt.Fprintf(styleFor(cmd).W, "  Target ID:    %s\n", result.TargetID)
	fmt.Fprintf(styleFor(cmd).W, "  Artifact:     %s\n", artifactPath)
	fmt.Fprintf(styleFor(cmd).W, "  Status:       %s\n", uploadStatus(result.Success))
	if result.RemotePath != "" {
		fmt.Fprintf(styleFor(cmd).W, "  Remote Path:  %s\n", result.RemotePath)
	}
	fmt.Fprintln(styleFor(cmd).W, "")
	PrintSuccess(cmd, "The artifact has been delivered to the target.")
	fmt.Fprintln(styleFor(cmd).W, "On the target machine, use 'anvil server release status <project-id>' or 'anvil server status' to verify the release.")

	return nil
}

// errArtifactZipFormat indicates the artifact is a zip archive: a valid
// Anvil package format produced by 'anvil artifact package', but upload
// reads manifests from tar.gz archives only (TD-011 review).
var errArtifactZipFormat = errors.New("artifact is a zip archive; upload requires a .tar.gz artifact")

// buildUploadPayload constructs the transport payload for an upload,
// embedding the artifact's manifest as the transport contract (ADR-017)
// requires (TD-011).
//
// The manifest is read from the artifact file with the same call
// `deployment install` uses, so the payload is complete at the upload
// boundary: receivers can rely on the payload manifest instead of
// re-reading the transported file. An error is returned when the
// artifact is not a valid tar.gz Anvil package; zip artifacts return
// errArtifactZipFormat so callers can emit a format-specific message.
func buildUploadPayload(artifactPath string) (deployment.ArtifactPayload, error) {
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		if hasZipMagic(artifactPath) {
			return deployment.ArtifactPayload{}, errArtifactZipFormat
		}
		// The underlying cause is already descriptive (e.g. "create
		// gzip reader: gzip: invalid header"); do not wrap it again
		// (TD-011 review).
		return deployment.ArtifactPayload{}, err
	}

	manifestBytes, err := artifact.MarshalManifest(*manifest)
	if err != nil {
		return deployment.ArtifactPayload{}, fmt.Errorf("serialize artifact manifest: %w", err)
	}

	return deployment.ArtifactPayload{
		Path:            artifactPath,
		ManifestContent: manifestBytes,
	}, nil
}

// hasZipMagic reports whether the file begins with the zip magic bytes
// (PK\x03\x04). Used to distinguish zip-format artifacts — which
// packaging also produces — from truly invalid files (TD-011 review).
func hasZipMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic == [4]byte{'P', 'K', 0x03, 0x04}
}

// uploadJSONOutput is the machine-readable output format for --json flag.
type uploadJSONOutput struct {
	TargetID   string `json:"target_id"`
	Artifact   string `json:"artifact"`
	Status     string `json:"status"`
	RemotePath string `json:"remote_path,omitempty"`
}

// outputUploadJSON writes the upload result as JSON to stdout.
//
// artifactPath is the local artifact path the caller delivered; it is
// included in the machine-readable output so automation can correlate
// the delivery with the artifact (BUG-011).
func outputUploadJSON(cmd *cobra.Command, result *deployment.TransportResult, artifactPath string) error {
	out := uploadJSONOutput{
		TargetID:   string(result.TargetID),
		Artifact:   artifactPath,
		Status:     uploadStatus(result.Success),
		RemotePath: result.RemotePath,
	}

	enc := json.NewEncoder(styleFor(cmd).Raw())
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
// The following type is an MVP implementation that will be replaced when
// a concrete Target implementation is available.

// cmdTarget is a minimal Target implementation used for MVP deployment
// commands. For upload it reads identity from the command arguments
// (the <target-id> correlation label) and the SSH credential
// environment (the host) — never from local server config (TD-006).
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

// Compile-time interface check.
var _ deployment.Target = (*cmdTarget)(nil)
