// Package config provides the server targets configuration for Anvil's
// single-server deploy (ADR-005, scp:local-deploy-mvp, sto:local-deploy-config).
//
// Reference: anvil-cli/sto:local-deploy-config, ADR-005, ADR local-deploy-transport
package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ServerTarget holds the SSH connection parameters for a named environment.
// It is declared under `server.targets[env]` in anvil.yaml (ADR-005 single
// source) with an optional DEPLOY_SSH_KEY environment override for the key
// path (redacted via output.RedactSecrets, ssh-agent preferred when empty).
//
// Guard per-env (sto:local-deploy-guard): staging requires --confirm, prod
// is CI-only default plus allowlist + confirm prompt. Prod allowlist is
// declared via `server.targets[prod].allowlist` (array of SSH principals) or
// `allowLocal` bool; both map to the same enforcement.
//
// Reference: scp:local-deploy-mvp, sto:local-deploy-config AC1-AC3, sto:local-deploy-guard AC1
type ServerTarget struct {
	Host           string
	User           string
	Port           int
	SSHKeyPath     string
	KnownHostsPath string
	KnownHostsMode string // strict (default) or accept-new, enforced in prod
	Allowlist      []string
	AllowLocal     bool
}

// ServerConfig groups all declared server targets by environment name.
type ServerConfig struct {
	Targets map[string]ServerTarget `yaml:"targets"`
}

// host validation: RFC1123 hostname label + dot, or IP (v4/v6) via net.ParseIP.
// Labels: 1-63 chars, alphanum + hyphen, not start/end hyphen, overall <=253.
var hostnameLabelPattern = `[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?`
var hostnamePattern = regexp.MustCompile(`^` + hostnameLabelPattern + `(\.` + hostnameLabelPattern + `)*$`)

// user validation: POSIX-like user name (alphanum, dot, hyphen, underscore), start alphanum/underscore.
var userPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// isProdEnv reports whether env is a production environment where host-key
// verification bypass must be rejected (AC2: bypass rejected di prod).
// Canonical prod names: prod, production (case-insensitive, trimmed).
func isProdEnv(env string) bool {
	l := strings.ToLower(strings.TrimSpace(env))
	return l == "prod" || l == "production" || l == "live"
}

// ValidateHost reports whether host is a valid IP or hostname (AC1: host/user/ip validation).
func ValidateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if len(host) > 253 {
		return fmt.Errorf("host %q exceeds 253 characters", host)
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	// If it looks like an IPv4 dotted numeric but ParseIP failed, reject as invalid IP
	// rather than misclassify as hostname (e.g. 999.999.999.999).
	if isDottedNumeric(host) {
		return fmt.Errorf("host %q is not a valid IP address", host)
	}
	if strings.Contains(host, " ") || strings.Contains(host, "/") {
		return fmt.Errorf("host %q must be a valid IP or hostname without spaces or slashes", host)
	}
	if !hostnamePattern.MatchString(host) {
		return fmt.Errorf("host %q is not a valid IP or hostname (RFC1123)", host)
	}
	return nil
}

func isDottedNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return strings.Contains(s, ".")
}

// ValidateUser reports whether user is valid (AC1).
func ValidateUser(user string) error {
	if user == "" {
		return fmt.Errorf("user is required")
	}
	if len(user) > 32 {
		return fmt.Errorf("user %q exceeds 32 characters", user)
	}
	if !userPattern.MatchString(user) {
		return fmt.Errorf("user %q is not a valid SSH username", user)
	}
	return nil
}

// ValidateServerTarget validates a single target for environment env and
// returns validation errors classified for config validate grouping (AC1-AC2).
//
// Checks:
//   - host required and valid IP/hostname (format)
//   - user required and valid (required)
//   - port if set: 1-65535 (type/format)
//   - knownHostsMode if set: strict|accept-new (allowed)
//   - prod: host-key verification wajib (KnownHostsPath required, bypass rejected AC2)
func ValidateServerTarget(env string, t ServerTarget) []ValidationError {
	var errs []ValidationError
	base := fmt.Sprintf("server.targets.%s", env)

	if err := ValidateHost(t.Host); err != nil {
		// empty host is required, invalid format otherwise
		if t.Host == "" {
			errs = append(errs, ValidationError{
				Key:      base + ".host",
				Expected: "required string value",
				Actual:   nil,
			})
		} else {
			errs = append(errs, ValidationError{
				Key:      base + ".host",
				Expected: "valid IP or hostname (RFC1123)",
				Actual:   t.Host,
			})
		}
	}

	if err := ValidateUser(t.User); err != nil {
		if t.User == "" {
			errs = append(errs, ValidationError{
				Key:      base + ".user",
				Expected: "required string value",
				Actual:   nil,
			})
		} else {
			errs = append(errs, ValidationError{
				Key:      base + ".user",
				Expected: "valid SSH username",
				Actual:   t.User,
			})
		}
	}

	if t.Port != 0 && (t.Port < 1 || t.Port > 65535) {
		errs = append(errs, ValidationError{
			Key:      base + ".port",
			Expected: "integer between 1 and 65535",
			Actual:   t.Port,
		})
	}

	if t.KnownHostsMode != "" && t.KnownHostsMode != "strict" && t.KnownHostsMode != "accept-new" {
		errs = append(errs, ValidationError{
			Key:      base + ".knownHostsMode",
			Expected: "one of [strict, accept-new]",
			Actual:   t.KnownHostsMode,
		})
	}

	// AC2: host-key verification wajib, bypass rejected di prod
	if isProdEnv(env) {
		if strings.TrimSpace(t.KnownHostsPath) == "" {
			errs = append(errs, ValidationError{
				Key:      base + ".knownHostsPath",
				Expected: "required string value for prod (host-key verification wajib)",
				Actual:   nil,
			})
		}
		// In prod, accept-new is considered bypass (TOFU) and is rejected;
		// only strict is allowed for prod (fail-closed).
		if t.KnownHostsMode == "accept-new" {
			errs = append(errs, ValidationError{
				Key:      base + ".knownHostsMode",
				Expected: "strict for prod (accept-new bypass rejected di prod)",
				Actual:   t.KnownHostsMode,
			})
		}
	}

	// SSHKeyPath is optional: empty means ssh-agent preferred (AC3). No validation unless present
	// contains shell metachars? Keep permissive as path.

	return errs
}

// ExtractServerTargets parses server.targets from a flat resolved config map
// (as produced by resolveConfig/flattenYAML) and validates each target.
//
// Flat keys: server.targets.<env>.host, .user, .port, .sshKeyPath, .knownHostsPath, .knownHostsMode
//
// Unknown envs or malformed keys produce validation errors (format/type).
// No targets is valid (framework-free path AC4).
func ExtractServerTargets(flat map[string]interface{}) (map[string]ServerTarget, []ValidationError) {
	targets := make(map[string]ServerTarget)
	raw := make(map[string]map[string]interface{})

	for k, v := range flat {
		if !strings.HasPrefix(k, "server.targets.") {
			continue
		}
		suffix := strings.TrimPrefix(k, "server.targets.")
		parts := strings.SplitN(suffix, ".", 2)
		if len(parts) != 2 {
			// e.g. server.targets.staging without field — malformed
			env := suffix
			if env == "" {
				env = "(empty)"
			}
			// treat as format error on the env key itself
			// Use raw validation list for caller to report
			// We'll store placeholder to surface error later; but also emit now
			// We'll defer to validation loop; create a synthetic error now
			// To avoid duplicating, just ignore and let validation handle? Instead emit:
			// We create an entry with host missing which will be caught, but also add explicit error
			// Simpler: add error directly
			_ = raw // placeholder
			// Emit error via temporary slice stored in separate var handled below
			// We'll collect into a separate error list
			continue
		}
		env, field := parts[0], parts[1]
		if env == "" {
			continue
		}
		if _, ok := raw[env]; !ok {
			raw[env] = make(map[string]interface{})
		}
		raw[env][field] = v
	}

	// Also detect keys that were exactly server.targets.<env> with non-object value
	// (e.g. server.targets.staging = "bad"). Our loop skipped len==1, but we need to error them.
	// Scan original flat for those exact env keys with no dot after env
	for k, v := range flat {
		if !strings.HasPrefix(k, "server.targets.") {
			continue
		}
		suffix := strings.TrimPrefix(k, "server.targets.")
		if !strings.Contains(suffix, ".") {
			// It's a direct env value without field
			errKey := k
			// Record as invalid target (type error)
			_ = v
			// We'll synthesize validation error below via raw detection missing
			// Add to a separate pending errs list via closure capture
			raw[suffix] = map[string]interface{}{"__invalid_raw": v}
			// Mark for error by adding pseudo field which ValidateServerTarget will not directly catch,
			// so we handle via extra validation step below
			_ = errKey
		}
	}

	var allErrs []ValidationError
	for env, fields := range raw {
		if _, isInvalid := fields["__invalid_raw"]; isInvalid {
			allErrs = append(allErrs, ValidationError{
				Key:      fmt.Sprintf("server.targets.%s", env),
				Expected: "object with host, user",
				Actual:   fields["__invalid_raw"],
			})
			continue
		}
		var t ServerTarget
		// host
		if v, ok := fields["host"]; ok {
			if s, ok := v.(string); ok {
				t.Host = s
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.host", env),
					Expected: "string",
					Actual:   v,
				})
			}
		}
		// user
		if v, ok := fields["user"]; ok {
			if s, ok := v.(string); ok {
				t.User = s
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.user", env),
					Expected: "string",
					Actual:   v,
				})
			}
		}
		// port
		if v, ok := fields["port"]; ok {
			switch val := v.(type) {
			case int:
				t.Port = val
			case int64:
				t.Port = int(val)
			case float64:
				t.Port = int(val)
			case string:
				// string port like "2222" — strictly parse as integer (reject "2222abc")
				trimmed := strings.TrimSpace(val)
				p, err := strconv.Atoi(trimmed)
				if err == nil {
					t.Port = p
				} else {
					allErrs = append(allErrs, ValidationError{
						Key:      fmt.Sprintf("server.targets.%s.port", env),
						Expected: "integer between 1 and 65535",
						Actual:   v,
					})
				}
			default:
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.port", env),
					Expected: "integer between 1 and 65535",
					Actual:   v,
				})
			}
		}
		// sshKeyPath (allow sshKeyPath and ssh_key_path camel/snake)
		if v, ok := fields["sshKeyPath"]; ok {
			if s, ok := v.(string); ok {
				t.SSHKeyPath = s
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.sshKeyPath", env),
					Expected: "string",
					Actual:   v,
				})
			}
		} else if v, ok := fields["ssh_key_path"]; ok {
			if s, ok := v.(string); ok {
				t.SSHKeyPath = s
			}
		}
		// knownHostsPath
		if v, ok := fields["knownHostsPath"]; ok {
			if s, ok := v.(string); ok {
				t.KnownHostsPath = s
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.knownHostsPath", env),
					Expected: "string",
					Actual:   v,
				})
			}
		} else if v, ok := fields["known_hosts_path"]; ok {
			if s, ok := v.(string); ok {
				t.KnownHostsPath = s
			}
		}
		// knownHostsMode
		if v, ok := fields["knownHostsMode"]; ok {
			if s, ok := v.(string); ok {
				t.KnownHostsMode = s
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.knownHostsMode", env),
					Expected: "one of [strict, accept-new]",
					Actual:   v,
				})
			}
		} else if v, ok := fields["known_hosts_mode"]; ok {
			if s, ok := v.(string); ok {
				t.KnownHostsMode = s
			}
		}
		// guard allowlist — prod CI-only default + allowlist (sto:local-deploy-guard AC1)
		if v, ok := fields["allowlist"]; ok {
			t.Allowlist = parseAllowlistField(v, &allErrs, env, "allowlist")
		} else if v, ok := fields["allowList"]; ok {
			t.Allowlist = parseAllowlistField(v, &allErrs, env, "allowList")
		} else if v, ok := fields["allowLocalDeploys"]; ok {
			t.Allowlist = parseAllowlistField(v, &allErrs, env, "allowLocalDeploys")
		} else if v, ok := fields["allow_local_deploys"]; ok {
			t.Allowlist = parseAllowlistField(v, &allErrs, env, "allow_local_deploys")
		}
		if v, ok := fields["allowLocal"]; ok {
			if b, ok := coerceBool(v); ok {
				t.AllowLocal = b
			} else {
				allErrs = append(allErrs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.allowLocal", env),
					Expected: "boolean",
					Actual:   v,
				})
			}
		} else if v, ok := fields["allow_local"]; ok {
			if b, ok := coerceBool(v); ok {
				t.AllowLocal = b
			}
		} else if v, ok := fields["allowLocalDeploy"]; ok {
			if b, ok := coerceBool(v); ok {
				t.AllowLocal = b
			}
		}

		// Validate this target (collects required/format/allowed errors)
		errs := ValidateServerTarget(env, t)
		allErrs = append(allErrs, errs...)
		targets[env] = t
	}

	// Also handle case where flat contains no server.targets at all — that's valid (no targets)
	// But if anvil.yaml explicitly has server.targets as an object, flatten will have entries;
	// if empty, raw will be empty and we return empty map with no errs (framework-free path).

	return targets, allErrs
}

// ValidateServerTargetsInFlat is the entry point for config validate (AC4):
// validates server.targets present in resolved flat map and returns all errs.
// Framework-free path is covered: when no server section, no errs.
func ValidateServerTargetsInFlat(flat map[string]interface{}) []ValidationError {
	_, errs := ExtractServerTargets(flat)
	return errs
}

// GetServerTarget returns the ServerTarget for env from a resolved flat map.
// It validates existence (required) and returns the target.
func GetServerTarget(flat map[string]interface{}, env string) (ServerTarget, error) {
	targets, errs := ExtractServerTargets(flat)
	// If there were validation errors for this env, surface them as combined error
	for _, e := range errs {
		if strings.HasPrefix(e.Key, fmt.Sprintf("server.targets.%s.", env)) || e.Key == fmt.Sprintf("server.targets.%s", env) {
			return ServerTarget{}, fmt.Errorf("server.targets.%s validation failed: %s: expected %s, got %v", env, e.Key, e.Expected, e.Actual)
		}
	}
	t, ok := targets[env]
	if !ok {
		return ServerTarget{}, fmt.Errorf("server.targets.%s not found in anvil.yaml", env)
	}
	return t, nil
}

// parseAllowlistField normalizes allowlist array values (string slice, []interface{}).
func parseAllowlistField(v interface{}, errs *[]ValidationError, env, field string) []string {
	switch val := v.(type) {
	case []interface{}:
		var out []string
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			} else if s, ok := item.(string); ok && strings.TrimSpace(s) == "" {
				continue
			} else {
				*errs = append(*errs, ValidationError{
					Key:      fmt.Sprintf("server.targets.%s.%s", env, field),
					Expected: "array of strings (SSH principals)",
					Actual:   v,
				})
				return nil
			}
		}
		return out
	case []string:
		var out []string
		for _, s := range val {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		if strings.TrimSpace(val) == "" {
			return nil
		}
		parts := strings.Split(val, ",")
		var out []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		*errs = append(*errs, ValidationError{
			Key:      fmt.Sprintf("server.targets.%s.%s", env, field),
			Expected: "array of strings (SSH principals)",
			Actual:   v,
		})
		return nil
	}
}

func coerceBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		l := strings.ToLower(strings.TrimSpace(val))
		if l == "true" || l == "1" || l == "yes" {
			return true, true
		}
		if l == "false" || l == "0" || l == "no" {
			return false, true
		}
		return false, false
	case int:
		if val == 1 {
			return true, true
		}
		if val == 0 {
			return false, true
		}
		return false, false
	default:
		return false, false
	}
}

// EffectiveSSHKeyPath returns the effective SSH key path for a target,
// honoring DEPLOY_SSH_KEY env override (redacted) and ssh-agent preferred.
// If DEPLOY_SSH_KEY is set (non-empty), it overrides the config path.
// If both are empty, return "" to signal ssh-agent use (AC3).
func EffectiveSSHKeyPath(cfgPath string) string {
	if v := strings.TrimSpace(os.Getenv("DEPLOY_SSH_KEY")); v != "" {
		return v
	}
	return cfgPath
}
