package builders

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DebBuilder — Debian package (.deb).
// Native apt/dpkg distribution, smallest Linux packaging overhead among managed options.
type DebBuilder struct{}

func (b *DebBuilder) ID() string           { return "deb" }
func (b *DebBuilder) OS() string           { return "linux" }
func (b *DebBuilder) DisplayName() string  { return "deb (dpkg)" }
func (b *DebBuilder) Extension() string    { return ".deb" }
func (b *DebBuilder) OverheadBytes() int64 { return 800 * 1024 } // ~0.8MB control + ar + compressed metadata

func (b *DebBuilder) SyntheticBuildDuration(payloadMB int) time.Duration {
	if payloadMB <= 0 {
		payloadMB = 5
	}
	// dpkg-deb fast: tar + ar
	return time.Duration(400+payloadMB*16) * time.Millisecond
}

func (b *DebBuilder) UX() UXFeatures {
	return UXFeatures{
		Tool:             "deb",
		SilentSupport:    true,
		SilentFlag:       "dpkg -i / DEBIAN_FRONTEND=noninteractive apt-get install -y",
		GUISupport:       false, // CLI native; GUI via Software Center (apt URL) not installer GUI
		ChooseLocation:   false, // fixed: /opt/<name> or /usr/share; relocatable via --instdir (rare)
		ShortcutSupport:  true,  // via /usr/share/applications/<name>.desktop
		UninstallSupport: true,
		UninstallMethod:  "dpkg -r <pkg> / apt remove <pkg>; prerm/postrm scripts",
		AdminRequired:    "yes", // dpkg requires root
		DefaultLocation:  "/opt/<name> or /usr/share/<name> (system-wide; root required)",
		Notes:            "Best for apt repo distribution; not portable across distros without repo; no GUI location chooser.",
	}
}

func (b *DebBuilder) Signing() SigningInfo {
	return SigningInfo{
		Tool:              "deb",
		Method:            "GPG detached (.dsc) + dpkg-sig + apt SecureApt (Release.gpg / InRelease)",
		SelfSignedCommand: "gpg --batch --gen-key dummy-gen && dpkg-sig -k <keyID> --sign builder app.deb && ar t app.deb # verify _gpgbuilder; apt-ftparchive release . > Release && gpg --clearsign -o InRelease Release",
		VerifyCommand:     "dpkg-sig --verify app.deb; gpg --verify InRelease; debsig-verify --policy generic app.deb",
		TamperDetect:      "dpkg-sig embeds GPG signature in ar member _gpgbuilder; tamper breaks signature; apt SecureApt validates Release/InRelease checksums (SHA256) for entire repo.",
		FeasibleOnCI:      true,
		Notes:             "Self-signed GPG key must be distributed via apt-key/keyring package; spike uses throwaway key (no real distribution).",
	}
}

func (b *DebBuilder) Build(cfg BuildConfig) (*BuildResult, error) {
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
	fmt.Fprintf(buildLog, "[deb] InstallerName=%q sanitized=%q filename=%q\n", cfg.InstallerName, sanitized, filename)
	fmt.Fprintf(buildLog, "[deb] Payload=%d bytes (%.1f MB)\n", payloadBytes, float64(payloadBytes)/1024/1024)
	fmt.Fprintf(buildLog, "[deb] Icon: %s (ok=%t) — installed to /usr/share/pixmaps + /usr/share/applications/<name>.desktop Icon=\n", iconDetail, iconOK)
	fmt.Fprintf(buildLog, "[deb] Simulated=%t\n", IsSimulated(b.ID()))
	// control file content simulation
	controlContent := fmt.Sprintf("Package: %s\nVersion: 1.0.0\nSection: utils\nPriority: optional\nArchitecture: amd64\nMaintainer: Anvil Spike <spike@anvil.test>\nDescription: %s - dummy payload\n", strings.ToLower(strings.ReplaceAll(sanitized, " ", "-")), sanitized)
	fmt.Fprintf(buildLog, "[deb] Control: %s\n", strings.ReplaceAll(controlContent, "\n", " | "))
	desktopContent := fmt.Sprintf("[Desktop Entry]\nName=%s\nExec=/opt/%s/app\nIcon=/usr/share/pixmaps/%s.png\nType=Application\n", sanitized, strings.ToLower(sanitized), strings.ToLower(sanitized))
	fmt.Fprintf(buildLog, "[deb] Desktop: %s\n", strings.ReplaceAll(desktopContent, "\n", " | "))
	fmt.Fprintf(buildLog, "[deb] Build: dpkg-deb --build %s %s\n", cfg.WorkDir, filename)

	overhead := b.OverheadBytes()
	totalSize := payloadBytes + overhead
	outputPath := filepath.Join(cfg.OutputDir, filename)
	header := fmt.Sprintf("DEB-AR-HEADER debian-binary=2.0 control=%s IconOK=%t Simulated=%t", strings.TrimSpace(controlContent), iconOK, IsSimulated(b.ID()))
	if err := WriteSizedFile(outputPath, totalSize, header); err != nil {
		return &BuildResult{Tool: b.ID(), OS: b.OS(), Success: false, Error: err.Error(), Log: buildLog.String()}, err
	}
	// also simulate .desktop + icon placement evidence in workdir
	debRoot := filepath.Join(cfg.WorkDir, "deb-root")
	_ = os.MkdirAll(filepath.Join(debRoot, "usr/share/applications"), 0755)
	_ = os.WriteFile(filepath.Join(debRoot, "usr/share/applications", strings.ToLower(sanitized)+".desktop"), []byte(desktopContent), 0644)

	synth := b.SyntheticBuildDuration(cfg.PayloadSizeMB)
	sleepFor := synth
	if sleepFor > 50*time.Millisecond {
		sleepFor = 50 * time.Millisecond
	}
	time.Sleep(sleepFor)
	fmt.Fprintf(buildLog, "[deb] Build done: %s (%d bytes, overhead %d bytes) in %s\n", outputPath, totalSize, overhead, synth)

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
