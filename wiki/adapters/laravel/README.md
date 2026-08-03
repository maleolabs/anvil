# Laravel Adapter — Usage Guide

The Laravel adapter gives Anvil Laravel-specific behavior for building, packaging, verifying, and deploying Laravel applications. It is the first framework adapter and uses the **`server` deployment model**: releases are deployed to a server and activated in place.

## What you get

| Capability | What the adapter does |
|---|---|
| **Init template** | `anvil init my-app --framework laravel` generates a Laravel build pipeline and framework-aware project config ([init.md](init.md)) |
| **Build pipeline** | Composer → npm → artisan cache commands as the default `.anvil/pipelines/build.yaml` ([build.md](build.md)) |
| **Verification** | 8 Laravel structure checks that must pass before an artifact is installed on a server ([verify.md](verify.md)) |
| **Activation** | `php artisan migrate --force`, `config:cache`, `route:cache`, `event:cache` on release activate ([deploy.md](deploy.md)) |
| **Rollback** | `php artisan migrate:rollback` on release rollback; cache phases are irreversible ([deploy.md](deploy.md)) |
| **Manifest metadata** | Activation/rollback command strings for the artifact manifest (ADR-017) ([manifest.md](manifest.md)) |
| **Config extension** | Reserved `framework.laravel.*` configuration keys ([config.md](config.md)) |

## Prerequisites

- **Anvil CLI** — installed and on `PATH`
- **Adapter binary `anvil-adapter-laravel`** — on `PATH` (required for server install/activate/rollback; install it with `install.sh --with-adapters laravel` or `anvil adapter install laravel` — see [Install the adapter](#install-the-adapter))
- On the server / build machine: **PHP** (with the `artisan` entrypoint), **Composer**, and **Node.js/npm** (for the asset build)

## Install the adapter

The adapter binary ships with every Anvil release as `anvil-adapter-laravel-<os>-<arch>` (checksum-verified against `SHA256SUMS.txt`). Install it at setup time or per project:

```bash
# At install time — CLI plus adapter
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --with-adapters laravel

# Post-install — one command
anvil adapter install laravel
```

`anvil update` refreshes installed adapters automatically (refresh-only — it never installs new ones). Remove the adapter with `anvil adapter uninstall laravel`.

## Build from source (development)

Manual builds are an option for adapter development, not the standard install path:

```bash
go build -o anvil-adapter-laravel ./cmd/laravel-adapter
# make it discoverable, e.g.:
sudo mv anvil-adapter-laravel /usr/local/bin/
```

Anvil resolves the adapter as `anvil-adapter-laravel` on `PATH`. If it is missing, server operations fail with:

```text
adapter executable "anvil-adapter-laravel" not found on PATH: ... (install the adapter binary or configure its path)
```

## Quick start

```bash
# 1. Initialize a Laravel project (generates Laravel build pipeline + config)
anvil init my-app --framework laravel
cd my-app

# 2. Build (runs the generated build.yaml: composer → npm → artisan caches)
anvil pipeline build

# 3. Package an immutable artifact
anvil artifact package

# 4. On the target server: initialize the Runtime and register the project
anvil server init
anvil server project register \
  --project-id my-app \
  --install-root /srv/apps/my-app \
  --adapter laravel \
  --non-interactive

# 5. Install the artifact as a Release (runs the 8 Laravel verification checks)
anvil server release install my-app .anvil/artifacts/<artifact>.tar.gz

# 6. Activate the Release (runs Laravel activation phases)
anvil server release activate my-app <release-id>

# 7. Roll back if needed
anvil server release rollback my-app
```

## Documentation pages

| Page | Contents |
|---|---|
| [init.md](init.md) | What `anvil init --framework laravel` generates; framework selection errors |
| [build.md](build.md) | Build pipeline template, `--env` flag, `environments:` overrides |
| [deploy.md](deploy.md) | Server deployment: register, install, activate, rollback |
| [verify.md](verify.md) | The 8 verification checks (table) and where they run |
| [manifest.md](manifest.md) | Activation/rollback commands in the artifact manifest |
| [config.md](config.md) | `framework.laravel.*` keys (reserved) |

See also: [Adapters Wiki](../README.md) · [Limitations](../limitations.md) · [Glossary](../glossary.md)
