# Project Initialization with the Laravel Framework

`anvil init` creates a new Anvil project. With `--framework laravel`, initialization applies Laravel-specific defaults.

## Command

```bash
anvil init my-app --framework laravel
```

| Flag | Description | Default |
|---|---|---|
| `--path <dir>` | Target directory for the project | `.` |
| `--framework <name>` | Framework for template generation (currently only `laravel`; `flutter` is planned) | (none — plain project) |

Project names may only contain letters, numbers, hyphens, and underscores.

## What it generates

```
my-app/
├── anvil.yaml                  # project config with Laravel defaults
└── .anvil/
    ├── project-identity.json   # immutable project identity
    ├── pipelines/
    │   ├── build.yaml          # Laravel build template
    │   └── ci.yaml             # default CI template
    └── state/
        └── lifecycle.yaml      # project lifecycle state (Created)
```

### `anvil.yaml` — Laravel-specific values

```yaml
project:
    name: my-app
    version: "1.0.0"
    description: ""
    framework: laravel
artifact:
    include:
        - vendor/**
    exclude: []
```

Two Laravel-specific defaults are applied:

1. **`project.framework: laravel`** — records the framework; it is the value the server registry uses to select the adapter (`--adapter laravel`, see [deploy.md](deploy.md)). Plain projects (`anvil init my-app`) do **not** get a `framework` key.
2. **`artifact.include: [vendor/**]`** — keeps `vendor/` in the packaged artifact.

### Why the `vendor/**` include override?

Anvil's compiled default excludes strip `vendor/` and `node_modules/` from packaged artifacts — correct for most compiled projects. For Laravel, `vendor/` is **runtime-critical** (Composer autoloading, framework code), so the Laravel template adds `vendor/**` to `artifact.include`. An include pattern overrides the compiled default exclude, so `vendor/` stays in the artifact. If you remove or weaken this override, `vendor/` will be stripped from your artifacts and the `vendor_present` verification check will fail at install.

### `.anvil/pipelines/build.yaml` — Laravel build template

Generated from the Laravel template (logical content):

```yaml
pipeline:
    name: build
    stages:
        - name: dependencies
          tasks:
            - name: composer-install
              command: composer
              args: [install, --no-dev, --optimize-autoloader]
        - name: assets
          tasks:
            - name: npm-build
              command: npm
              args: [run, build]
        - name: optimize
          tasks:
            - name: cache-config
              command: php
              args: [artisan, config:cache]
            - name: cache-route
              command: php
              args: [artisan, route:cache]
            - name: cache-view
              command: php
              args: [artisan, view:cache]
```

See [build.md](build.md) for how it executes and how to customize it.

### `.anvil/pipelines/ci.yaml`

The default CI template (placeholder `echo` tasks for build and test stages) — identical for plain and Laravel projects.

## Init without a framework

```bash
anvil init my-app
```

Creates a plain project: default (empty) build template, no `framework` key, no include overrides. This is the historical behavior — framework flags only change behavior when a framework template is selected.

## Framework selection errors

| Command | Result |
|---|---|
| `anvil init my-app --framework flutter` | Error: `framework "flutter" is not yet supported (template not available)` — Flutter is on the roadmap but has no template yet |
| `anvil init my-app --framework symfony` | Error: `unknown framework "symfony"` |

Both errors occur **before any file or directory is created** — a failed framework selection leaves nothing behind.

## Example output

```text
Project 'my-app' created with 'laravel' framework. Ready for use.
Next steps:
  cd . && anvil config list
```

## What init does NOT do

- Does not create runtime state (releases, artifacts, execution history)
- Does not scaffold Laravel application files (controllers, migrations, Blade views, etc.) — Anvil is a deployment tool, not a Laravel installer
- Does not install the adapter binary (see [limitations](../limitations.md))

See also: [Usage overview](README.md) · [Build pipeline](build.md) · [Deploy](deploy.md)
