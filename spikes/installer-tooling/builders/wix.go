package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WiXBuilder — WiX Toolset (Windows Installer MSI).
// Enterprise, slowest, largest overhead, best for GPO/AD.
type WiXBuilder struct{}

func (b *WiXBuilder) ID() string          { return "wix" }
func (b *WiXBuilder) OS() string          { return "windows" }
func (b *WiXBuilder) DisplayName() string { return "WiX Toolset" }
func (b *WiXBuilder) Extension() string   { return ".msi" }
func (b *WiXBuilder) OverheadBytes() int64 { return 4200 * 1024 } // ~4.2MB MSI metadata, CAB

func (b *WiXBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	if payloadMB <= 0 {
		payloadMB = 5
	}
	// WiX slowest: candle+light+heat, XML/XSLT
	return time.Duration(2800+payloadMB*120) * time.Millisecond
}

func (b *WiXBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "wix",
		SilentSupport:    true,
		SilentFlag:       "msiexec /qn /i app.msi",
		GUISupport:       true,
		ChooseLocation:   true, // via WixUI_InstallDir
		ShortcutSupport:  true,
		UninstallSupport: true,
		UninstallMethod:  "msiexec /x {ProductCode} + ARP; native Windows Installer transactional uninstall/rollback",
		AdminRequired:    "yes", // MSI per-machine by default; per-user possible but atypical
		DefaultLocation:  "ProgramFilesFolder\\<Name> (requires elevation)",
		Notes:            "Best for enterprise GPO deployment; steep learning curve (XML/heat); deterministic GUIDs needed.",
	}
}

func (b *WiXBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "wix",
		Method:            "Authenticode on .msi via signtool / osslsigncode (same as EXE)",
		SelfSignedCommand: "osslsigncode sign -certs dummy.crt -key dummy.key -in app.msi -out app-signed.msi && msiexec /a app-signed.msi /qb TARGETDIR=C:\\temp\\verify",
		VerifyCommand:     "osslsigncode verify app-signed.msi; signtool verify /pa app-signed.msi",
		TamperDetect:      "MSI Authenticode covers entire MSI stream; tamper breaks signature; MsiVerifyPackage would fail.",
		FeasibleOnCI:      true,
		Notes:             "Self-signed same SmartScreen caveat as NSIS; WiX msi benefits from EV cert for enterprise trust.",
	}
}

func (b *WiXBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
	start := time.Now()
	_ = start
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
	fmt.Fprintf(buildLog, "[wix] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[wix] Payload=%d bytes (%.1f MB)\n", payloadBytes, float64(payloadBytes)/1024/1024)
	fmt.Fprintf(buildLog, "[wix] Icon: %s (ok=%t) — Wix Icon Id embedded in .wxs\n", iconDetail, iconOK)
	fmt.Fprintf(buildLog, "[wix] Simulated=%t (needs candle.exe + light.exe on Windows)\n", IsSimulated(b.ID()))
	fmt.Fprintf(buildLog, "[wix] Source: Product.wxs (ProductCode GUID, UpgradeCode GUID), heat dir app -cg AppFiles -dr INSTALLFOLDER\n")
	fmt.Fprintf(buildLog, "[wix] Build: candle Product.wxs -o Product.wixobj && light Product.wixobj -o %s\n", filename)
	fmt.Fprintf(buildLog, "[wix] Silent: msiexec /qn /i %s  GUI: WixUI_InstallDir\n", filename)

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	outputPath := filepath.Join(cfg.OutputDir, filename)
	header := fmt.Sprintf("MSI-HEADER WiX Product=%s IconOK=%t Simulated=%t ; OLE-CFB + CAB streams", sanitized, iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	fmt.Fprintf(buildLog, "[wix] Build done: %s (%d bytes, overhead %d bytes) in %s\n", outputPath, totalSize, overhead, synth)

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
