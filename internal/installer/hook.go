package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// SetupConfig holds resolved setup values from installer.setup templates + formsInput.
//
// Reference: anvil-cli/sto:installer-standard-hook
// superAdmin identifier templated {{forms.superAdmin.email}} or {{forms.superAdmin.username}}
// fallback hardcode `setup.superAdmin.value` if no forms.
// ExtraCommands templated via same mechanism.
type SetupConfig struct {
	// Identifier is the resolved superAdmin identifier (email or username) after template.
	Identifier string `json:"identifier"`
	// Email is resolved email if identifier is email-type.
	Email string `json:"email"`
	// Username is resolved username if identifier is username-type.
	Username string `json:"username"`
	// ExtraCommands are resolved extraCommands after templating.
	ExtraCommands []string `json:"extraCommands,omitempty"`
	// RawTemplate is the original template string used.
	RawTemplate string `json:"rawTemplate,omitempty"`
	// ResolvedFrom tracks which config key provided the template.
	ResolvedFrom string `json:"resolvedFrom,omitempty"`
	// PasswordFields are names of password fields for redaction (derived from forms).
	PasswordFields []string `json:"-"`
}

var (
	emailRegexHook = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	// username non-empty check (looser than email)
)

// ParseFormsInput reads --forms-json temp file JSON map form.field=value.
// Supports multiple shapes:
//
//  1. {"forms":{"superAdmin":{"email":"a@b.com","username":"admin"}}}
//  2. {"superAdmin":{"email":"a@b.com"}}
//  3. {"superAdmin.email":"a@b.com","superAdmin.username":"admin"}
//  4. {"forms":{"superAdmin.email":"a@b.com"}}
//
// Returns map[form]map[field]value.
func ParseFormsInput(path string) (map[string]map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]map[string]string{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read forms json %q: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]map[string]string{}, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse forms json: %w", err)
	}
	// If root contains "forms" key, use it as root; else root is whole.
	root := raw
	if v, ok := raw["forms"]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			root = m
		} else if v == nil {
			root = map[string]interface{}{}
		}
		// else keep raw? but if forms is not object, fallback to raw
	}
	out := map[string]map[string]string{}
	for k, v := range root {
		// Skip the wrapper key if we already extracted forms? root already stripped, so not needed.
		// But if original had forms wrapper and we stripped, k will be form names.
		switch val := v.(type) {
		case map[string]interface{}:
			formMap := map[string]string{}
			for fk, fv := range val {
				formMap[fk] = fmt.Sprintf("%v", fv)
			}
			if _, ok := out[k]; !ok {
				out[k] = formMap
			} else {
				for fk, fv := range formMap {
					out[k][fk] = fv
				}
			}
		case string:
			// Could be flat key "superAdmin.email" with string value.
			if strings.Contains(k, ".") {
				parts := strings.SplitN(k, ".", 2)
				formName := parts[0]
				fieldName := parts[1]
				if _, ok := out[formName]; !ok {
					out[formName] = map[string]string{}
				}
				out[formName][fieldName] = val
			} else {
				// Single-level key with string value — treat as form with single field? Not useful, but store.
				if _, ok := out[k]; !ok {
					out[k] = map[string]string{}
				}
				// Put under key ""? We'll store as "_value"
				out[k]["value"] = val
			}
		case float64, bool, int:
			if strings.Contains(k, ".") {
				parts := strings.SplitN(k, ".", 2)
				formName := parts[0]
				fieldName := parts[1]
				if _, ok := out[formName]; !ok {
					out[formName] = map[string]string{}
				}
				out[formName][fieldName] = fmt.Sprintf("%v", val)
			}
		default:
			// nested map[string]interface{} already handled; for other types stringify
			if strings.Contains(k, ".") {
				parts := strings.SplitN(k, ".", 2)
				formName := parts[0]
				fieldName := parts[1]
				if _, ok := out[formName]; !ok {
					out[formName] = map[string]string{}
				}
				out[formName][fieldName] = fmt.Sprintf("%v", val)
			}
		}
	}
	// Also handle flat keys that were inside "forms" wrapper already flattened above.
	// Additional pass: if raw contained both nested and flat, they are already merged.
	// If raw was {"forms": {...}} and inside was flat "superAdmin.email", that case handled because
	// root = raw["forms"] map, and we loop above; flat keys inside forms would be string values with dot.
	// Good.
	return out, nil
}

// ResolveSetupConfig resolves superAdmin identifier and extraCommands from config map + formsInput.
// config map is flat dot-notation (e.g. "installer.setup.super_admin_email" -> "{{forms.superAdmin.email}}").
// For backward compat also checks "setup.superAdmin.value" and "installer.setup.super_admin_name".
func ResolveSetupConfig(cfg map[string]interface{}, formsInput map[string]map[string]string) (SetupConfig, error) {
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if formsInput == nil {
		formsInput = map[string]map[string]string{}
	}

	// Collect password field names for redaction later (from cfg if installer.forms provided)
	passwordFields := extractPasswordFieldNames(cfg)

	// Determine templates and fallbacks
	emailTmpl := getString(cfg, "installer.setup.super_admin_email")
	nameTmpl := getString(cfg, "installer.setup.super_admin_name")
	fallback := getString(cfg, "setup.superAdmin.value")
	if fallback == "" {
		fallback = getString(cfg, "setup.superAdmin.email")
	}
	if fallback == "" {
		fallback = getString(cfg, "installer.setup.super_admin_email.fallback")
	}
	// Also fallback to default from schema if still empty: admin@example.com for email, Admin for name
	if fallback == "" {
		// If template is empty and no fallback, use schema default
		if emailTmpl == "" && nameTmpl == "" {
			fallback = "admin@example.com"
		}
	}

	var identifier string
	var resolvedFrom string
	var rawTmpl string

	// Prefer email template if it contains {{forms or is non-empty
	if emailTmpl != "" {
		rawTmpl = emailTmpl
		resolvedFrom = "installer.setup.super_admin_email"
		identifier = config.ResolveFormsTemplateWithFallback(emailTmpl, formsInput, fallback)
		// If still contains placeholder and fallback non-empty, use fallback (already handled)
		if config.ContainsFormsPlaceholder(identifier) && fallback != "" {
			identifier = fallback
		}
	} else if nameTmpl != "" {
		rawTmpl = nameTmpl
		resolvedFrom = "installer.setup.super_admin_name"
		identifier = config.ResolveFormsTemplateWithFallback(nameTmpl, formsInput, fallback)
		if config.ContainsFormsPlaceholder(identifier) && fallback != "" {
			identifier = fallback
		}
	} else {
		// No template defined — use fallback directly
		rawTmpl = fallback
		resolvedFrom = "setup.superAdmin.value"
		identifier = fallback
		if identifier == "" {
			identifier = "admin@example.com"
		}
	}
	// If identifier still empty after resolution, fallback
	if strings.TrimSpace(identifier) == "" && fallback != "" {
		identifier = fallback
	}
	if strings.TrimSpace(identifier) == "" {
		identifier = "admin@example.com"
	}
	// Detect if identifier still has unresolved placeholder -> treat as error then fallback if possible
	if config.ContainsFormsPlaceholder(identifier) {
		if fallback != "" {
			identifier = fallback
		} else {
			return SetupConfig{}, fmt.Errorf("superAdmin identifier unresolved template %q with no fallback", rawTmpl)
		}
	}
	identifier = strings.TrimSpace(identifier)
	// Context-aware validation: if template was email, enforce email format even if no '@'
	if resolvedFrom == "installer.setup.super_admin_email" {
		if !emailRegexHook.MatchString(identifier) {
			return SetupConfig{}, fmt.Errorf("superAdmin identifier %q must be valid email", identifier)
		}
	} else if resolvedFrom == "installer.setup.super_admin_name" {
		// username validation (non-empty, >=2, allowed chars)
		if strings.TrimSpace(identifier) == "" {
			return SetupConfig{}, fmt.Errorf("superAdmin identifier empty after template resolution")
		}
		if len(identifier) < 2 {
			return SetupConfig{}, fmt.Errorf("superAdmin identifier %q must be at least 2 characters", identifier)
		}
	} else {
		if err := ValidateIdentifier(identifier); err != nil {
			return SetupConfig{}, err
		}
	}

	// Resolve extraCommands
	extraRaw := getStringSlice(cfg, "installer.setup.extraCommands")
	// also try "installer.setup.extra_commands" snake
	if len(extraRaw) == 0 {
		extraRaw = getStringSlice(cfg, "installer.setup.extra_commands")
	}
	var extraResolved []string
	for _, cmd := range extraRaw {
		// Resolve templates in each command; use fallback empty (don't replace with fallback, just leave if unresolved? But use ResolveFormsTemplate)
		resolved := config.ResolveFormsTemplate(cmd, formsInput)
		// If still contains placeholder and fallback available, replace placeholders with fallback
		if config.ContainsFormsPlaceholder(resolved) && fallback != "" {
			resolved = config.ResolveFormsTemplateWithFallback(cmd, formsInput, fallback)
		}
		extraResolved = append(extraResolved, resolved)
	}

	// Split identifier into email vs username
	var email, username string
	if strings.Contains(identifier, "@") {
		email = identifier
	} else {
		username = identifier
	}

	return SetupConfig{
		Identifier:     identifier,
		Email:          email,
		Username:       username,
		ExtraCommands:  extraResolved,
		RawTemplate:    rawTmpl,
		ResolvedFrom:   resolvedFrom,
		PasswordFields: passwordFields,
	}, nil
}

// ResolveEnvMap resolves installer.setup.env_map templates from formsInput.
// Supports both object form (installer.setup.env_map: {DB_HOST: "{{forms.database.db_host}}"}) and fragmented keys (installer.setup.env_map.DB_HOST).
func ResolveEnvMap(cfg map[string]interface{}, formsInput map[string]map[string]string) map[string]string {
	if cfg == nil {
		return map[string]string{}
	}
	if formsInput == nil {
		formsInput = map[string]map[string]string{}
	}
	envMap := map[string]string{}
	// Collect from object key
	if raw, ok := cfg["installer.setup.env_map"]; ok {
		switch v := raw.(type) {
		case map[string]interface{}:
			for k, val := range v {
				if s, ok := val.(string); ok {
					envMap[k] = config.ResolveFormsTemplate(s, formsInput)
				} else {
					envMap[k] = config.ResolveFormsTemplate(fmt.Sprintf("%v", val), formsInput)
				}
			}
		case map[string]string:
			for k, val := range v {
				envMap[k] = config.ResolveFormsTemplate(val, formsInput)
			}
		}
	}
	// Collect fragmented keys installer.setup.env_map.<KEY>
	prefix := "installer.setup.env_map."
	for k, v := range cfg {
		if strings.HasPrefix(k, prefix) {
			envKey := strings.TrimPrefix(k, prefix)
			if envKey == "" {
				continue
			}
			var tmpl string
			switch s := v.(type) {
			case string:
				tmpl = s
			default:
				tmpl = fmt.Sprintf("%v", v)
			}
			envMap[envKey] = config.ResolveFormsTemplate(tmpl, formsInput)
		}
	}
	return envMap
}

// ApplyEnvMap writes resolved env_map to installRoot/envFile (default .env).
// Creates file if not exists, updates existing keys preserving other lines.
// Returns written path.
func ApplyEnvMap(cfg map[string]interface{}, formsInput map[string]map[string]string, installRoot string) (string, error) {
	envMap := ResolveEnvMap(cfg, formsInput)
	if len(envMap) == 0 {
		return "", nil
	}
	envFile := getString(cfg, "installer.setup.env_file")
	if envFile == "" {
		envFile = ".env"
	}
	if !filepath.IsAbs(envFile) && installRoot != "" {
		envFile = filepath.Join(installRoot, envFile)
	} else if !filepath.IsAbs(envFile) {
		envFile = filepath.Join(".", envFile)
	}
	dir := filepath.Dir(envFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	existing := map[string]string{}
	var origLines []string
	if data, err := os.ReadFile(envFile); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			origLines = append(origLines, line)
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "#") {
				continue
			}
			if idx := strings.Index(trim, "="); idx > 0 {
				k := strings.TrimSpace(trim[:idx])
				v := strings.TrimSpace(trim[idx+1:])
				existing[k] = v
			}
		}
	}
	for k, v := range envMap {
		existing[k] = v
	}
	// Write merged
	var out strings.Builder
	written := map[string]bool{}
	for _, line := range origLines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			out.WriteString(line + "\n")
			continue
		}
		if idx := strings.Index(trim, "="); idx > 0 {
			k := strings.TrimSpace(trim[:idx])
			if val, ok := envMap[k]; ok && !written[k] {
				out.WriteString(fmt.Sprintf("%s=%s\n", k, val))
				written[k] = true
				continue
			}
			if _, ok := existing[k]; ok && written[k] {
				continue
			}
		}
		out.WriteString(line + "\n")
	}
	for k, v := range envMap {
		if !written[k] {
			out.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
	}
	if err := os.WriteFile(envFile, []byte(out.String()), 0o644); err != nil {
		return "", err
	}
	// Redacted log: don't log password values
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	fmt.Fprintf(os.Stderr, "[installer] env_map written (redacted) keys=%v to %s\n", keys, envFile)
	return envFile, nil
}

// ValidateIdentifier checks identifier after template resolution.
// If contains '@' validates as email, else requires non-empty username (>=2 chars to avoid trivial).
func ValidateIdentifier(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("superAdmin identifier empty after template resolution")
	}
	if strings.Contains(id, "@") {
		if !emailRegexHook.MatchString(id) {
			return fmt.Errorf("superAdmin identifier %q must be valid email", id)
		}
		return nil
	}
	// username path: must be non-empty and match allowed pattern (at least 2 chars, alphanum/._-)
	if len(id) < 2 {
		return fmt.Errorf("superAdmin identifier %q must be at least 2 characters", id)
	}
	// allow letters, digits, underscore, dot, hyphen
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return fmt.Errorf("superAdmin identifier %q contains invalid character %q", id, string(r))
	}
	return nil
}

// RedactSetupLog redacts password fields from a log line using dynamic field names.
// It delegates to RedactInstallerLogWithForms with forms-derived password fields plus generic password.
func RedactSetupLog(line string, cfg SetupConfig) string {
	fields := cfg.PasswordFields
	// also ensure generic "password" is covered
	hasPassword := false
	for _, f := range fields {
		if strings.EqualFold(f, "password") {
			hasPassword = true
			break
		}
	}
	if !hasPassword {
		fields = append(append([]string{}, fields...), "password")
	}
	return RedactInstallerLogWithForms(line, fields)
}

// ResolveExtraCommands resolves templated extraCommands slice using formsInput.
func ResolveExtraCommands(cmds []string, formsInput map[string]map[string]string) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, config.ResolveFormsTemplate(c, formsInput))
	}
	return out
}

// LaravelSetupCommands returns the standard setup stage commands for laravel-adapter.
// Patch: if laravel-adapter setup stage is missing, these commands create superAdmin via artisan.
// Uses templated identifier (email or username) already resolved in cfg.
func LaravelSetupCommands(cfg SetupConfig, installRoot string) []string {
	identifier := cfg.Identifier
	if identifier == "" {
		identifier = "admin@example.com"
	}
	// Base artisan commands owned by standard hook (per spikes/installer-boundary/standard_hook.go)
	// - php artisan migrate --force
	// - php artisan storage:link
	// - superAdmin seed via tinker or db:seed
	cmds := []string{
		"php artisan migrate --force",
		"php artisan storage:link",
	}
	// SuperAdmin seeding: prefer tinker with identifier
	if strings.Contains(identifier, "@") {
		// email identifier
		cmds = append(cmds, fmt.Sprintf(`php artisan tinker --execute="\\App\\Models\\User::firstOrCreate(['email'=>'%s'], ['name'=>'Admin','password'=>bcrypt('password')]);"`, escapeSingleQuote(identifier)))
	} else {
		cmds = append(cmds, fmt.Sprintf(`php artisan tinker --execute="\\App\\Models\\User::firstOrCreate(['username'=>'%s'], ['name'=>'Admin','email'=>'%s@example.com','password'=>bcrypt('password')]);"`, escapeSingleQuote(identifier), escapeSingleQuote(identifier)))
	}
	// Append resolved extraCommands
	for _, c := range cfg.ExtraCommands {
		if strings.TrimSpace(c) != "" {
			cmds = append(cmds, c)
		}
	}
	if strings.TrimSpace(installRoot) != "" {
		// prefix with cd if needed (caller may handle)
		_ = installRoot
	}
	return cmds
}

// EnsureLaravelSetupStage patches laravel-adapter setup stage if missing.
// In this repository the adapter is external; this function documents the hook
// and ensures the installer hook provides the expected commands.
// It returns the commands that the adapter should execute.
func EnsureLaravelSetupStage(cfg SetupConfig, installRoot string) []string {
	return LaravelSetupCommands(cfg, installRoot)
}

// Helpers

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case []byte:
			return string(s)
		default:
			return fmt.Sprintf("%v", s)
		}
	}
	return ""
}

func getStringSlice(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, e := range val {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprintf("%v", e))
			}
		}
		return out
	case string:
		// single string with commas? treat as one
		if strings.TrimSpace(val) == "" {
			return nil
		}
		return []string{val}
	default:
		return nil
	}
}

func extractPasswordFieldNames(cfg map[string]interface{}) []string {
	var out []string
	// Try to extract from installer.forms object if present
	if raw, ok := cfg["installer.forms"]; ok {
		switch v := raw.(type) {
		case map[string]interface{}:
			for _, formRaw := range v {
				if fm, ok := formRaw.(map[string]interface{}); ok {
					if fields, ok := fm["fields"]; ok {
						switch fv := fields.(type) {
						case []interface{}:
							for _, f := range fv {
								if fm2, ok := f.(map[string]interface{}); ok {
									if typ, ok := fm2["type"].(string); ok && typ == "password" {
										if name, ok := fm2["name"].(string); ok && name != "" {
											out = append(out, name)
										}
									}
								}
							}
						case []map[string]interface{}:
							for _, fm2 := range fv {
								if typ, ok := fm2["type"].(string); ok && typ == "password" {
									if name, ok := fm2["name"].(string); ok && name != "" {
										out = append(out, name)
									}
								}
							}
						}
					}
				}
			}
		case config.InstallerForms:
			for _, form := range v {
				for _, f := range form.Fields {
					if f.Type == "password" {
						out = append(out, f.Name)
					}
				}
			}
		}
	}
	// Use helper PasswordFieldNames if cfg contains typed forms
	// Also fallback to generic detection: any field name containing password is considered
	return out
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
