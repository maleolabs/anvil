// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P11-03, TS-011-003, EPIC-011, ADR-019
package deployment

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// SSH credential environment variable names (EPIC-011 §7.3, ADR-019).
//
// Credentials are injected by CI/CD systems at runtime and are never
// stored in Anvil configuration files (ADR-019, ST-P11-01 §4).
const (
	// EnvDeployServerHost is the server hostname or IP address.
	EnvDeployServerHost = "DEPLOY_SERVER_HOST"
	// EnvDeployServerUser is the SSH login username.
	EnvDeployServerUser = "DEPLOY_SERVER_USER"
	// EnvDeployServerPort is the SSH port; optional, defaults to 22.
	EnvDeployServerPort = "DEPLOY_SERVER_PORT"
	// EnvDeploySSHKey is the path to the SSH private key file.
	EnvDeploySSHKey = "DEPLOY_SSH_KEY"
	// EnvDeploySSHKnownHosts is the path to an OpenSSH known_hosts file
	// used to verify the server's host key (TD-004). Optional: when
	// unset, host key verification stays disabled (legacy behavior).
	EnvDeploySSHKnownHosts = "DEPLOY_SSH_KNOWN_HOSTS"
	// EnvDeploySSHKnownHostsMode is the host key verification mode
	// (TD-004). Optional: strict when unset.
	EnvDeploySSHKnownHostsMode = "DEPLOY_SSH_KNOWN_HOSTS_MODE"
)

// KnownHostsMode selects how the SSH transport verifies the server's
// host key against the known-hosts file (TD-004). The names mirror
// OpenSSH's StrictHostKeyChecking values.
//
// Reference: TD-004, EPIC-011 §7.3
type KnownHostsMode string

// Host key verification modes (TD-004).
const (
	// KnownHostsModeStrict rejects unknown and changed host keys: the
	// connection fails closed when the server identity cannot be
	// verified against the known-hosts file. Default mode.
	KnownHostsModeStrict KnownHostsMode = "strict"
	// KnownHostsModeAcceptNew records an unknown host key in the
	// known-hosts file and accepts the connection (TOFU for first
	// contact); a changed host key is still rejected because it can
	// signal a man-in-the-middle attack.
	KnownHostsModeAcceptNew KnownHostsMode = "accept-new"
)

// ParseKnownHostsMode validates a raw DEPLOY_SSH_KNOWN_HOSTS_MODE value
// and returns the corresponding mode (TD-004).
func ParseKnownHostsMode(raw string) (KnownHostsMode, error) {
	switch KnownHostsMode(raw) {
	case KnownHostsModeStrict:
		return KnownHostsModeStrict, nil
	case KnownHostsModeAcceptNew:
		return KnownHostsModeAcceptNew, nil
	default:
		return "", fmt.Errorf(
			"invalid host key verification mode %q, supported modes are %q and %q",
			raw, KnownHostsModeStrict, KnownHostsModeAcceptNew,
		)
	}
}

// DefaultSSHPort is the default SSH port when DEPLOY_SERVER_PORT is not
// set (EPIC-011 §7.3).
const DefaultSSHPort = 22

// SSHCredentials holds the SSH connection parameters read from the
// environment (EPIC-011 §7.3). It is the input for constructing an
// SSHTransport.
//
// Reference: TS-P11-03, TS-011-003 AC-1, EPIC-011 §7.3
type SSHCredentials struct {
	// Host is the server hostname or IP address (DEPLOY_SERVER_HOST).
	Host string

	// Port is the SSH port (DEPLOY_SERVER_PORT); 22 when unset.
	Port int

	// User is the SSH login username (DEPLOY_SERVER_USER).
	User string

	// KeyPath is the path to the SSH private key file (DEPLOY_SSH_KEY).
	KeyPath string

	// KnownHostsPath is the path to an OpenSSH known_hosts file
	// (DEPLOY_SSH_KNOWN_HOSTS); empty disables host key verification,
	// which keeps the legacy behavior so existing CI flows keep
	// working (TD-004).
	KnownHostsPath string

	// KnownHostsMode is the host key verification mode
	// (DEPLOY_SSH_KNOWN_HOSTS_MODE); strict when unset. Only consulted
	// when KnownHostsPath is set (TD-004).
	KnownHostsMode KnownHostsMode
}

// ResolveSSHCredentialsForTarget resolves SSH credentials for a named
// deployment target from the single-source config (anvil.yaml
// server.targets[env], ADR-005) with DEPLOY_SSH_KEY override (redacted,
// AC3) and ssh-agent preferred when no key path is supplied.
//
// Host/user come from server.targets[env] (required, AC1). Port and
// known-hosts come from the same target, with env vars as optional
// overrides for CI (DEPLOY_SSH_KNOWN_HOSTS, DEPLOY_SSH_KNOWN_HOSTS_MODE).
// DEPLOY_SSH_KEY, when set, overrides the target's sshKeyPath (redacted
// in logs via output.RedactSecrets). When both are empty the credentials
// carry an empty KeyPath to signal ssh-agent use (AC3 preferred).
//
// Host-key verification is mandatory and bypass is rejected in prod
// (env == prod/production → KnownHostsPath required, strict mode only, AC2).
//
// Reference: sto:local-deploy-config AC1-AC3, ADR-005, ADR local-deploy-transport
func ResolveSSHCredentialsForTarget(env string) (SSHCredentials, error) {
	if env == "" {
		return SSHCredentials{}, fmt.Errorf("target env is required")
	}

	flat, _, err := config.ResolveAndValidateConfig()
	if err != nil {
		return SSHCredentials{}, err
	}
	target, err := config.GetServerTarget(flat, env)
	if err != nil {
		return SSHCredentials{}, err
	}

	creds := SSHCredentials{
		Host: target.Host,
		User: target.User,
		Port: target.Port,
		// DEPLOY_SSH_KEY override (redacted) — ssh-agent preferred when empty (AC3)
		KeyPath: config.EffectiveSSHKeyPath(target.SSHKeyPath),
		KnownHostsPath: target.KnownHostsPath,
		KnownHostsMode: KnownHostsMode(target.KnownHostsMode),
	}
	if creds.Port == 0 {
		creds.Port = DefaultSSHPort
	}
	if creds.KnownHostsMode == "" {
		creds.KnownHostsMode = KnownHostsModeStrict
	}

	// CI overrides for host-key verification (optional, not per-subsystem config).
	if v := os.Getenv(EnvDeploySSHKnownHosts); v != "" {
		creds.KnownHostsPath = v
	}
	if rawMode, ok := os.LookupEnv(EnvDeploySSHKnownHostsMode); ok && rawMode != "" {
		mode, err := ParseKnownHostsMode(rawMode)
		if err != nil {
			return SSHCredentials{}, fmt.Errorf("%s: %v", EnvDeploySSHKnownHostsMode, err)
		}
		creds.KnownHostsMode = mode
	}
	if creds.KnownHostsPath == "" && os.Getenv(EnvDeploySSHKnownHostsMode) != "" {
		return SSHCredentials{}, fmt.Errorf(
			"%s is set but %s is not: host key verification requires a known-hosts file",
			EnvDeploySSHKnownHostsMode, EnvDeploySSHKnownHosts,
		)
	}

	if creds.Host == "" || creds.User == "" {
		return SSHCredentials{}, fmt.Errorf("server.targets.%s missing host or user in anvil.yaml", env)
	}

	// AC2: host-key verification wajib, bypass rejected di prod
	if isProdEnv(env) && strings.TrimSpace(creds.KnownHostsPath) == "" {
		return SSHCredentials{}, fmt.Errorf("host-key verification wajib di prod: set server.targets.%s.knownHostsPath or %s", env, EnvDeploySSHKnownHosts)
	}
	if isProdEnv(env) && creds.KnownHostsMode == KnownHostsModeAcceptNew {
		return SSHCredentials{}, fmt.Errorf("host-key verification bypass rejected di prod: server.targets.%s.knownHostsMode must be strict, got %q", env, creds.KnownHostsMode)
	}

	// KeyPath may be empty → ssh-agent preferred (AC3). Not an error here; transport will try agent.
	return creds, nil
}

func isProdEnv(env string) bool {
	l := strings.ToLower(strings.TrimSpace(env))
	return l == "prod" || l == "production" || l == "live"
}

// ReadSSHCredentialsFromEnv reads SSH credentials from the environment.
//
// Required variables: DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER,
// DEPLOY_SSH_KEY. Optional variables: DEPLOY_SERVER_PORT (default: 22),
// DEPLOY_SSH_KNOWN_HOSTS and DEPLOY_SSH_KNOWN_HOSTS_MODE (host key
// verification, TD-004).
//
// A variable is considered missing when it is unset or set-but-empty:
// an empty credential is never usable, and treating it as missing keeps
// the reported error truthful for CI/CD misconfigurations.
//
// When required variables are missing, the returned error names every
// missing variable so operators know exactly which environment
// variables to set (ST-P11-01 §3). A non-numeric or out-of-range
// DEPLOY_SERVER_PORT produces an explicit error. An invalid
// DEPLOY_SSH_KNOWN_HOSTS_MODE, or a mode set without a known-hosts
// file, also produces an explicit error (TD-004).
//
// Reference: TS-P11-03, TS-011-003 AC-1..AC-4, EPIC-011 §7.3, ADR-019, TD-004
func ReadSSHCredentialsFromEnv() (SSHCredentials, error) {
	var creds SSHCredentials

	// os.LookupEnv distinguishes unset from set-but-empty; both are
	// reported as missing because an empty credential is unusable.
	missing := make([]string, 0, 3)
	for _, variable := range []struct {
		name string
		dst  *string
	}{
		{EnvDeployServerHost, &creds.Host},
		{EnvDeployServerUser, &creds.User},
		{EnvDeploySSHKey, &creds.KeyPath},
	} {
		value, ok := os.LookupEnv(variable.name)
		if !ok || value == "" {
			missing = append(missing, variable.name)
			continue
		}
		*variable.dst = value
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return SSHCredentials{}, fmt.Errorf(
			"missing SSH credential environment variables: %s",
			joinEnvVars(missing),
		)
	}

	creds.Port = DefaultSSHPort
	if rawPort, ok := os.LookupEnv(EnvDeployServerPort); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return SSHCredentials{}, fmt.Errorf(
				"%s must be a valid TCP port number between 1 and 65535, got %q",
				EnvDeployServerPort, rawPort,
			)
		}
		creds.Port = port
	}

	// Optional: host key verification (TD-004). Verification is enabled
	// only when DEPLOY_SSH_KNOWN_HOSTS is set — unset keeps the legacy
	// behavior (no verification) so existing CI flows keep working. The
	// mode defaults to strict (fail closed) and only matters when the
	// known-hosts file is configured.
	if rawPath, ok := os.LookupEnv(EnvDeploySSHKnownHosts); ok && rawPath != "" {
		creds.KnownHostsPath = rawPath
	}
	creds.KnownHostsMode = KnownHostsModeStrict
	rawMode, modeSet := os.LookupEnv(EnvDeploySSHKnownHostsMode)
	if modeSet && rawMode != "" {
		mode, err := ParseKnownHostsMode(rawMode)
		if err != nil {
			return SSHCredentials{}, fmt.Errorf("%s: %v", EnvDeploySSHKnownHostsMode, err)
		}
		creds.KnownHostsMode = mode
	}
	if creds.KnownHostsPath == "" && modeSet {
		// An explicitly configured mode without a known-hosts file
		// would silently mean "no verification"; reject it so a
		// misconfigured verification intent never goes unnoticed
		// (fail closed on configuration errors).
		return SSHCredentials{}, fmt.Errorf(
			"%s is set but %s is not: host key verification requires a known-hosts file",
			EnvDeploySSHKnownHostsMode, EnvDeploySSHKnownHosts,
		)
	}

	return creds, nil
}

// joinEnvVars renders a sorted list of variable names for error
// messages, e.g. `DEPLOY_SERVER_HOST, DEPLOY_SERVER_USER and
// DEPLOY_SSH_KEY`.
func joinEnvVars(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}
