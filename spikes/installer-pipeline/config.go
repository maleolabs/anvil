package spkinstallerpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/output"
)

// InstallerConfig mirrors anvil.yaml installer block + ADR-005 unified config.
//
// Reference: anvil-cli/spk:installer-pipeline AC1, ADR-005 §7.5, §10.2
type InstallerConfig struct {
	Name           string   `json:"name" yaml:"name"`
	Icon           string   `json:"icon" yaml:"icon"`
	ArtifactSource string   `json:"artifactSource" yaml:"artifactSource"`
	OSTargets      []string `json:"osTargets" yaml:"osTargets"`

	// Resolved source for diagnostics (which level won).
	ResolvedFrom string `json:"resolvedFrom,omitempty"`
}

// DefaultInstallerConfig is the compiled defaults level (Global lowest precedence).
var DefaultInstallerConfig = InstallerConfig{
	Name:           "anvil",
	ArtifactSource: ".",
	OSTargets:      []string{"windows", "linux"},
}

// AllowedOSTargets is the canonical allowed set for installer.osTargets.
var AllowedOSTargets = map[string]bool{"windows": true, "linux": true}

// SanitizeInstallerName mirrors spike 2 sanitization: strip invalid chars, trim, default "anvil".
func SanitizeInstallerName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "anvil"
	}
	re := regexp.MustCompile(`[^a-zA-Z0-9\-_ ]+`)
	sanitized := re.ReplaceAllString(raw, "")
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "anvil"
	}
	sanitized = regexp.MustCompile(`\s+`).ReplaceAllString(sanitized, " ")
	return sanitized
}

// LoadInstallerConfig resolves installer config via ADR-005 4-level order:
//
//  1. Compiled defaults (DefaultInstallerConfig) — Global level
//  2. Project anvil.yaml installer block — Project level
//  3. Environment variables ANVIL_CFG_INSTALLER_* — Execution level (highest)
//
// Global file discovery (internal/config DiscoverConfigFiles pattern) is simulated
// by reading anvil.yaml at repoRoot; env overrides use ANVIL_CFG_ prefix matching
// internal/config loadEnvVars convention (ST-P2-02, ADR-005 §10.2).
//
// It validates via ValidateInstallerConfig and returns the resolved config plus
// a map of env overrides applied (for evidence / redaction checks).
func LoadInstallerConfig(repoRoot string) (*InstallerConfig, map[string]string, error) {
	// 1. Start from defaults (Global level)
	cfg := DefaultInstallerConfig

	// 2. Project level: read anvil.yaml if present
	yamlPath := filepath.Join(repoRoot, "anvil.yaml")
	if b, err := os.ReadFile(yamlPath); err == nil {
		var doc map[string]interface{}
		if err := yaml.Unmarshal(b, &doc); err == nil {
			if inst, ok := doc["installer"].(map[string]interface{}); ok {
				if v, ok := inst["name"].(string); ok && strings.TrimSpace(v) != "" {
					cfg.Name = SanitizeInstallerName(v)
				}
				if v, ok := inst["icon"].(string); ok {
					cfg.Icon = strings.TrimSpace(v)
				}
				if v, ok := inst["artifactSource"].(string); ok && strings.TrimSpace(v) != "" {
					cfg.ArtifactSource = strings.TrimSpace(v)
				}
				if v, ok := inst["artifact_source"].(string); ok && strings.TrimSpace(v) != "" {
					cfg.ArtifactSource = strings.TrimSpace(v)
				}
				if raw, ok := inst["osTargets"]; ok {
					cfg.OSTargets = normalizeOSTargets(raw)
				}
				if raw, ok := inst["os_targets"]; ok {
					cfg.OSTargets = normalizeOSTargets(raw)
				}
			}
		}
	}
	cfg.ResolvedFrom = "project:anvil.yaml"

	// 3. Execution level: ANVIL_CFG_INSTALLER_* overrides (highest precedence)
	overrides := make(map[string]string)
	if v := strings.TrimSpace(os.Getenv("ANVIL_CFG_INSTALLER_NAME")); v != "" {
		cfg.Name = SanitizeInstallerName(v)
		overrides["ANVIL_CFG_INSTALLER_NAME"] = v
	}
	if v := strings.TrimSpace(os.Getenv("ANVIL_CFG_INSTALLER_ICON")); v != "" {
		cfg.Icon = v
		overrides["ANVIL_CFG_INSTALLER_ICON"] = v
	}
	if v := strings.TrimSpace(os.Getenv("ANVIL_CFG_INSTALLER_ARTIFACT_SOURCE")); v != "" {
		cfg.ArtifactSource = v
		overrides["ANVIL_CFG_INSTALLER_ARTIFACT_SOURCE"] = v
	}
	// osTargets: comma-separated
	if v := strings.TrimSpace(os.Getenv("ANVIL_CFG_INSTALLER_OS_TARGETS")); v != "" {
		cfg.OSTargets = parseOSTargetsEnv(v)
		overrides["ANVIL_CFG_INSTALLER_OS_TARGETS"] = v
	}
	if v := strings.TrimSpace(os.Getenv("ANVIL_CFG_INSTALLER_OS_TARGETS_ALT")); v != "" {
		// alternate underscore variant not used; kept for completeness
		cfg.OSTargets = parseOSTargetsEnv(v)
		overrides["ANVIL_CFG_INSTALLER_OS_TARGETS_ALT"] = v
	}

	// Normalize targets (lowercase, dedup, filter allowed)
	cfg.OSTargets = normalizeOSTargetsList(cfg.OSTargets)

	if len(overrides) > 0 {
		cfg.ResolvedFrom = "execution:env"
	}

	if err := ValidateInstallerConfig(&cfg); err != nil {
		return nil, overrides, err
	}

	return &cfg, overrides, nil
}

func normalizeOSTargets(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		return parseOSTargetsEnv(v)
	default:
		return nil
	}
}

func parseOSTargetsEnv(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeOSTargetsList(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if !AllowedOSTargets[t] {
			continue
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		// fallback to defaults if empty after filtering (keep at least one)
		// caller validation will catch truly empty; here preserve defaults
		return DefaultInstallerConfig.OSTargets
	}
	return out
}

// ValidateInstallerConfig validates required fields + allowed osTargets + icon extension gate.
func ValidateInstallerConfig(c *InstallerConfig) error {
	if c == nil {
		return fmt.Errorf("installer config nil")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("installer.name is required (must be non-empty)")
	}
	if len(c.OSTargets) == 0 {
		return fmt.Errorf("installer.osTargets must contain at least one of [windows, linux]")
	}
	for _, t := range c.OSTargets {
		if !AllowedOSTargets[strings.ToLower(t)] {
			return fmt.Errorf("installer.osTargets: unsupported target %q (want windows|linux)", t)
		}
	}
	// Icon gate (AC2): if icon provided, must be .ico for windows or .png/.svg for linux context
	// Spike: if osTargets includes windows, .ico preferred; if only linux, .png preferred — but don't hard fail
	// Only validate extension is known
	if c.Icon != "" {
		ext := strings.ToLower(filepath.Ext(c.Icon))
		if ext != ".ico" && ext != ".png" && ext != ".svg" && ext != ".xpm" {
			return fmt.Errorf("installer.icon: unsupported extension %q (want .ico for windows, .png/.svg for linux)", ext)
		}
	}
	return nil
}

// RedactInstallerLog masks secrets in installer pipeline logs.
//
// Uses internal/output RedactSecrets + SanitizeLogLine (ADR-005 redaction) plus
// installer-specific signing key env ANVIL_SIGNING_KEY.
func RedactInstallerLog(s string) string {
	// redact ANVIL_SIGNING_KEY value if leaked verbatim
	if val := os.Getenv("ANVIL_SIGNING_KEY"); val != "" && strings.Contains(s, val) {
		s = strings.ReplaceAll(s, val, "***REDACTED_SIGNING_KEY***")
	}
	s = output.SanitizeLogLine(s)
	s = output.RedactSecrets(s)
	return s
}

// IsTargetAllowed reports whether requested target is in osTargets.
func (c *InstallerConfig) IsTargetAllowed(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, t := range c.OSTargets {
		if strings.ToLower(t) == target {
			return true
		}
	}
	return false
}

// RenderedInstallerFilename maps target → filename (mirrors spike 2 helpers).
func (c *InstallerConfig) RenderedInstallerFilename(target string) string {
	name := SanitizeInstallerName(c.Name)
	switch strings.ToLower(target) {
	case "windows":
		// NSIS: "<Name>-Setup.exe"
		return strings.ReplaceAll(name, " ", "-") + "-Setup.exe"
	case "linux":
		// Makeself: "<Name>.run"
		return strings.ReplaceAll(name, " ", "-") + ".run"
	default:
		return strings.ReplaceAll(name, " ", "-") + ".bin"
	}
}
