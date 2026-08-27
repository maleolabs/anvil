package spkinstallerpipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
)

// ── Builder interface — spike 2 winner mock (NSIS + Makeself) ──

// Builder is the installer tooling contract (spike 2 interface mock).
type Builder interface {
	ID() string
	OS() string
	Build(opts BuilderOpts) (*BuilderResult, error)
}

type BuilderOpts struct {
	InstallerName string
	IconPath      string
	ArtifactPath  string // verified tar.gz to embed
	Manifest      *artifact.Manifest
	OutputDir     string // dist/installer/
	Logger        io.Writer
}

type BuilderResult struct {
	OutputPath string `json:"output_path"`
	FileName   string `json:"file_name"`
	SizeBytes  int64  `json:"size_bytes"`
	Simulated  bool   `json:"simulated"`
	Log        string `json:"log"`
}

// NSISMock simulates Windows NSIS .exe (spike 2 winner — real would exec makensis).
type NSISMock struct{}

func (b *NSISMock) ID() string { return "nsis" }
func (b *NSISMock) OS() string { return "windows" }
func (b *NSISMock) Build(opts BuilderOpts) (*BuilderResult, error) {
	name := SanitizeInstallerName(opts.InstallerName)
	filename := strings.ReplaceAll(name, " ", "-") + "-Setup.exe"
	outPath := filepath.Join(opts.OutputDir, filename)
	header := fmt.Sprintf("NSIS-MOCK|name=%s|artifact=%s|checksum=%s|icon=%s\n", name, filepath.Base(opts.ArtifactPath), opts.Manifest.Checksum[:16], filepath.Base(opts.IconPath))
	// simulate overhead ~1.6MB + payload stub (artifact size + header)
	info, _ := os.Stat(opts.ArtifactPath)
	payloadSize := int64(0)
	if info != nil {
		payloadSize = info.Size()
	}
	overhead := int64(1677721) // ~1.6MB NSIS overhead (spike 2 measurement)
	total := overhead + payloadSize + int64(len(header))
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, err
	}
	if err := WriteSizedFile(outPath, total, header); err != nil {
		return nil, fmt.Errorf("nsis mock write: %w", err)
	}
	// append manifest embed marker for verification
	f, _ := os.OpenFile(outPath, os.O_APPEND|os.O_WRONLY, 0644)
	if f != nil {
		_, _ = f.WriteString(fmt.Sprintf("\n#MANIFEST artifact_id=%s version=%s\n", opts.Manifest.ArtifactID, opts.Manifest.Version))
		f.Close()
	}
	sz := total
	if fi, err := os.Stat(outPath); err == nil {
		sz = fi.Size()
	}
	log := fmt.Sprintf("NSIS mock: %s (%d bytes) icon=%s simulated=true\nHeader: %s", filename, sz, opts.IconPath, header)
	if opts.Logger != nil {
		fmt.Fprintln(opts.Logger, log)
	}
	return &BuilderResult{OutputPath: outPath, FileName: filename, SizeBytes: sz, Simulated: true, Log: log}, nil
}

// MakeselfMock simulates Linux Makeself .run (spike 2 winner — real would exec makeself.sh).
type MakeselfMock struct{}

func (b *MakeselfMock) ID() string { return "makeself" }
func (b *MakeselfMock) OS() string { return "linux" }
func (b *MakeselfMock) Build(opts BuilderOpts) (*BuilderResult, error) {
	name := SanitizeInstallerName(opts.InstallerName)
	filename := strings.ReplaceAll(name, " ", "-") + ".run"
	outPath := filepath.Join(opts.OutputDir, filename)
	header := fmt.Sprintf("#!/bin/sh\n# Makeself-MOCK|name=%s|artifact=%s|checksum=%s|icon=%s\n", name, filepath.Base(opts.ArtifactPath), opts.Manifest.Checksum[:16], filepath.Base(opts.IconPath))
	info, _ := os.Stat(opts.ArtifactPath)
	payloadSize := int64(0)
	if info != nil {
		payloadSize = info.Size()
	}
	overhead := int64(48 * 1024) // Makeself overhead ~48KB
	total := overhead + payloadSize + int64(len(header))
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, err
	}
	if err := WriteSizedFile(outPath, total, header); err != nil {
		return nil, fmt.Errorf("makeself mock write: %w", err)
	}
	f, _ := os.OpenFile(outPath, os.O_APPEND|os.O_WRONLY, 0644)
	if f != nil {
		_, _ = f.WriteString(fmt.Sprintf("\n#MANIFEST artifact_id=%s version=%s\n", opts.Manifest.ArtifactID, opts.Manifest.Version))
		f.Close()
	}
	_ = os.Chmod(outPath, 0755)
	sz := total
	if fi, err := os.Stat(outPath); err == nil {
		sz = fi.Size()
	}
	log := fmt.Sprintf("Makeself mock: %s (%d bytes) icon=%s simulated=true\nHeader: %s", filename, sz, opts.IconPath, header)
	if opts.Logger != nil {
		fmt.Fprintln(opts.Logger, log)
	}
	return &BuilderResult{OutputPath: outPath, FileName: filename, SizeBytes: sz, Simulated: true, Log: log}, nil
}

// BuilderForTarget selects winner mock by target.
func BuilderForTarget(target string) (Builder, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "windows":
		return &NSISMock{}, nil
	case "linux":
		return &MakeselfMock{}, nil
	default:
		return nil, fmt.Errorf("unsupported target %q (want windows|linux)", target)
	}
}

// WriteSizedFile creates a file at path with exactly size bytes (deterministic header + pattern).
func WriteSizedFile(path string, size int64, header string) error {
	if size < 0 {
		size = 0
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := []byte(header + "\n")
	if int64(len(h)) > size {
		h = h[:size]
	}
	if _, err := f.Write(h); err != nil {
		return err
	}
	remaining := size - int64(len(h))
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = byte('A' + (i % 26))
	}
	for remaining > 0 {
		toWrite := int64(len(chunk))
		if toWrite > remaining {
			toWrite = remaining
		}
		if _, err := f.Write(chunk[:toWrite]); err != nil {
			return err
		}
		remaining -= toWrite
	}
	return f.Sync()
}

// ── Pipeline ──

// PipelineConfig controls one `anvil installer build --target X` execution.
//
// Reference: spk:installer-pipeline AC2-AC4, cmd/deploy pattern
type PipelineConfig struct {
	Target          string           // windows | linux (required)
	DryRun          bool             // --dry-run (AC4)
	JSONOutput      bool             // --json (AC4)
	OutputDir       string           // dist/installer (default)
	InstallerConfig *InstallerConfig // from anvil.yaml + env (AC1)
	ProjectRoot     string           // anchors SourceDir via artifactSource
	SourceDir       string           // override for artifact.Package (default: ProjectRoot + artifactSource)
	Version         string           // artifact version (default from project or 1.0.0)
	Logger          io.Writer        // human log writer (when !JSONOutput)
	RawWriter       io.Writer        // json writer (when JSONOutput) — envelope v1; if nil uses Logger
}

// PipelineResult is the pipeline execution evidence (for harness + json envelope).
type PipelineResult struct {
	Target       string                         `json:"target"`
	DryRun       bool                           `json:"dry_run"`
	ArtifactID   string                         `json:"artifact_id"`
	Version      string                         `json:"version"`
	Checksum     string                         `json:"checksum"`
	ChecksumType string                         `json:"checksum_type"`
	ProjectID    string                         `json:"project_id"`
	ArtifactPath string                         `json:"artifact_path"`
	Manifest     *artifact.Manifest             `json:"manifest,omitempty"`
	Verify       *artifact.VerificationResult   `json:"verify"`
	Installer    *BuilderResult                 `json:"installer,omitempty"`
	OutputPath   string                         `json:"installer_path,omitempty"`
	Idempotent   bool                           `json:"idempotent,omitempty"`
	DurationMs   int64                          `json:"duration_ms"`
}

// installerState is the idempotency marker (dist/installer/.installer-state.json).
type installerState struct {
	ArtifactID    string `json:"artifact_id"`
	Checksum      string `json:"checksum"`
	InstallerName string `json:"installer_name"`
	Target        string `json:"target"`
	Version       string `json:"version"`
	OutputFile    string `json:"output_file"`
	SizeBytes     int64  `json:"size_bytes"`
	Hash          string `json:"hash"` // sha256(artifact checksum + config)
}

func statePath(outputDir string) string { return filepath.Join(outputDir, ".installer-state.json") }

func computeStateHash(artifactChecksum, installerName, target, version string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{artifactChecksum, installerName, target, version}, "|")))
	return hex.EncodeToString(h[:])[:16]
}

// RunPipeline executes the installer pipeline:
//
//  1. Validate target (AC1 osTargets gate, AC4 error envelope)
//  2. Package artifact via internal/artifact.Package (AC2 reuse)
//  3. VerifyBeforeTrust gate — checksum verify before embed, abort if FAIL (AC3)
//  4. If --dry-run → return verify only, no installer build (AC4)
//  5. Idempotency check → skip rebuild if hash unchanged (AC3)
//  6. Builder mock → dist/installer/<Name>-Setup.exe | <Name>.run (AC2)
//  7. Render human or json envelope v1 (AC4)
func RunPipeline(cfg PipelineConfig) (*PipelineResult, error) {
	start := time.Now()
	if cfg.InstallerConfig == nil {
		return nil, fmt.Errorf("installer config required")
	}
	target := strings.ToLower(strings.TrimSpace(cfg.Target))
	if target == "" {
		return nil, newPipelineAppError("missing required flag --target", "The --target flag is required (windows|linux)", "Run 'anvil installer build --target <windows|linux>' — targets declared in anvil.yaml installer.osTargets", output.ExitCodeConfig)
	}
	if _, err := BuilderForTarget(target); err != nil {
		return nil, newPipelineAppError(fmt.Sprintf("unsupported target %q", target), err.Error(), "Use --target windows or --target linux (must be in anvil.yaml installer.osTargets)", output.ExitCodeConfig)
	}
	if !cfg.InstallerConfig.IsTargetAllowed(target) {
		return nil, newPipelineAppError(fmt.Sprintf("target %q not allowed", target), fmt.Sprintf("installer.osTargets=%v does not include %q", cfg.InstallerConfig.OSTargets, target), "Add the target to anvil.yaml installer.osTargets or override via ANVIL_CFG_INSTALLER_OS_TARGETS", output.ExitCodeConfig)
	}

	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(cfg.ProjectRoot, "dist", "installer")
	}
	projectRoot := cfg.ProjectRoot
	if projectRoot == "" {
		projectRoot = "."
	}
	sourceDir := cfg.SourceDir
	if sourceDir == "" {
		sourceDir = filepath.Join(projectRoot, cfg.InstallerConfig.ArtifactSource)
		if cfg.InstallerConfig.ArtifactSource == "." {
			sourceDir = projectRoot
		}
	}
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	rawWriter := cfg.RawWriter
	if rawWriter == nil {
		rawWriter = cfg.Logger
	}
	logger := cfg.Logger
	if logger == nil {
		logger = io.Discard
	}
	if cfg.JSONOutput {
		logger = io.Discard // silent in json mode except envelope
	}

	// Resolve project name for manifest source/project_id
	projectID := SanitizeInstallerName(cfg.InstallerConfig.Name)
	if projectID == "" {
		projectID = "anvil-project"
	}

	// Step 1: Package artifact (reuse internal/artifact.Package)
	tmpOut, err := os.MkdirTemp("", "anvil-installer-pkg-*")
	if err != nil {
		return nil, newPipelineAppError("could not create temp dir", err.Error(), "Check temp directory permissions", output.ExitCodeGeneral)
	}
	// tmpOut is retained until process exit so PipelineResult.ArtifactPath stays readable for harness/tests
	// (no defer RemoveAll — callers in production use dist/installer or explicit cleanup)

	// Artifact include/exclude from defaults (mirrors cmd/deploy)
	pkgRes, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: tmpOut,
		Formats:   []string{"tar.gz"},
		Version:   version,
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, newPipelineAppError("could not package artifact", err.Error(), "Check artifactSource exists and is readable; run with --dry-run to isolate packaging", output.ExitCodeGeneral)
	}
	manifest := pkgRes.Manifest
	if manifest == nil {
		return nil, newPipelineAppError("could not read artifact manifest", "package returned nil manifest", "Re-package with valid source dir", output.ExitCodeGeneral)
	}

	// Step 2: Verification-before-trust gate (AC3) — MUST pass before embed
	vr, err := artifact.VerifyArtifact(pkgRes.ArtifactPath)
	if err != nil {
		return nil, newPipelineAppError("artifact verification error", err.Error(), "Rebuild artifact: internal error during verification", output.ExitCodeGeneral)
	}
	duration := time.Since(start)
	result := &PipelineResult{
		Target:       target,
		DryRun:       cfg.DryRun,
		ArtifactID:   manifest.ArtifactID,
		Version:      manifest.Version,
		Checksum:     manifest.Checksum,
		ChecksumType: manifest.ChecksumType,
		ProjectID:    manifest.ProjectID,
		ArtifactPath: pkgRes.ArtifactPath,
		Manifest:     manifest,
		Verify:       vr,
		DurationMs:   duration.Milliseconds(),
	}
	if !vr.Passed {
		details := collectFailedChecks(vr)
		return result, newPipelineAppError("artifact verification FAIL — installer build rejected", details, "Do not embed tampered artifact — rebuild: anvil installer build will rebuild and re-verify. Run with --dry-run to inspect verify checks", output.ExitCodeGeneral)
	}

	// Step 3: --dry-run → verify only, no installer build (AC4)
	if cfg.DryRun {
		if cfg.JSONOutput {
			_ = RenderJSON(rawWriter, result, nil)
		} else {
			RenderHuman(logger, result, vr, manifest, duration)
			fmt.Fprintln(logger, "Dry-run complete — no installer built (verify only)")
		}
		return result, nil
	}

	// Step 4: Idempotency check (AC3) — reuse if same artifact checksum + config hash
	hash := computeStateHash(manifest.Checksum, cfg.InstallerConfig.Name, target, version)
	if st, err := loadInstallerState(outputDir); err == nil && st.Hash == hash && st.OutputFile != "" {
		existingPath := filepath.Join(outputDir, st.OutputFile)
		if _, err := os.Stat(existingPath); err == nil {
			result.Idempotent = true
			result.OutputPath = existingPath
			result.Installer = &BuilderResult{OutputPath: existingPath, FileName: st.OutputFile, SizeBytes: st.SizeBytes, Simulated: true}
			result.DurationMs = time.Since(start).Milliseconds()
			if cfg.JSONOutput {
				_ = RenderJSON(rawWriter, result, nil)
			} else {
				RenderHuman(logger, result, vr, manifest, duration)
				fmt.Fprintf(logger, "Idempotent: installer already built (hash %s) — skip rebuild → %s\n", hash, filepath.Base(existingPath))
			}
			return result, nil
		}
	}

	// Step 5: Builder invocation (AC2) — NSIS/Makeself mock (real would exec tooling)
	builder, _ := BuilderForTarget(target)
	iconPath := cfg.InstallerConfig.Icon
	if iconPath != "" && !filepath.IsAbs(iconPath) {
		iconPath = filepath.Join(projectRoot, iconPath)
	}
	// Ensure icon fixture exists for spike: if not found, create dummy per target
	if iconPath == "" || fileNotExists(iconPath) {
		iconPath = ensureIconFixture(projectRoot, target)
	}
	bRes, err := builder.Build(BuilderOpts{
		InstallerName: cfg.InstallerConfig.Name,
		IconPath:      iconPath,
		ArtifactPath:  pkgRes.ArtifactPath,
		Manifest:      manifest,
		OutputDir:     outputDir,
		Logger:        logger,
	})
	if err != nil {
		return result, newPipelineAppError("installer build failed", err.Error(), "Check icon path and output dir permissions; verify with --dry-run first", output.ExitCodeGeneral)
	}
	result.Installer = bRes
	result.OutputPath = bRes.OutputPath
	result.DurationMs = time.Since(start).Milliseconds()

	// Write idempotency state
	_ = saveInstallerState(outputDir, installerState{
		ArtifactID:    manifest.ArtifactID,
		Checksum:      manifest.Checksum,
		InstallerName: cfg.InstallerConfig.Name,
		Target:        target,
		Version:       version,
		OutputFile:    bRes.FileName,
		SizeBytes:     bRes.SizeBytes,
		Hash:          hash,
	})

	// Step 6: Render output (AC4 envelope v1)
	if cfg.JSONOutput {
		_ = RenderJSON(rawWriter, result, nil)
	} else {
		RenderHuman(logger, result, vr, manifest, time.Since(start))
	}
	return result, nil
}

func fileNotExists(p string) bool {
	_, err := os.Stat(p)
	return os.IsNotExist(err)
}

func ensureIconFixture(projectRoot, target string) string {
	dir := filepath.Join(projectRoot, "spikes", "installer-pipeline", "fixtures")
	_ = os.MkdirAll(dir, 0755)
	var path string
	var content []byte
	if target == "windows" {
		path = filepath.Join(dir, "app.ico")
		content = append([]byte("ICO\x00DUMMY-ICON-256x256-ANVIL"), make([]byte, 64)...)
	} else {
		path = filepath.Join(dir, "app.png")
		content = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("DUMMY-PNG-256x256-ANVIL")...)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.WriteFile(path, content, 0644)
	}
	return path
}

func loadInstallerState(outputDir string) (*installerState, error) {
	b, err := os.ReadFile(statePath(outputDir))
	if err != nil {
		return nil, err
	}
	var st installerState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func saveInstallerState(outputDir string, st installerState) error {
	_ = os.MkdirAll(outputDir, 0755)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(outputDir), b, 0644)
}

func collectFailedChecks(vr *artifact.VerificationResult) string {
	if vr == nil {
		return "verification result unavailable"
	}
	var buf bytes.Buffer
	first := true
	for _, c := range vr.Checks {
		if !c.Passed {
			if !first {
				buf.WriteString("; ")
			}
			fmt.Fprintf(&buf, "%s: %s", c.Name, c.Details)
			first = false
		}
	}
	if buf.Len() == 0 {
		return "verification failed (no details)"
	}
	return buf.String()
}

// ── Output renderers — human + json envelope v1 (AC4, mirrors cmd/deploy) ──

func RenderHuman(w io.Writer, res *PipelineResult, vr *artifact.VerificationResult, m *artifact.Manifest, dur time.Duration) {
	if w == nil {
		w = io.Discard
	}
	if res.DryRun {
		fmt.Fprintf(w, "Installer dry-run --target %s\n", res.Target)
	} else {
		fmt.Fprintf(w, "Installer build --target %s\n", res.Target)
	}
	fmt.Fprintf(w, "  Step: Build artifact ✓ (%s)\n", output.FormatDuration(dur/2))
	fmt.Fprintf(w, "    artifact_id: %s\n", m.ArtifactID)
	fmt.Fprintf(w, "    version: %s\n", m.Version)
	fmt.Fprintf(w, "    checksum: %s (%s)\n", m.Checksum, m.ChecksumType)
	fmt.Fprintf(w, "    path: %s\n", filepath.Base(res.ArtifactPath))
	fmt.Fprintf(w, "  Step: Verify artifact ✓ (%s)\n", output.FormatDuration(dur/2))
	if vr != nil && vr.Passed {
		output.PrintStatus(w, output.StatusPass, fmt.Sprintf("Verify %d checks PASS", len(vr.Checks)))
	} else {
		output.PrintStatus(w, output.StatusFail, "Verify FAIL")
	}
	if vr != nil {
		for _, c := range vr.Checks {
			st := output.StatusPass
			if !c.Passed {
				st = output.StatusFail
			}
			output.PrintStatus(w, st, fmt.Sprintf("%s: %s", c.Name, c.Details))
		}
	}
	if res.DryRun {
		fmt.Fprintf(w, "Dry-run complete — no installer built (verify only) (%s)\n", output.FormatDuration(dur))
		return
	}
	if res.Installer != nil {
		fmt.Fprintf(w, "  Step: Build installer ✓ (%s)\n", output.FormatDuration(dur/3))
		fmt.Fprintf(w, "    installer: %s (%d bytes)\n", filepath.Base(res.OutputPath), res.Installer.SizeBytes)
		if res.Idempotent {
			fmt.Fprintln(w, "    idempotent: skip rebuild (hash unchanged)")
		}
	}
	fmt.Fprintf(w, "Installer build complete — %s (%s)\n", filepath.Base(res.OutputPath), output.FormatDuration(dur))
}

func RenderJSON(w io.Writer, res *PipelineResult, appErr *output.AppError) error {
	if appErr != nil {
		return output.WriteJSONError(w, RedactInstallerLog(appErr.Error()))
	}
	data := map[string]interface{}{
		"target":        res.Target,
		"dry_run":       res.DryRun,
		"artifact_id":   res.ArtifactID,
		"version":       res.Version,
		"checksum":      res.Checksum,
		"checksum_type": res.ChecksumType,
		"project_id":    res.ProjectID,
		"artifact_path": filepath.Base(res.ArtifactPath),
		"verify":        res.Verify,
	}
	if !res.DryRun && res.OutputPath != "" {
		data["installer_path"] = filepath.Base(res.OutputPath)
		data["installer_size"] = res.Installer.SizeBytes
		data["idempotent"] = res.Idempotent
	}
	return output.WriteJSON(w, data)
}

// TamperArtifact creates a tampered copy of artifactPath (flips a byte) for AC3 tamper test.
func TamperArtifact(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(b) > 200 {
		b[200] ^= 0xFF
	} else if len(b) > 0 {
		b[0] ^= 0xFF
	}
	return os.WriteFile(dst, b, 0644)
}

// ── AppError helper (pipeline-level, exit codes stable) ──

type pipelineAppError struct {
	*output.AppError
}

func newPipelineAppError(message, reason, resolution string, exitCode int) error {
	return &output.AppError{
		Message:       message,
		Reason:        reason,
		Resolution:    resolution,
		ExitCodeValue: exitCode,
	}
}

// ValidateHumanJSONConsistency checks human contains ids + json envelope v1 success (AC4 gate, mirrors deploy).
func ValidateHumanJSONConsistency(human string, jsonOutput string, manifest *artifact.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest nil")
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonOutput), &env); err != nil {
		return err
	}
	var data map[string]interface{}
	if raw, ok := env["data"]; ok {
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("envelope missing data field")
	}
	fields := map[string]string{
		"artifact_id": manifest.ArtifactID,
		"version":     manifest.Version,
		"checksum":    manifest.Checksum,
		"project_id":  manifest.ProjectID,
	}
	for k, want := range fields {
		got, _ := data[k].(string)
		if got != want {
			return fmt.Errorf("mismatch %s: manifest %q vs json %q", k, want, got)
		}
	}
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(jsonOutput), &envelope); err != nil {
		return err
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		return fmt.Errorf("envelope invalid: version=%q status=%q want 1/success", envelope.Version, envelope.Status)
	}
	for _, needle := range []string{manifest.ArtifactID[:16], manifest.Version, manifest.Checksum[:16]} {
		if !bytes.Contains([]byte(human), []byte(needle)) {
			return fmt.Errorf("human missing %q", needle[:8])
		}
	}
	return nil
}
