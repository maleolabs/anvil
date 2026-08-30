package builders

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SanitizeInstallerName mirrors expected anvil.yaml installer.name rendering.
// Strips invalid chars, trims, defaults to "anvil" if empty.
// Keeps alphanumeric, dash, underscore, space -> replaces space with dash for filename safety where needed.
func SanitizeInstallerName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "anvil"
	}
	// allow letters, digits, -, _, space only
	re := regexp.MustCompile(`[^a-zA-Z0-9\-_ ]+`)
	sanitized := re.ReplaceAllString(raw, "")
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "anvil"
	}
	// collapse multiple spaces
	sanitized = regexp.MustCompile(`\s+`).ReplaceAllString(sanitized, " ")
	return sanitized
}

// RenderedFilename maps sanitized name -> per-tool installer filename.
func RenderedFilename(sanitizedName, ext string) string {
	switch ext {
	case ".exe":
		// NSIS/Inno: "<Name>-Setup.exe"
		return fmt.Sprintf("%s-Setup%s", strings.ReplaceAll(sanitizedName, " ", "-"), ext)
	case ".msi":
		// WiX: "<Name>.msi" (or with version, spike uses plain)
		return fmt.Sprintf("%s%s", strings.ReplaceAll(sanitizedName, " ", "-"), ext)
	case ".deb":
		// deb: lowercase, no spaces => "<name>_1.0.0_amd64.deb"
		lower := strings.ToLower(strings.ReplaceAll(sanitizedName, " ", "-"))
		lower = regexp.MustCompile(`[^a-z0-9\-_.]+`).ReplaceAllString(lower, "")
		if lower == "" {
			lower = "anvil"
		}
		return fmt.Sprintf("%s_1.0.0_amd64%s", lower, ext)
	case ".AppImage":
		return fmt.Sprintf("%s%s", strings.ReplaceAll(sanitizedName, " ", "-"), ext)
	case ".run":
		return fmt.Sprintf("%s%s", strings.ReplaceAll(sanitizedName, " ", "-"), ext)
	default:
		return sanitizedName + ext
	}
}

// LoadInstallerName reads installer.name from anvil.yaml at repo root.
// Falls back to "anvil" if file missing or field absent.
func LoadInstallerName(repoRoot string) string {
	candidates := []string{
		filepath.Join(repoRoot, "anvil.yaml"),
		"anvil.yaml",
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			continue
		}
		// try installer.name
		if inst, ok := doc["installer"].(map[string]interface{}); ok {
			if name, ok := inst["name"].(string); ok && strings.TrimSpace(name) != "" {
				return SanitizeInstallerName(name)
			}
		}
		// also try project.name fallback
		if proj, ok := doc["project"].(map[string]interface{}); ok {
			if name, ok := proj["name"].(string); ok && strings.TrimSpace(name) != "" {
				// use project name as installer name if installer.name missing (spike convenience)
				return SanitizeInstallerName(name)
			}
		}
	}
	return "anvil"
}

// CreateDummyPayload creates an incompressible payload file of sizeMB in workDir/app/payload.bin
// Returns payload size bytes. Uses crypto/rand for incompressibility (mirrors AC1 realism).
func CreateDummyPayload(workDir string, sizeMB int) (string, int64, error) {
	if sizeMB <= 0 {
		sizeMB = 5
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "", 0, err
	}
	payloadPath := filepath.Join(appDir, "payload.bin")
	f, err := os.Create(payloadPath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	chunk := make([]byte, 1<<20) // 1MB
	for i := 0; i < sizeMB; i++ {
		if _, err := io.ReadFull(rand.Reader, chunk); err != nil {
			return "", 0, err
		}
		if _, err := f.Write(chunk); err != nil {
			return "", 0, err
		}
	}
	if err := f.Sync(); err != nil {
		return "", 0, err
	}
	info, _ := os.Stat(payloadPath)
	var sz int64
	if info != nil {
		sz = info.Size()
	}
	// also drop a minimal entrypoint so installer has content beyond payload
	_ = os.WriteFile(filepath.Join(appDir, "index.php"), []byte("<?php echo 'anvil-hello';"), 0644)
	return payloadPath, sz, nil
}

// CreateIconFixtures ensures dummy icon files exist for AC2.
// Returns map[ext]path.
func CreateIconFixtures(dir string) map[string]string {
	_ = os.MkdirAll(dir, 0755)
	m := make(map[string]string)
	// minimal ICO header (not valid icon but distinguishable for test): "ICO\x00" + 32 bytes
	icoPath := filepath.Join(dir, "app.ico")
	if _, err := os.Stat(icoPath); os.IsNotExist(err) {
		_ = os.WriteFile(icoPath, append([]byte("ICO\x00DUMMY-ICON-256x256-ANVIL"), make([]byte, 64)...), 0644)
	}
	m["ico"] = icoPath
	pngPath := filepath.Join(dir, "app.png")
	if _, err := os.Stat(pngPath); os.IsNotExist(err) {
		// PNG magic
		_ = os.WriteFile(pngPath, append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("DUMMY-PNG-256x256-ANVIL")...), 0644)
	}
	m["png"] = pngPath
	return m
}

// VerifyIcon checks icon path matches expected type for builder OS.
// Windows: requires .ico; Linux: requires .png (or .svg/.xpm accepted).
func VerifyIcon(iconPath, osType string) (bool, string) {
	if iconPath == "" {
		return false, "no icon provided"
	}
	if _, err := os.Stat(iconPath); err != nil {
		return false, fmt.Sprintf("icon not found: %s", iconPath)
	}
	ext := strings.ToLower(filepath.Ext(iconPath))
	switch osType {
	case "windows":
		if ext == ".ico" {
			return true, fmt.Sprintf("icon %s verified as Windows .ico", filepath.Base(iconPath))
		}
		return false, fmt.Sprintf("windows expects .ico, got %s", ext)
	case "linux":
		if ext == ".png" || ext == ".svg" || ext == ".xpm" {
			return true, fmt.Sprintf("icon %s verified as Linux desktop icon (%s)", filepath.Base(iconPath), ext)
		}
		return false, fmt.Sprintf("linux expects .png/.svg/.xpm, got %s", ext)
	default:
		return false, "unknown OS type"
	}
}

// WriteSizedFile creates a file at path with exactly size bytes.
// Writes a deterministic header then fills remaining with pattern.
// Ensures realistic size measurement on FS (not sparse where possible).
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
	// write in 1M chunks of pattern
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

// IsSimulated returns true when native toolchain is absent (Linux CI for Windows tools).
// For spike, Windows builders are always simulated on Linux; Linux builders simulate only if tool absent.
func IsSimulated(toolID string) bool {
	// Windows tools are simulated on Linux CI by design
	switch toolID {
	case "nsis", "wix", "inno":
		return true
	case "deb", "appimage", "makeself":
		// try to detect native tool; if missing, simulated
		// deb: dpkg-deb, appimage: appimagetool, makeself: makeself.sh
		// For determinism in spike, treat as simulated unless env SPIKE_REAL_LINUX=1
		if os.Getenv("SPIKE_REAL_LINUX") == "1" {
			return false
		}
		return true
	}
	return true
}
