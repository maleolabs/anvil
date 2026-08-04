# Server Deployment (Laravel)

Laravel uses the **`server` deployment model**: artifacts are installed on a server and the release is **activated in place** — the release directory becomes the live application and Laravel's own commands (migrations, caches) are executed against it.

> **Terminology:** `anvil server release ...` commands are project-centric (for server operators). The equivalent target-centric commands (`anvil deployment install/activate/rollback/info`) are local aliases: they perform the same underlying operations on the local server runtime and honor the adapter the same way. Choose one style per workflow. `anvil deployment upload` is the only SSH transport command — it delivers the artifact from CI to the server and needs no local server state (TD-006).

## 1. Register the project with the adapter

```bash
anvil server init   # once per server, if not already done

anvil server project register \
  --project-id my-app \
  --install-root /srv/apps/my-app \
  --adapter laravel \
  --non-interactive
```

### Register flags

| Flag | Required | Description |
|---|---|---|
| `--project-id <id>` | Yes (non-interactive) | Unique project identifier |
| `--install-root <path>` | Yes (non-interactive) | Absolute install path on the server |
| `--adapter <name>` | — | Deployment adapter — use `laravel` to select the Laravel adapter |
| `--display-name <name>` | — | Human-readable display name |
| `--owner <user>` / `--group <group>` | — | Ownership for the installed files |
| `--shared-link from=<path>,to=<path>` | — | Repeatable; symlinks shared resources (e.g. `.env`, `storage`) into the release |
| `--non-interactive` | — | Skip prompts; requires `--project-id` and `--install-root` (also implied when both are provided) |
| `--server-root <path>` | — | Override config root (non-production only) |

Interactive mode (no flags) prompts for the same fields.

## 2. Install the artifact as a Release

```bash
anvil server release install my-app .anvil/artifacts/<artifact>.tar.gz
```

Install runs, in order:

1. Generic artifact integrity verification (archive, manifest, checksum)
2. **The 8 Laravel verification checks** — all must pass or the install fails (see [verify.md](verify.md))
3. Project ID match between artifact manifest and registry
4. Release creation in `ready` stage

The Release is **not** automatically activated.

## 3. Activate the Release

```bash
anvil server release activate my-app <release-id>
```

The artifact is extracted into the release directory (shared links applied), the release is promoted to active, and then the **adapter activation phases** run — each as `php artisan <command>` from the release directory:

| Order | Phase | Command | Reversible? |
|---|---|---|---|
| 1 | `migrate` | `php artisan migrate --force` | Reversible (`migrate:rollback`) |
| 2 | `config_cache` | `php artisan config:cache` | Irreversible |
| 3 | `route_cache` | `php artisan route:cache` | Irreversible |
| 4 | `event_cache` | `php artisan event:cache` | Irreversible |

A failing phase fails the activation command with an error. Note the ordering: the release is promoted to `active` **before** the adapter phases run, so a failed phase leaves the release recorded as active — re-run `anvil server release activate` to converge (the phases are re-executed from the start).

## 4. Roll back a Release

```bash
anvil server release rollback my-app
```

Rollback restores the previously active release, then runs the adapter rollback operations:

| Phase | Rollback behavior |
|---|---|
| `migrate` | `php artisan migrate:rollback` — reverses the migration |
| `config_cache` / `route_cache` / `event_cache` | **Irreversible** — a rollback cannot undo a cache. The adapter reports an *informational* result and the rollback proceeds without undoing these operations. The restored release's own activation regenerates its caches from its code. |

Rollback never blocks on irreversible phases.

## Adapter binary requirement

Install, activate, and rollback all invoke the adapter executable `anvil-adapter-laravel` (resolved on `PATH`). If it is missing, the operation fails with:

```text
adapter executable "anvil-adapter-laravel" not found on PATH: ... (install the adapter binary or configure its path)
```

Install it once per server — the adapter is distributed with every Anvil release:

```bash
# At install time — CLI plus adapter
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --with-adapters laravel

# Post-install — one command
anvil adapter install laravel
```

`anvil update` refreshes installed adapters automatically. For adapter development, a manual build remains possible: `go build -o anvil-adapter-laravel ./cmd/laravel-adapter && sudo mv anvil-adapter-laravel /usr/local/bin/`. See the [Laravel adapter guide](README.md).

## Server directory layout (per project)

```text
/srv/apps/my-app/
├── artifacts/            # installed artifact archives
├── releases/             # extracted releases (one dir per release)
├── shared/               # shared resources (config, storage, logs)
├── state/releases/       # release state JSON
└── runtime-state.json    # active release pointer
```

See also: [Verification checks](verify.md) · [Manifest commands](manifest.md) · [Glossary](../glossary.md)
