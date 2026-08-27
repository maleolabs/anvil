package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MakeselfBuilder — Makeself (.run) shell self-extracting archive.
// Smallest overhead, simplest UX, fallback for no-privilege installs.
type MakeselfBuilder struct{}

func (b *MakeselfBuilder) ID() string          { return "makeself" }
func (b *MakeselfBuilder) OS() string          { return "linux" }
func (b *MakeselfBuilder) DisplayName() string { return "Makeself" }
func (b *MakeselfBuilder) Extension() string   { return ".run" }
func (b *MakeselfBuilder) OverheadBytes() int64 { return 48 * 1024 } // ~48KB shell stub

func (b *MakeselfBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	if payloadMB <= 0 {
		payloadMB = 5
	}
	// shell script + tar.gz pack very fast
	return time.Duration(200+payloadMB*14) * time.Millisecond
}

func (b *MakeselfBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "makeself",
		SilentSupport:    true,
		SilentFlag:       "./app.run -- --silent (or --target /path --noexec + manual extract)",
		GUISupport:       false, // shell CLI only; GUI possible via dialog/zenity hook in startup script
		ChooseLocation:   true,  // --target <dir>
		ShortcutSupport:  true,  // startup script creates ~/.local/share/applications + ~/.local/bin symlink
		UninstallSupport: true,
		UninstallMethod:  "rm -rf <install-dir> + rm ~/.local/share/applications/<name>.desktop (startup script provides --uninstall)",
		AdminRequired:    "no", // installs to ~ or /opt depending on --target; no privilege if target in $HOME
		DefaultLocation:  "~/.local/<name> (per-user, no root) or /opt/<name> (if run as root)",
		Notes:            "Lowest overhead; best for single-binary distribution; no package manager integration; user must trust shell script.",
	}
}

func (b *MakeselfBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "makeself",
		Method:            "GPG detached .sig + SHA256 checksum file (same directory)",
		SelfSignedCommand: "gpg --detach-sign app.run && sha256sum app.run > app.run.sha256 && gpg --clearsign app.run.sha256",
		VerifyCommand:     "gpg --verify app.run.sig app.run && sha256sum -c app.run.sha256; gpg --verify app.run.sha256.asc",
		TamperDetect:      "Detached GPG sig OR checksum mismatch detects tamper; no OS-level enforcement — user must verify manually before chmod +x.",
		FeasibleOnCI:      true,
		Notes:             "Self-signed same distribution caveat; Makeself --gpg-extra can embed GPG check in extractor; spike uses detached sig.",
	}
}

func (b *MakeselfBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
	sanitized := SanitizeInstallerName(cfg.InstallerName)
	filename := RenderedFilename(sanitized, b.Extension())
	if cfg.OutputDir == "" {
		cfg.OutputDir = os.TempDir()
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = os.TempDir()
	}
	_ = os.MkdirAll(cfg.OutputDir, 0755)
	_ = os.MkdirAll(cfg.WorkDir, 0755)

	payloadPath, payloadBytes, err := CreateDummyPayload(cfg.WorkDir, cfg.PayloadSizeMB)
	if err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error()}, err
	}
	_ = payloadPath

	iconOK, iconDetail := VerifyIcon(cfg.IconPath, b.OS())
	buildLog := &strings.Builder{}
	fmt.Fprintf(buildLog, "[makeself] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[makeself] Payload=%d bytes (%.1f MB)\n", payloadBytes, float64(payloadBytes)/1024/1024)
	fmt.Fprintf(buildLog, "[makeself] Icon: %s (ok=%t) — startup script installs .desktop to ~/.local/share/applications\n", iconDetail, iconOK)
	fmt.Fprintf(buildLog, "[makeself] Simulated=%t (needs makeself.sh)\n", IsSimulated(b.ID()))
	fmt.Fprintf(buildLog, "[makeself] Startup script: creates <target>/app, .desktop, symlink ~/.local/bin/%s\n", strings.ToLower(sanitized))
	fmt.Fprintf(buildLog, "[makeself] Build: makeself.sh --sha256 --gpg --target /tmp %s \"Anvil %s\" ./startup.sh\n", cfg.WorkDir, sanitized)

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	outputPath := filepath.Join(cfg.OutputDir, filename)
	header := fmt.Sprintf("#!/bin/sh\n# Makeself self-extracting archive Name=%s IconOK=%t Simulated=%t\n# STUB 48KB + payload tar.gz\n", sanitized, iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	_ = os.Chmod(outputPath, 0755)
	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	fmt.Fprintf(buildLog, "[makeself] Build done: %s (%d bytes, overhead %d bytes) in %s (chmod +x)\n", outputPath, totalSize, overhead, synth)

	return &BuildResult{
		Tool:           b.ID(),
		OS:             b.OS(),
		OutputPath:     outputPath,
		OutputFileName: filename,
		InstallerName:  sanitized,
		BuildDuration:  synth,
		SizeBytes:      totalSize,
		OverheadBytes:  overhead,
		PayloadBytes:   payloadBytes,
		Success:        true,
		Log:            buildLog.String(),
		IconVerified:   iconOK,
		IconDetail:     iconDetail,
		NameRendered:   filename,
		Simulated:      IsSimulated(b.ID()),
	}, nil
}
