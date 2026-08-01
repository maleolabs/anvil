# Activation / Rollback Commands in the Artifact Manifest

Per **ADR-017** (Artifact-Centric Deployment Model), the artifact manifest stores the full activation and rollback command strings as deployment metadata. An orchestrator — Anvil or an external runner — reads these from the manifest and executes them during release activation and rollback.

The manifest fields are:

| Manifest field | JSON key | Contents |
|---|---|---|
| `ActivationCommands` | `activation_commands` | Commands executed at activation, in order |
| `RollbackCommands` | `rollback_commands` | Commands executed at rollback, in order |

Both fields are omitted from the manifest when empty.

## Laravel values

**Activation commands** (execution order — migration first, then cache warming):

```text
php artisan migrate --force
php artisan config:cache
php artisan route:cache
php artisan view:cache
```

**Rollback commands:**

```text
php artisan migrate:rollback
```

## Current wiring status — read this before relying on the fields

**`anvil artifact package` does not populate these fields yet.**

The manifest schema supports them, and the packaging engine stores whatever the packaging caller supplies — but the CLI wiring that would pull the Laravel values from the adapter at packaging time is **not implemented yet**. In practice today:

- Artifacts packaged with `anvil artifact package` contain **no** `activation_commands` / `rollback_commands` keys
- The fields are populated only when the packaging caller explicitly supplies the command lists (the Laravel values above are the defined source of truth for when that wiring lands)

Planned behavior: once wired, packaging a Laravel project will embed these commands automatically, and any orchestrator reading the manifest can execute them.

## Note: `view:cache` vs `event:cache` divergence

There is a **documented divergence** between the two command surfaces:

| Surface | Cache command |
|---|---|
| Manifest activation metadata (ADR-017) | `php artisan view:cache` |
| Executable activation pipeline (adapter phases) | `php artisan event:cache` |

The manifest strings are the *metadata* form; the executable phase table is the *behavior* Anvil runs today during `server release activate` (see [deploy.md](deploy.md)). This divergence is a deliberate, documented decision — it must not be "fixed" by aligning one to the other.

See also: [Deploy](deploy.md) — the executable activation pipeline · [Limitations](../limitations.md)
