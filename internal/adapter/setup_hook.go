package adapter

import (
	"fmt"
	"os"
	"strings"

	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/installer"
)

// LaravelStandardHook bridges installer SetupConfig into Laravel adapter setup stage.
// If laravel-adapter setup stage is missing, this provides the stage that creates
// superAdmin via artisan tinker or seeder with templated identifier.
type LaravelStandardHook struct {
	InstallRoot string
	Config      map[string]interface{}
	FormsInput  map[string]map[string]string
}

// Resolve returns the resolved SetupConfig using installer hook logic.
func (h *LaravelStandardHook) Resolve() (installer.SetupConfig, error) {
	if h.Config == nil {
		h.Config = map[string]interface{}{}
	}
	if h.FormsInput == nil {
		h.FormsInput = map[string]map[string]string{}
	}
	return installer.ResolveSetupConfig(h.Config, h.FormsInput)
}

// SetupCommands returns the artisan commands for the setup stage.
// This patches the adapter if the stage was missing.
func (h *LaravelStandardHook) SetupCommands() ([]string, error) {
	cfg, err := h.Resolve()
	if err != nil {
		return nil, err
	}
	return installer.EnsureLaravelSetupStage(cfg, h.InstallRoot), nil
}

// Validate ensures identifier is valid after template resolution.
func (h *LaravelStandardHook) Validate() error {
	cfg, err := h.Resolve()
	if err != nil {
		return err
	}
	return installer.ValidateIdentifier(cfg.Identifier)
}

// RedactedLog returns a redacted log line that never exposes password fields.
// It uses RedactInstallerLogWithForms with dynamic field names from formsInput + config.
func (h *LaravelStandardHook) RedactedLog(line string) string {
	cfg, err := h.Resolve()
	if err != nil {
		// fallback to generic redaction
		return installer.RedactInstallerLog(line)
	}
	return installer.RedactSetupLog(line, cfg)
}

// ParseFormsInputFromEnv reads --forms-json temp file path from env or direct path and parses.
// If path is empty, it falls back to INSTALLER_FORMS_JSON env var.
func ParseFormsInputFromEnv(path string) (map[string]map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		path = os.Getenv(installer.FormsEnvVar)
	}
	// If path still looks like env var name, resolve it
	if path == installer.FormsEnvVar {
		if v := os.Getenv(path); v != "" {
			path = v
		}
	}
	return installer.ParseFormsInput(path)
}

// EnsureLaravelSetupHookExists is the patch entry-point: if adapter has no setup stage,
// this function supplies the standard hook commands that use templated superAdmin.
// It is intentionally explicit and testable.
//
// Example usage in laravel adapter:
//
//	hook := &adapter.LaravelStandardHook{InstallRoot: root, Config: flatConfig, FormsInput: forms}
//	cmds, err := hook.SetupCommands()
//	// execute cmds via artisan
func EnsureLaravelSetupHookExists(flatConfig map[string]interface{}, forms map[string]map[string]string, installRoot string) []string {
	h := &LaravelStandardHook{InstallRoot: installRoot, Config: flatConfig, FormsInput: forms}
	cmds, err := h.SetupCommands()
	if err != nil {
		// fallback to hardcode if validation fails
		fallback := "admin@example.com"
		if v, ok := flatConfig["setup.superAdmin.value"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				fallback = s
			}
		}
		cfg := installer.SetupConfig{Identifier: fallback}
		return installer.EnsureLaravelSetupStage(cfg, installRoot)
	}
	return cmds
}

// ResolveSetupEmailForAdapter is a thin wrapper around installer.ResolveSetupEmail for adapter use.
func ResolveSetupEmailForAdapter(templateValue string, formValues map[string]map[string]string, fallback string) string {
	return installer.ResolveSetupEmail(templateValue, formValues, fallback)
}

// ResolveExtraCommandsForAdapter resolves extraCommands templates for adapter.
func ResolveExtraCommandsForAdapter(cmds []string, forms map[string]map[string]string) []string {
	return installer.ResolveExtraCommands(cmds, forms)
}

// Ensure template helpers are available via config package
var _ = config.ResolveFormsTemplate
var _ = fmt.Sprintf
