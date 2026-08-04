# Limitations and Deferrals

Everything below is **verified current behavior**. Each item states what works today and what is coming. When a limitation is lifted, update the corresponding page and remove the entry here.

## 1. Adapter binary distribution

- **Status:** resolved (TS-007-034 — TS-007-037)
- **Works today:** adapter binaries are distributed as release assets `anvil-adapter-<framework>-{os}-{arch}` (linux/darwin × amd64/arm64), verified against `SHA256SUMS.txt`, and installed as `anvil-adapter-<framework>` next to the CLI:
  - `install.sh --with-adapters <name[,name...]>` — installs adapters alongside the CLI (whitelist: `laravel`, `flutter` only)
  - `anvil adapter install <name>` / `anvil adapter uninstall <name>` — per-adapter lifecycle after install (`--force` / `--json` supported)
  - `anvil update` — refreshes installed adapters to the latest release (checksum-verified, atomic replace; refresh-only, never installs new ones)
- **Previously:** `install.sh` and release builds shipped only the `anvil` binary, so a fresh server failed with `adapter executable "anvil-adapter-laravel" not found on PATH` and the workaround was a manual `go build` per machine. Manual builds remain an option for development only — see the [Laravel adapter guide](adapters/laravel/README.md).

## 2. Manifest command metadata in packaged artifacts

- **Status:** resolved (CH-012-001)
- **Works today:** `anvil artifact package` pulls `activation_commands` / `rollback_commands` from the adapter's manifest command when the project declares a framework and the adapter binary is installed (005-adapter-command-contract §10.10, ADR-009 §8.1, ADR-017). The adapter is optional (ADR-009 §9.7) — a missing or failing adapter degrades to a warning and artifacts ship without command metadata (`omitempty`, backward compatible). The Laravel values are defined (`php artisan migrate --force` etc. — see [manifest.md](adapters/laravel/manifest.md))
- **Previously:** packaging never pulled the Laravel values from the adapter — packaged artifacts contained no command metadata

## 3. `framework.laravel.*` config keys are inert

- **Status:** reserved, not yet effective
- **Works today:** the keys are declared (namespace `framework.laravel.`) and validated (SemVer, cache driver list, relative-path, safe-flags rules)
- **Does not work:** there is no `framework.laravel:` section in `anvil.yaml` to set them, and no build/activation flow consumes them
- **Coming:** consumption by build and activation flows + a dedicated `anvil.yaml` section
- See [config.md](adapters/laravel/config.md) for the full key table

## 4. `anvil init --framework flutter` has no dedicated wiki yet

- **Status:** implemented, documentation pending
- **Works today:** `anvil init my-app --framework flutter` is implemented and generates a platform-aware build template (web / apk / ios targets via `metadata.platforms` / `metadata.target`); `anvil init my-app --framework laravel` (implemented); `anvil init my-app` (plain, no framework)
- **Does not work:** there is no `wiki/adapters/flutter/` usage guide yet
- **Note:** the Flutter adapter binary is distributed — install it with `install.sh --with-adapters flutter` or `anvil adapter install flutter` (see item 1)
- **Coming:** dedicated Flutter adapter wiki (templates, verification, deployment model)

## 5. `anvil artifact verify` and framework verification checks

- **Status:** resolved (CH-012-001)
- **Works today:** `anvil artifact verify <path>` runs the generic integrity checks (archive validity, manifest presence/content, checksum match) and then the adapter-declared framework verification checks when the project declares a framework and the adapter is installed (005-adapter-command-contract §4, TS-P7-11; `runFrameworkVerification`). The adapter is optional (ADR-009 §9.7) — a missing adapter degrades to a warning and only the generic checks run; a present but failing adapter fails the verification
- **Previously:** the 8 Laravel verification checks ran only at server install time — there was no CLI command to run them against an artifact locally
- See [verify.md](adapters/laravel/verify.md)

## 6. `vendor/` inclusion depends on the generated include override

- **Status:** design constraint (not a bug)
- **Works today:** `anvil init --framework laravel` adds `artifact.include: [vendor/**]`, which overrides the compiled default exclude that strips `vendor/` and `node_modules/` — `vendor/` is runtime-critical for Laravel and must ship in the artifact
- **Watch out:** removing or weakening the override silently strips `vendor/` from artifacts; install then fails the `vendor_present` check
- See [init.md](adapters/laravel/init.md)

## 7. Local `anvil pipeline build` does not invoke the adapter

- **Status:** partially resolved — server release builds run through the adapter; local pipeline builds stay in the generic engine
- **Works today:** `anvil server release build <project-id>` runs the project's framework adapter `build` command — the adapter-owned build phases are the single source of build knowledge at server release time (ADR-020 §4), the 15-minute timeout bound is enforced, and `--target`/`--strict` are supported (TS-007-040, TS-007-041); `anvil pipeline build` executes `.anvil/pipelines/build.yaml` directly (the Laravel template generated at init)
- **Does not work:** local `anvil pipeline build` still executes the pipeline YAML via the generic engine instead of dispatching to the adapter executable (ADR-020 §3) — the local template and the adapter build phases can still drift
- **Coming:** optional dispatch of local builds to the adapter (ADR-020 §3)
- See [build.md](adapters/laravel/build.md)

## 8. Framework knowledge is Core-embedded (templates, discovery, defaults)

- **Status:** resolved — framework knowledge (pipeline templates, build phases, discovery) lives in adapter binaries per ADR-020; adding a framework means writing an adapter binary, with no Core release required for discovery, template generation, or server builds. The remaining Core-embedded piece is documented below as a deferral.
- **Works today (TS-007-038..041):**
  - **Adapter-owned templates** — `anvil init --framework laravel|flutter` invokes the adapter's `template` command, validates the returned pipeline definitions through the pipeline loader, and writes `.anvil/pipelines/build.yaml` / `ci.yaml` from them. The Core-embedded `LaravelBuildPipeline`/`FlutterBuildPipeline` template functions are removed; the generic default pipeline remains as the offline fallback.
  - **PATH-based discovery** — `anvil adapter list`, `anvil adapter inspect`, and `anvil adapter use` resolve `anvil-adapter-*` executables from the CLI install directory and PATH; a binary counts as an adapter only when it answers the `capabilities` probe, so third-party adapters (e.g. a community `anvil-adapter-rails`) appear in `anvil adapter list` and are usable via `anvil adapter use` and server builds without Core changes. Non-Anvil binaries that fail the probe are excluded.
  - **Server build wiring** — `anvil server release build <project-id>` executes the project adapter's build phases (15-minute bound, first-failing-phase semantics, `--target`/`--strict` parity) — the adapter-owned phases are the single source of build knowledge at release time.
- **Does not work:** local `anvil pipeline build` still executes `.anvil/pipelines/build.yaml` via the generic engine instead of dispatching to the adapter executable — optional dispatch is deferred (ADR-020 §3, see item 7). `anvil init --framework <f>` still validates the framework name against the Core's built-in whitelist (`laravel`, `flutter`; `unknown framework` error otherwise) and `config.NewFrameworkProjectConfig` still applies framework-specific artifact defaults — the config-layer migration is outside ADR-020's scope.
- **Coming:** optional dispatch of local builds to the adapter (ADR-020 §3)
- See [ADR-020](../docs/decisions/020-adapter-owned-pipeline-templates-and-discovery.md)
