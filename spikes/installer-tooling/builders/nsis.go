package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NSISBuilder — Nullsoft Scriptable Install System.
// Fast, smallest scripting overhead, best for MVP Windows.
type NSISBuilder struct{}

func (b *NSISBuilder) ID() string           { return "nsis" }
func (b *NSISBuilder) OS() string           { return "windows" }
func (b *NSISBuilder) DisplayName() string  { return "NSIS" }
func (b *NSISBuilder) Extension() string    { return ".exe" }
func (b *NSISBuilder) OverheadBytes() int64 { return 1600 * 1024 } // ~1.6MB stub + MUI

func (b *NSISBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	// NSIS fastest Windows: base 800ms + ~30ms per MB (LZMA)
	if payloadMB <= 0 {
		payloadMB = 5
	}
	return time.Duration(800+payloadMB*30) * time.Millisecond
}

func (b *NSISBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "nsis",
		SilentSupport:    true,
		SilentFlag:       "/S",
		GUISupport:       true,
		ChooseLocation:   true,
		ShortcutSupport:  true,
		UninstallSupport: true,
		UninstallMethod:  "uninstall.exe generated + ARP entry (Add/Remove Programs)",
		AdminRequired:    "optional", // can install to $LOCALAPPDATA vs $PROGRAMFILES
		DefaultLocation:  "$PROGRAMFILES\\<Name> (per-machine) or $LOCALAPPDATA\\<Name> (per-user)",
		Notes:            "Modern UI 2; per-user fallback avoids UAC; mature community.",
	}
}

func (b *NSISBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "nsis",
		Method:            "Authenticode via signtool.exe / osslsigncode",
		SelfSignedCommand: "openssl req -x509 -newkey rsa:2048 -keyout dummy.key -out dummy.crt -days 1 -nodes -subj \"/CN=Anvil Spike\" && osslsigncode sign -certs dummy.crt -key dummy.key -in app.exe -out app-signed.exe",
		VerifyCommand:     "osslsigncode verify app-signed.exe  OR  signtool verify /pa app-signed.exe",
		TamperDetect:      "Authenticode digest (SHA256) embedded; Windows SmartScreen warns on tamper; verify fails if binary modified after signing.",
		FeasibleOnCI:      true,
		Notes:             "Self-signed cert triggers SmartScreen on real Windows unless cert installed to Trusted Root; spike uses dummy cert only.",
	}
}

func (b *NSISBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
	start := time.Now()
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
	fmt.Fprintf(buildLog, "[nsis] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[nsis] Payload=%d bytes (%.1f MB) in %s\n", payloadBytes, float64(payloadBytes)/1024/1024, payloadPath)
	fmt.Fprintf(buildLog, "[nsis] Icon: %s (ok=%t)\n", iconDetail, iconOK)
	fmt.Fprintf(buildLog, "[nsis] Simulated=%t (Linux CI — real build needs makensis on Windows runner)\n", IsSimulated(b.ID()))
	// NSIS script snippet
	fmt.Fprintf(buildLog, "[nsis] Script: OutFile \"%s\"; Name \"%s\"; InstallDir \"$PROGRAMFILES\\%s\"; Icon \"%s\"\n", filename, sanitized, sanitized, cfg.IconPath)
	fmt.Fprintf(buildLog, "[nsis] Sections: payload + shortcut ($SMPROGRAMS), uninstaller, registry ARP\n")
	fmt.Fprintf(buildLog, "[nsis] Silent flag: /S ; GUI: Modern UI 2\n")

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	outputPath := filepath.Join(cfg.OutputDir, filename)
	// Add 4KB NSIS header simulation
	header := fmt.Sprintf("NSIS-MZ-HEADER Name=%s IconOK=%t Simulated=%t", sanitized, iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	// synthetic duration (with minimal real sleep for timing realism)
	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	duration := synth // evidence uses synthetic for matrix determinism
	// log size
	fmt.Fprintf(buildLog, "[nsis] Build done: %s (%d bytes, overhead %d bytes) in %s (simulated: slept %s)\n", outputPath, totalSize, overhead, duration, sleepFor)
	_ = start // keep real start for future

	return &BuildResult{
		Tool:           b.ID(),
		OS:             b.OS(),
		OutputPath:     outputPath,
		OutputFileName: filename,
		InstallerName:  sanitized,
		BuildDuration:  duration,
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
