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
