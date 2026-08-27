package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AppImageBuilder — AppImage portable bundle.
// Largest overhead (squashfs + runtime), but zero-install & no privilege.
type AppImageBuilder struct{}

func (b *AppImageBuilder) ID() string          { return "appimage" }
func (b *AppImageBuilder) OS() string          { return "linux" }
func (b *AppImageBuilder) DisplayName() string { return "AppImage" }
func (b *AppImageBuilder) Extension() string   { return ".AppImage" }
func (b *AppImageBuilder) OverheadBytes() int64 { return 12 * 1024 * 1024 } // ~12MB runtime

func (b *AppImageBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	if payloadMB <= 0 {
		payloadMB = 5
	}
	// mksquashfs + appimagetool
	return time.Duration(1200+payloadMB*55) * time.Millisecond
}

func (b *AppImageBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "appimage",
		SilentSupport:    true, // just chmod +x and run; no installer silent needed
		SilentFlag:       "chmod +x app.AppImage && ./app.AppImage (or --appimage-extract)",
		GUISupport:       false, // no installer GUI; app itself may have GUI
		ChooseLocation:   false, // user chooses where to place AppImage file itself
		ShortcutSupport:  true,  // via appimaged / desktop integration prompt; .desktop inside AppDir
		UninstallSupport: true,  // delete file
		UninstallMethod:  "rm app.AppImage; desktop integration removed via appimaged --remove",
		AdminRequired:    "no",
		DefaultLocation:  "Anywhere (~/Applications, ~/bin, /opt) — user-chosen; no privileged install",
		Notes:            "Zero-install portable; heavy runtime overhead; update via AppImageUpdate; not ideal for system-wide deployment.",
	}
}

func (b *AppImageBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "appimage",
		Method:            "GPG detached .sig + optional embedded update info (AppImageUpdate signature)",
		SelfSignedCommand: "gpg --detach-sign app.AppImage  # produces app.AppImage.sig; appimagetool --sign app.AppImage (if key configured)",
		VerifyCommand:     "gpg --verify app.AppImage.sig app.AppImage; AppImageUpdate --verify-signature (if embedded)",
		TamperDetect:      "Detached GPG sig covers whole AppImage; tamper fails gpg --verify; no built-in OS enforcement like Authenticode.",
		FeasibleOnCI:      true,
		Notes:             "Self-signed same caveat as deb; detached sig must be distributed alongside AppImage; no central trust store.",
	}
}

func (b *AppImageBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
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
	fmt.Fprintf(buildLog, "[appimage] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[appimage] Payload=%d bytes (%.1f MB)\n", payloadBytes, float64(payloadBytes)/1024/1024)
	fmt.Fprintf(buildLog, "[appimage] Icon: %s (ok=%t) — AppDir/usr/share/icons/hicolor/256x256/apps + .desktop\n", iconDetail, iconOK)
	fmt.Fprintf(buildLog, "[appimage] Simulated=%t (needs mksquashfs + appimagetool)\n", IsSimulated(b.ID()))
	fmt.Fprintf(buildLog, "[appimage] Structure: AppDir/AppRun + AppDir/app/* + AppDir/<name>.desktop + AppDir/<name>.png\n")
	fmt.Fprintf(buildLog, "[appimage] Build: appimagetool AppDir %s\n", filename)

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	outputPath := filepath.Join(cfg.OutputDir, filename)
	header := fmt.Sprintf("APPIMAGE-ELF-HEADER + SQUASHFS Runtime=12M Name=%s IconOK=%t Simulated=%t", sanitized, iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	// chmod +x
	_ = os.Chmod(outputPath, 0755)
	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	fmt.Fprintf(buildLog, "[appimage] Build done: %s (%d bytes, overhead %d bytes) in %s (chmod +x)\n", outputPath, totalSize, overhead, synth)

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
