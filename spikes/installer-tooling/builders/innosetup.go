package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InnoBuilder — Inno Setup (JRSoftware).
// Minimal, Delphi-based, leaner than NSIS for simple installs.
type InnoBuilder struct{}

func (b *InnoBuilder) ID() string          { return "inno" }
func (b *InnoBuilder) OS() string          { return "windows" }
func (b *InnoBuilder) DisplayName() string { return "Inno Setup" }
func (b *InnoBuilder) Extension() string   { return ".exe" }
func (b *InnoBuilder) OverheadBytes() int64 { return 1200 * 1024 } // ~1.2MB

func (b *InnoBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	if payloadMB <= 0 {
		payloadMB = 5
	}
	return time.Duration(900+payloadMB*45) * time.Millisecond
}

func (b *InnoBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "inno",
		SilentSupport:    true,
		SilentFlag:       "/SILENT or /VERYSILENT (+ /SUPPRESSMSGBOXES, /NORESTART)",
		GUISupport:       true,
		ChooseLocation:   true,
		ShortcutSupport:  true,
		UninstallSupport: true,
		UninstallMethod:  "unins000.exe + ARP entry; [UninstallDelete] section",
		AdminRequired:    "optional", // PrivilegesRequired=lowest|admin
		DefaultLocation:  "{autopf}\\<Name> (or {localappdata}\\<Name> if lowest)",
		Notes:            "Simplest scripting ([Setup]/[Files]/[Icons]); less flexible than NSIS for complex logic.",
	}
}

func (b *InnoBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "inno",
		Method:            "Authenticode via signtool/osslsigncode on resulting .exe (SignedUninstaller optional)",
		SelfSignedCommand: "osslsigncode sign -certs dummy.crt -key dummy.key -in app.exe -out app-signed.exe",
		VerifyCommand:     "osslsigncode verify app-signed.exe",
		TamperDetect:      "Same Authenticode digest as NSIS; Inno SignedUninstaller adds inner signature for uninstaller.",
		FeasibleOnCI:      true,
		Notes:             "Inno can sign uninstaller separately via [Setup] SignedUninstaller=yes + SignTool directive.",
	}
}

func (b *InnoBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
	sanitized := SanitizeInstallerName(cfg.InstallerName)
	filename := RenderedFilename(sanitized, b.Extension())
	// Inno also uses -Setup.exe; but to disambiguate from NSIS in same dir, keep same pattern — tests check name contains sanitized
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
	fmt.Fprintf(buildLog, "[inno] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[inno] Payload=%d bytes (%.1f MB)\n", payloadBytes, float64(payloadBytes)/1024/1024)
	fmt.Fprintf(buildLog, "[inno] Icon: %s (ok=%t) — SetupIconFile=%s\n", iconDetail, iconOK, cfg.IconPath)
	fmt.Fprintf(buildLog, "[inno] Simulated=%t (needs iscc on Windows)\n", IsSimulated(b.ID()))
	fmt.Fprintf(buildLog, "[inno] Script: [Setup] AppName=%s DefaultDirName={autopf}\\%s SetupIconFile=%s\n", sanitized, sanitized, cfg.IconPath)
	fmt.Fprintf(buildLog, "[inno] Silent: /SILENT ; GUI: wizard pages\n")

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	// disambiguate filename if NSIS already wrote same name — prefix with inno- for uniqueness in shared OutputDir
	// But RenderedFilename for .exe is deterministic; keep identical for AC2 name-render test, add suffix only if collision
	outputPath := filepath.Join(cfg.OutputDir, filename)
	if _, err := os.Stat(outputPath); err == nil {
		// collision with NSIS output — use distinct name for evidence but preserve NameRendered as canonical
		outputPath = filepath.Join(cfg.OutputDir, strings.TrimSuffix(filename, ".exe")+"-inno.exe")
		filename = filepath.Base(outputPath)
	}
	header := fmt.Sprintf("INNO-SETUP-HEADER AppName=%s IconOK=%t Simulated=%t", sanitized, iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	fmt.Fprintf(buildLog, "[inno] Build done: %s (%d bytes, overhead %d bytes) in %s\n", outputPath, totalSize, overhead, synth)

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
