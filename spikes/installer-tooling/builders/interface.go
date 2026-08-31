package builders

import (
	"io"
	"time"
)

// BuildConfig is per-builder input. Mirrors anvil.yaml installer.name + icon.
type BuildConfig struct {
	InstallerName string // from anvil.yaml#installer.name (or default "anvil")
	IconPath      string // filesystem path to icon fixture (.ico for Windows, .png for Linux)
	PayloadSizeMB int    // dummy payload size (default 5 fast, 50 lab)
	OutputDir     string // where installer artifact is written
	WorkDir       string // temp workdir per builder (payload staging)
	Logger        io.Writer
}

// BuildResult is one builder execution evidence.
type BuildResult struct {
	Tool           string        `json:"tool"`
	OS             string        `json:"os"` // windows | linux
	OutputPath     string        `json:"output_path"`
	OutputFileName string        `json:"output_filename"`
	InstallerName  string        `json:"installer_name"` // sanitized rendered name
	BuildDuration  time.Duration `json:"build_duration"`
	SizeBytes      int64         `json:"size_bytes"`
	OverheadBytes  int64         `json:"overhead_bytes"`
	PayloadBytes   int64         `json:"payload_bytes"`
	Success        bool          `json:"success"`
	Error          string        `json:"error,omitempty"`
	Log            string        `json:"log,omitempty"`
	IconVerified   bool          `json:"icon_verified"`
	IconDetail     string        `json:"icon_detail"`
	NameRendered   string        `json:"name_rendered"`
	Simulated      bool          `json:"simulated"` // true on Linux CI for Windows tools
	BuildLogPath   string        `json:"build_log_path,omitempty"`
}

// UXFeatures captures AC4 evaluation per tooling.
type UXFeatures struct {
	Tool             string `json:"tool"`
	SilentSupport    bool   `json:"silent_support"`
	SilentFlag       string `json:"silent_flag"`
	GUISupport       bool   `json:"gui_support"`
	ChooseLocation   bool   `json:"choose_location"`
	ShortcutSupport  bool   `json:"shortcut_support"`
	UninstallSupport bool   `json:"uninstall_support"`
	UninstallMethod  string `json:"uninstall_method"`
	AdminRequired    string `json:"admin_required"` // "no" | "optional" | "yes" | "per-machine"
	DefaultLocation  string `json:"default_location"`
	Notes            string `json:"notes"`
}

// SigningInfo captures AC3 per tooling.
type SigningInfo struct {
	Tool              string `json:"tool"`
	Method            string `json:"method"`
	SelfSignedCommand string `json:"self_signed_command"`
	VerifyCommand     string `json:"verify_command"`
	TamperDetect      string `json:"tamper_detect"`
	FeasibleOnCI      bool   `json:"feasible_on_ci"`
	Notes             string `json:"notes"`
}

// Builder is materialized per tooling (NSIS, WiX, etc.)
type Builder interface {
	ID() string // e.g. "nsis"
	OS() string // "windows" | "linux"
	DisplayName() string
	Extension() string // .exe, .msi, .deb, .AppImage, .run
	Build(cfg BuildConfig) (*BuildResult, error)
	UX() UXFeatures
	Signing() SigningInfo
	OverheadBytes() int64                               // nominal overhead for size calc
	SyntheticBuildDuration(payloadMB int) time.Duration // nominal build time for matrix
}

// All returns builders in deterministic evaluation order: Windows 3, Linux 3.
func All() []Builder {
	return []Builder{
		&NSISBuilder{},
		&WiXBuilder{},
		&InnoBuilder{},
		&DebBuilder{},
		&AppImageBuilder{},
		&MakeselfBuilder{},
	}
}

// ByID lookup.
func ByID(id string) Builder {
	for _, b := range All() {
		if b.ID() == id {
			return b
		}
	}
	return nil
}
