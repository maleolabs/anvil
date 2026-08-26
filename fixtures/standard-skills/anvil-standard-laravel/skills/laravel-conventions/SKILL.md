---
name: laravel-conventions
description: "Laravel project conventions under the Anvil delivery lifecycle standard — project identity, scaffold, build pipeline, configuration surface, and verification expectations. Use when working on a Laravel project managed by Anvil: check the declared standard capability before assuming framework-version support, and follow the generated pipeline rather than inventing build steps."
license: MIT
---

# Laravel Conventions

The Laravel delivery lifecycle standard (`anvil-standard-laravel`) owns all
Laravel-specific knowledge in Anvil: the lifecycle phases, verification
rules, build templates, and configuration surface. Anvil itself is
framework-agnostic — it runs the standard as a subprocess and never embeds
Laravel behavior.

## Project identity

- The Anvil project is a directory tree with `anvil.yaml` at its root. Run
  `anvil init <name>` to create one, then register the Laravel standard:
  `anvil standard install anvil-standard-laravel <version>`.
- The standard declares its **framework-version support scope** in its
  registry metadata (`capability.frameworkVersion`). Before writing Laravel
  version-specific guidance, check the installed standard's declared scope
  (`anvil standard list`) — a mismatch between the project's Laravel version
  and the standard's support scope is a compatibility problem, not a
  convention to work around.
- The standard targets a specific contract version of the delivery lifecycle
  specification. Follow the declared contract surface; the standard supplies
  content within the lifecycle, it does not redefine it.

## Scaffold

- Project initialization generates the framework's build pipeline and
  configuration from the **installed standard** (A10) — not from Anvil core.
- The standard's build pipeline template is the source of truth for the
  project's build steps. Do not hand-write a parallel build; extend or
  adjust the generated pipeline through the standard's configuration
  surface.
- Laravel-specific configuration keys live under the framework's own
  namespace (`framework.laravel.*`, e.g. `framework.laravel.version`,
  `framework.laravel.php_version`, `framework.laravel.migrations.path`,
  `framework.laravel.cache.store`, `framework.laravel.composer_flags`) and
  are validated by the standard itself. Anvil enforces namespace isolation
  and passes values through; it does not interpret them.

## Build conventions

The generated pipeline reflects the Laravel build shape, in order:

1. dependency install — `composer install --no-dev --optimize-autoloader`
   (with lockfiles committed, so installs are reproducible);
2. asset build — `npm run build`;
3. artisan optimization caches — `config:cache`, `route:cache`,
   `view:cache` (view:cache is a build phase; it is deliberately absent
   from activation).

Each build step must be reproducible from a clean checkout: lockfiles are
committed, and the pipeline must not depend on developer-machine state.

## Verification expectations

- The standard declares **eight structural verification checks** that run
  during artifact verification, each validating that a required file or
  directory is present in the artifact:
  `vendor_present` (`vendor/autoload.php`), `bootstrap_structure`
  (`bootstrap/app.php`), `config_files` (`config/app.php` + `.env.example`),
  `artisan_file` (`artisan`), `composer_json` (`composer.json`), `env_file`
  (`.env` or `.env.example`), `app_directory` (`app/`), `routes_directory`
  (`routes/`).
- Gates are mandatory and unskippable: the standard adds checks, it never
  weakens them. Verification outcomes are recorded as lifecycle evidence;
  treat a failed check as a release blocker, not a warning.

## Configuration surface

- Read the standard's declared configuration extension before setting
  Laravel options: keys under `framework.laravel.*` are validated by the
  standard, and unknown or mis-typed values are rejected with actionable
  errors.
- Do not invent keys outside the declared namespace; Anvil rejects unknown
  scalar values it does not recognize.

## When to use this skill

- Orienting in a Laravel project managed by Anvil (what is generated, what
  is verified, what the configuration surface is).
- Writing or editing build configuration for a Laravel project under Anvil.
- Checking whether a Laravel feature is supported by the installed standard's
  capability declaration.

For the lifecycle itself — activation, rollback, failure semantics — load
`laravel-delivery`.
