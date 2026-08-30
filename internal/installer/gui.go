package installer

import "os"

// HasGUI reports whether a Tauri webview can be rendered.
// Mirrors Rust has_gui() logic: Windows always true, Linux checks DISPLAY/WAYLAND + overrides.
func HasGUI() bool {
	if os.Getenv("ANVIL_FORCE_TUI") == "1" {
		return false
	}
	if os.Getenv("ANVIL_FORCE_GUI") == "1" {
		return true
	}
	// On Linux, require display env
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		// In tests, allow ANVIL_HAS_WEBKIT override to simulate GUI
		if os.Getenv("ANVIL_HAS_WEBKIT") == "1" {
			return true
		}
		return false
	}
	if v := os.Getenv("ANVIL_HAS_WEBKIT"); v == "0" {
		return false
	}
	return true
}

// IsGUIAvailable is alias for HasGUI for CLI dispatch readability.
func IsGUIAvailable() bool { return HasGUI() }

// InstallerGUIBuildScript is the cargo tauri build command.
const InstallerGUIBuildScript = "cargo tauri build"
