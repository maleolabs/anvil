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
| Upload to target server | Anvil (`anvil deployment upload`) |
| Install and activate release | Anvil (`anvil deployment install/activate`) |

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
  +-----------+     anvil deployment       +------------------+
  | Trigger   | ---- upload ------------>  | Receive artifact |
  +-----------+     install ------------>  | Install release  |
       |           activate ----------->  | Activate release |
       v                                   +------------------+
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
| `anvil deployment upload <target-id> <artifact-path>` | Upload artifact to a remote server |
| `anvil deployment install <target-id> <artifact-path>` | Install artifact on target |
| `anvil deployment activate <target-id> <project-id> <release-id>` | Activate the release |

## Typical CI/CD pipeline stages

```text
1. Lint & Test     -->  anvil pipeline ci
2. Build           -->  anvil pipeline build
3. Package         -->  anvil artifact package
4. Verify          -->  anvil artifact verify <path>
5. Upload          -->  anvil deployment upload <target-id> <path>
6. Install         -->  anvil deployment install <target-id> <path>
7. Activate        -->  anvil deployment activate <target-id> <project-id> <release-id>
```

Not all stages are required. A CI-only workflow might stop at step 3. A deployment workflow might skip step 1 if quality gates ran in a previous stage.

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
