# CI/CD Integration

Anvil is a **runtime orchestrator**, not a CI platform. It handles build, package, verify, and deploy operations — but does not replace your CI platform for triggering, scheduling, or managing pipelines.

## Responsibility boundary

| Concern | Who owns it |
|---|---|
| Triggering workflows (push, PR, schedule) | CI platform |
| Managing secrets and environment variables | CI platform |
| Caching dependencies between runs | CI platform |
| Build orchestration (compile, assets) | Anvil (`anvil pipeline build`) |
| Quality gates (lint, test) | Anvil (`anvil pipeline ci`) |
| Artifact packaging | Anvil (`anvil artifact package`) |
| Artifact integrity verification | Anvil (`anvil artifact verify`) |
| Upload to target server (SSH) | Anvil (`anvil deployment upload`) — the only SSH transport command |
| Install and activate release (on the server runtime) | Anvil (`anvil server release install/activate`, or the local target-centric aliases `anvil deployment install/activate`) |

> **Where do `deployment install/activate/rollback/info` run?** They are local
> target-centric aliases of the `server release` operations: they run **on the
> server runtime** (via the `ServerReleaseCoordinator`) and require a locally
> initialized server. They are NOT SSH transport commands — a fresh CI runner
> cannot run them. CI uploads the artifact (step 5 below); the release lifecycle
> is executed on the server.

## Workflow diagram

```text
  CI Platform                    Anvil                         Target Server
  ============                   =====                         =============

  push / PR event
       |
       v
  +-----------+     anvil pipeline ci      +------------------+
  | Trigger   | -------------------------> | Quality + Tests  |
  +-----------+                            +------------------+
       |
       v
  +-----------+     anvil pipeline build   +------------------+
  | Trigger   | -------------------------> | Build app        |
  +-----------+                            +------------------+
       |
       v
  +-----------+     anvil artifact package +------------------+
  | Trigger   | -------------------------> | Package artifact |
  +-----------+                            +------------------+
       |
       v
  +-----------+     anvil deployment upload +-----------------+
  | Trigger   | -------------------------> | Receive artifact |
  +-----------+          (SSH transport)   +-----------------+
                                                  |
                                                  v    anvil server release install
                                            +------------------+  (or local alias
                                            | Install release  |   anvil deployment install)
                                            +------------------+
                                                  |
                                                  v    anvil server release activate
                                            +------------------+  (or local alias
                                            | Activate release |   anvil deployment activate)
                                            +------------------+
       |
       v
  +-----------+
  | Report    |
  +-----------+
```

## Supported CI platforms

| Platform | Documentation |
|---|---|
| **GitHub Actions** | [github-actions.md](github-actions.md) |
| **GitLab CI** | [gitlab-ci.md](gitlab-ci.md) |
| **Jenkins** | [jenkins.md](jenkins.md) |

Anvil is CI-platform-agnostic. Any platform that can run shell commands can integrate with Anvil. The guides above cover the most common setups.

## Common Anvil commands in CI/CD

| Command | When to use |
|---|---|
| `anvil pipeline ci` | Run quality gates (lint, static analysis, tests) |
| `anvil pipeline build` | Build the application using the pipeline definition |
| `anvil artifact package` | Package the project into an immutable artifact |
| `anvil artifact verify <path>` | Verify artifact integrity before deployment |
| `anvil deployment upload <target-id> <artifact-path>` | Upload artifact to a remote server over SSH (the only SSH transport command; env-only, works on a fresh runner) |
| `anvil deployment install <target-id> <artifact-path>` | Local target-centric alias of `anvil server release install` — run ON the server runtime (requires a locally initialized server) |
| `anvil deployment activate <target-id> <project-id> <release-id>` | Local target-centric alias of `anvil server release activate` — run ON the server runtime |

## Typical CI/CD pipeline stages

```text
1. Lint & Test     -->  anvil pipeline ci
2. Build           -->  anvil pipeline build
3. Package         -->  anvil artifact package
4. Verify          -->  anvil artifact verify <path>
5. Upload (CI)     -->  anvil deployment upload <target-id> <path>
6. Install (server)-->  anvil server release install <project-id> <path>
                         (or the local alias: anvil deployment install <target-id> <path>)
7. Activate (server)--> anvil server release activate <project-id> <release-id>
                         (or the local alias: anvil deployment activate <target-id> <project-id> <release-id>)
```

Not all stages are required. A CI-only workflow might stop at step 3. A deployment workflow might skip step 1 if quality gates ran in a previous stage.

> **Steps 6-7 run ON the target server runtime.** `anvil deployment install` and
> `anvil deployment activate` are local target-centric aliases of the
> `server release` operations: they execute on a machine with a locally
> initialized server (the target server itself, or an operator workstation).
> A fresh CI runner performs step 5 (upload) only — it does not run the release
> lifecycle (EPIC-011 §8: CI is "not involved" in the release lifecycle).

## Installing Anvil in CI

Use the official install script:

```bash
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
```

For a specific version:

```bash
curl -fsSL https://github.com/maleolabs/anvil/releases/download/v1.2.0/install.sh | sh
```

The script detects your platform (linux/darwin, amd64/arm64) and installs to `/usr/local/bin/anvil`.

## SSH host key verification (TD-004)

`anvil deployment upload` reads its SSH credentials from environment variables
(ADR-019) and supports **opt-in host key verification** (TD-004):

| Variable | Required | Default | Description |
|---|---|---|---|
| `DEPLOY_SERVER_HOST` | yes | — | Server hostname or IP address |
| `DEPLOY_SERVER_USER` | yes | — | SSH username |
| `DEPLOY_SERVER_PORT` | no | `22` | SSH port |
| `DEPLOY_SSH_KEY` | yes | — | Path to the SSH private key file |
| `DEPLOY_SSH_KNOWN_HOSTS` | no | — | Path to an OpenSSH `known_hosts` file used to verify the server's host key. **When unset, host key verification is disabled** (legacy behavior) |
| `DEPLOY_SSH_KNOWN_HOSTS_MODE` | no | `strict` | Host key verification mode, only consulted when `DEPLOY_SSH_KNOWN_HOSTS` is set: `strict` (default — unknown or changed host keys are rejected, fail-closed) or `accept-new` (records an unknown host key on first contact; changed keys are still rejected) |

> **Security posture (TD-004):** host key verification is **opt-in** for
> backward compatibility — the default (no `DEPLOY_SSH_KNOWN_HOSTS`) does
> **not** verify the server's identity, which is insecure against
> man-in-the-middle attacks. **Configure `DEPLOY_SSH_KNOWN_HOSTS` for
> production deployments** (default mode `strict`); use `accept-new` only for
> first contact against a trusted network path.

The per-CI guides ([GitHub Actions](github-actions.md), [GitLab CI](gitlab-ci.md),
[Jenkins](jenkins.md)) cover how to define the credential variables per
platform; the two known-hosts variables above follow the same pattern.

## Framework-specific notes

### Laravel

Requires PHP, Composer, and Node.js/npm on the CI runner. See [github-actions.md](github-actions.md), [gitlab-ci.md](gitlab-ci.md), or [jenkins.md](jenkins.md) for setup examples.

### Flutter

Requires the Flutter SDK on the CI runner. See the platform-specific guides for setup examples.

## See also

- [Adapters Wiki](../README.md)
- [Laravel adapter](../adapters/laravel/)
- [Glossary](../glossary.md)
- [Limitations](../limitations.md)
