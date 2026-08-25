package spklocaldeployux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// ── Dry-Run Contract (AC1) ────────────────────────────────────────────

// DryRunParams controls the isolated dry-run harness (build+verify without install).
type DryRunParams struct {
	ProjectID  string
	TargetEnv  string // e.g. "staging"
	Version    string // e.g. "0.1.0"
	SizeMB     int    // dummy payload size per artifact (keeps tests fast)
	ArtifactsDir string // temp dir for artifact output
	Logger     io.Writer
}

// DryRunResult captures AC1 evidence: same artifact_id/version/checksum in human+JSON.
type DryRunResult struct {
	ArtifactPath string              `json:"artifact_path"`
	Manifest     *artifact.Manifest  `json:"manifest"`
	VerifyResult *artifact.VerificationResult `json:"verify"`
	HumanOutput  string              `json:"human_output"`
	JSONOutput   string              `json:"json_output"`
	JSONEnvelope output.OutputEnvelope `json:"-"` // parsed envelope for contract check
	Duration     time.Duration       `json:"duration"`
}

// RunDryRun executes build+verify without install and produces both human and JSON outputs.
// The two outputs are contract-consistent: same artifact_id / version / checksum.
// Reuses internal/artifact.Package + artifact.VerifyArtifact (spec:artifact-manifest, verification-contract).
func RunDryRun(p DryRunParams) (*DryRunResult, error) {
	if p.ProjectID == "" {
		p.ProjectID = "spike-test-project"
	}
	if p.Version == "" {
		p.Version = "0.1.0"
	}
	if p.SizeMB <= 0 {
		p.SizeMB = 1
	}
	if p.TargetEnv == "" {
		p.TargetEnv = "staging"
	}
	if p.ArtifactsDir == "" {
		return nil, fmt.Errorf("ArtifactsDir required")
	}
	if p.Logger == nil {
		p.Logger = io.Discard
	}
	start := time.Now()

	// 1) build artifact via internal/artifact.Package (isolated temp source)
	artPath, manifest, err := buildArtifact(p.ArtifactsDir, p.SizeMB, p.ProjectID, p.Version)
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}

	// 2) verify (verification-before-trust gate, no install)
	vr, err := artifact.VerifyArtifact(artPath)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	// 3) human output (non-JSON, plain — consistent CLI output via internal/output)
	var humanBuf bytes.Buffer
	renderDryRunHuman(&humanBuf, p.TargetEnv, manifest, vr, time.Since(start), artPath)

	// 4) machine output (JSON envelope version:1 status:success — via internal/output.WriteJSON)
	dryRunData := map[string]interface{}{
		"target":      p.TargetEnv,
		"dry_run":     true,
		"artifact_id": manifest.ArtifactID,
		"version":     manifest.Version,
		"checksum":    manifest.Checksum,
		"checksum_type": manifest.ChecksumType,
		"project_id":  manifest.ProjectID,
		"artifact_path": artPath,
		"verify": map[string]interface{}{
			"passed": vr.Passed,
			"checks": vr.Checks,
		},
	}
	var jsonBuf bytes.Buffer
	if err := output.WriteJSON(&jsonBuf, dryRunData); err != nil {
		return nil, fmt.Errorf("write json: %w", err)
	}
	// parse envelope back to verify contract
	var env output.OutputEnvelope
	if err := json.Unmarshal(jsonBuf.Bytes(), &env); err != nil {
		return nil, fmt.Errorf("parse envelope: %w", err)
	}

	duration := time.Since(start)
	fmt.Fprintf(p.Logger, "%s", humanBuf.String())
	fmt.Fprintf(p.Logger, "\n[json]\n%s", jsonBuf.String())

	return &DryRunResult{
		ArtifactPath: artPath,
		Manifest:     manifest,
		VerifyResult: vr,
		HumanOutput:  humanBuf.String(),
		JSONOutput:   jsonBuf.String(),
		JSONEnvelope: env,
		Duration:     duration,
	}, nil
}

func renderDryRunHuman(w io.Writer, targetEnv string, m *artifact.Manifest, vr *artifact.VerificationResult, dur time.Duration, artifactPath string) {
	fmt.Fprintf(w, "Deploy dry-run --target %s\n", targetEnv)
	fmt.Fprintf(w, "  Step: Build artifact... \n")
	fmt.Fprintf(w, "  Step: Build artifact ✓ (%s)\n", output.FormatDuration(dur))
	fmt.Fprintf(w, "    artifact_id: %s\n", m.ArtifactID)
	fmt.Fprintf(w, "    version: %s\n", m.Version)
	fmt.Fprintf(w, "    checksum: %s (%s)\n", m.Checksum, m.ChecksumType)
	fmt.Fprintf(w, "    path: %s\n", filepath.Base(artifactPath))
	fmt.Fprintf(w, "  Step: Verify artifact... \n")
	if vr.Passed {
		output.PrintStatus(w, output.StatusPass, fmt.Sprintf("Verify %d checks PASS", len(vr.Checks)))
	} else {
		output.PrintStatus(w, output.StatusFail, "Verify FAIL")
	}
	for _, c := range vr.Checks {
		status := output.StatusPass
		if !c.Passed {
			status = output.StatusFail
		}
		output.PrintStatus(w, status, fmt.Sprintf("%s: %s", c.Name, c.Details))
	}
	fmt.Fprintf(w, "Dry-run complete — no install performed (build+verify only) (%s)\n", output.FormatDuration(dur))
	fmt.Fprintf(w, "Tip: remove --dry-run to push and install.\n")
}

// buildArtifact creates an isolated source dir with dummy payload and packages via artifact.Package.
func buildArtifact(outDir string, sizeMB int, projectID, version string) (string, *artifact.Manifest, error) {
	srcDir, err := os.MkdirTemp("", "spk-ux-src-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(srcDir)

	if err := os.MkdirAll(filepath.Join(srcDir, "app"), 0755); err != nil {
		return "", nil, err
	}
	dummyPath := filepath.Join(srcDir, "app", "payload.bin")
	if err := createDummyFile(dummyPath, sizeMB); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(srcDir, "app", "index.php"), []byte("<?php echo 'anvil-ux';"), 0644); err != nil {
		return "", nil, err
	}
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: srcDir,
		OutputDir: outDir,
		Formats:   []string{"tar.gz"},
		Version:   version,
		Source:    "spike-local-deploy-ux",
		ProjectID: projectID,
	})
	if err != nil {
		return "", nil, err
	}
	m, err := artifact.ReadManifest(result.ArtifactPath)
	if err != nil {
		return result.ArtifactPath, result.Manifest, nil // fallback to result manifest
	}
	return result.ArtifactPath, m, nil
}

func createDummyFile(path string, sizeMB int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	chunk := make([]byte, 256*1024)
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}
	remaining := sizeMB * 1024 * 1024
	for remaining > 0 {
		n := len(chunk)
		if remaining < n {
			n = remaining
		}
		if _, err := f.Write(chunk[:n]); err != nil {
			return err
		}
		remaining -= n
	}
	return f.Sync()
}

// ValidateHumanJSONConsistency checks that human vs JSON outputs carry same artifact_id/version/checksum.
func ValidateHumanJSONConsistency(r *DryRunResult) error {
	if r.Manifest == nil {
		return fmt.Errorf("manifest nil")
	}
	var envMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.JSONOutput), &envMap); err != nil {
		return err
	}
	var data map[string]interface{}
	if raw, ok := envMap["data"]; ok {
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	}
	fields := map[string]string{
		"artifact_id": r.Manifest.ArtifactID,
		"version":     r.Manifest.Version,
		"checksum":    r.Manifest.Checksum,
		"project_id":  r.Manifest.ProjectID,
	}
	for k, want := range fields {
		got, _ := data[k].(string)
		if got != want {
			return fmt.Errorf("mismatch %s: human/manifest %q vs json %q", k, want, got)
		}
	}
	if r.JSONEnvelope.Version != "1" || r.JSONEnvelope.Status != "success" {
		return fmt.Errorf("envelope invalid: version=%q status=%q want 1/success", r.JSONEnvelope.Version, r.JSONEnvelope.Status)
	}
	// also ensure human contains same ids
	for _, needle := range []string{r.Manifest.ArtifactID, r.Manifest.Version, r.Manifest.Checksum} {
		if !bytes.Contains([]byte(r.HumanOutput), []byte(needle)) {
			return fmt.Errorf("human missing %q", needle[:min(16, len(needle))])
		}
	}
	return nil
}

func min(a, b int) int { if a < b { return a }; return b }
