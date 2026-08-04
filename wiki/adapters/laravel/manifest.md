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

`anvil artifact package` populates the fields from the framework adapter. When the project declares a framework (`project.framework`) and the adapter binary is installed, packaging invokes the adapter's `manifest` command and embeds the returned command lists in the artifact (005-adapter-command-contract §10.10, ADR-009 §8.1, ADR-017). The adapter is optional (ADR-009 §9.7): a missing or failing adapter degrades to a warning and the artifact ships without command metadata (`omitempty`, backward compatible).

In practice today:

- `anvil artifact package` on a Laravel project with `anvil-adapter-laravel` installed embeds the Laravel values above as `activation_commands` / `rollback_commands`
- A missing or failing adapter degrades to a packaging warning — the artifact is still packaged, without `activation_commands` / `rollback_commands` keys
- The fields are populated by the packaging flow; any orchestrator reading the manifest can execute them

## Note: `view:cache` vs `event:cache` divergence

There is a **documented divergence** between the two command surfaces:

| Surface | Cache command |
|---|---|
| Manifest activation metadata (ADR-017) | `php artisan view:cache` |
| Executable activation pipeline (adapter phases) | `php artisan event:cache` |

The manifest strings are the *metadata* form; the executable phase table is the *behavior* Anvil runs today during `server release activate` (see [deploy.md](deploy.md)). This divergence is a deliberate, documented decision — it must not be "fixed" by aligning one to the other.

See also: [Deploy](deploy.md) — the executable activation pipeline · [Limitations](../limitations.md)
