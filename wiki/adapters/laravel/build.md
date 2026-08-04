# Build Pipeline

A Laravel project created with `anvil init --framework laravel` gets a build pipeline tailored to Laravel's production build steps. It is executed with `anvil pipeline build`, which runs the pipeline defined in `.anvil/pipelines/build.yaml`.

## The generated pipeline

| Stage | Task | Command | Purpose |
|---|---|---|---|
| `dependencies` | `composer-install` | `composer install --no-dev --optimize-autoloader` | Install production dependencies, optimized autoloader |
| `assets` | `npm-build` | `npm run build` | Compile frontend assets |
| `optimize` | `cache-config` | `php artisan config:cache` | Cache configuration |
| `optimize` | `cache-route` | `php artisan route:cache` | Cache routes |
| `optimize` | `cache-view` | `php artisan view:cache` | Cache compiled views |

Tasks run in order; the pipeline stops at the first failing task.

## Running the build

```bash
anvil pipeline build                # default environment: development
anvil pipeline build --env production
anvil pipeline build --output dist  # or -o; sets $ANVIL_OUTPUT_DIR for tasks
```

| Flag | Description | Default |
|---|---|---|
| `--env <environment>` | Environment selection (`development`, `production`) | `development` |
| `--output` / `-o <dir>` | Output directory for build artifacts, injected as `ANVIL_OUTPUT_DIR` into every task's environment | (empty) |

## `--env` behavior in the default template

The engine supports environment-aware execution, but the **default Laravel template has no environment-specific overrides**: `--env development` and `--env production` run exactly the same commands. This is safe — the template's commands (Composer without dev dependencies, asset build, artisan caches) are production-oriented regardless of the env label.

## Customizing per environment (`environments:`)

Environment-specific behavior is a **customization point** in `build.yaml`: any task may declare an `environments:` map keyed by environment name (`development`, `production`, …). When `--env <name>` is given, the matching override replaces the task's base fields (command, args, working dir, env, timeout). Without an override for the selected environment, the task runs unchanged.

Example — skip the artisan caches in development, use `npm run build:prod` in production:

```yaml
pipeline:
    name: build
    stages:
        - name: optimize
          tasks:
            - name: cache-config
              command: php
              args: [artisan, config:cache]
              environments:
                development:
                  command: echo
                  args: [skipping-config-cache-in-dev]
        - name: assets
          tasks:
            - name: npm-build
              command: npm
              args: [run, build]
              environments:
                production:
                  args: [run, build:prod]
```

## Timeout note for the adapter build pipeline

The Laravel adapter executable also declares a build pipeline (composer → npm → `config:cache` → `route:cache` → `view:cache`). When the Core invokes an adapter's build command, the subprocess is bounded to **15 minutes** — longer than other adapter operations (30 seconds) because builds routinely include dependency installation and asset compilation.

**Current status:** `anvil server release build <project-id>` invokes the adapter executable's build pipeline with the 15-minute bound enforced. `anvil pipeline build` still executes the pipeline YAML directly and does **not** invoke the adapter executable — the local engine path remains a separate deferral (ADR-020 §3). See [limitations](../limitations.md).

See also: [Init](init.md) — how the template is generated · [Deploy](deploy.md)
