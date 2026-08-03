# Limitations and Deferrals

Everything below is **verified current behavior**. Each item states what works today and what is coming. When a limitation is lifted, update the corresponding page and remove the entry here.

## 1. Adapter binary distribution

- **Status:** resolved (TS-007-034 — TS-007-037)
- **Works today:** adapter binaries are distributed as release assets `anvil-adapter-<framework>-{os}-{arch}` (linux/darwin × amd64/arm64), verified against `SHA256SUMS.txt`, and installed as `anvil-adapter-<framework>` next to the CLI:
  - `install.sh --with-adapters <name[,name...]>` — installs adapters alongside the CLI (whitelist: `laravel`, `flutter` only)
  - `anvil adapter install <name>` / `anvil adapter uninstall <name>` — per-adapter lifecycle after install (`--force` / `--json` supported)
  - `anvil update` — refreshes installed adapters to the latest release (checksum-verified, atomic replace; refresh-only, never installs new ones)
- **Previously:** `install.sh` and release builds shipped only the `anvil` binary, so a fresh server failed with `adapter executable "anvil-adapter-laravel" not found on PATH` and the workaround was a manual `go build` per machine. Manual builds remain an option for development only — see the [Laravel adapter guide](adapters/laravel/README.md).

## 2. Manifest commands not populated by `anvil artifact package`

- **Status:** limitation (CLI wiring pending)
- **Works today:** the artifact manifest schema supports `activation_commands` / `rollback_commands` (ADR-017); the packaging engine stores them when the caller supplies them; the Laravel values are defined (`php artisan migrate --force` etc. — see [manifest.md](adapters/laravel/manifest.md))
- **Does not work:** `anvil artifact package` does not yet pull the Laravel values from the adapter — packaged artifacts contain no command metadata
- **Coming:** CLI wiring at packaging time so Laravel artifacts embed the commands automatically

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

## 5. `anvil artifact verify` does not run framework checks

- **Status:** limitation
- **Works today:** `anvil artifact verify <path>` verifies generic integrity (archive validity, manifest presence/content, checksum match)
- **Does not work:** the 8 Laravel verification checks run only at server install time — there is no CLI command to run them against an artifact locally
- **Coming:** an artifact-verify integration that runs adapter checks on demand
- See [verify.md](adapters/laravel/verify.md)

## 6. `vendor/` inclusion depends on the generated include override

- **Status:** design constraint (not a bug)
- **Works today:** `anvil init --framework laravel` adds `artifact.include: [vendor/**]`, which overrides the compiled default exclude that strips `vendor/` and `node_modules/` — `vendor/` is runtime-critical for Laravel and must ship in the artifact
- **Watch out:** removing or weakening the override silently strips `vendor/` from artifacts; install then fails the `vendor_present` check
- See [init.md](adapters/laravel/init.md)

## 7. Adapter build pipeline not wired to any CLI command

- **Status:** deferral
- **Works today:** `anvil pipeline build` executes `.anvil/pipelines/build.yaml` directly (the Laravel template generated at init)
- **Does not work:** the adapter executable's own `build` pipeline has no production caller — the 15-minute build timeout bound exists in the Core but applies only once the wiring lands
- **Coming:** server release build path wired to the adapter `build` command (TS-007-040); build contract gains `--target`/`--strict` parity (TS-007-041)
- See [build.md](adapters/laravel/build.md)

## 8. Framework knowledge is Core-embedded (templates, discovery, defaults)

- **Status:** partially addressed — `anvil adapter list` has migrated to system scanning (ADR-020 §2); template ownership and `inspect`/`use` discovery remain Core-closed
- **Works today:** `anvil init --framework laravel|flutter` generates `.anvil/pipelines/build.yaml` from Go data embedded in the Core binary (`internal/execution/laravel_template.go`, `flutter_template.go`); `anvil adapter list` detects installed adapters by scanning the CLI directory and PATH for `anvil-adapter-*` binaries (no closed set), and `--available` lists the adapters published in the latest release; Laravel-specific artifact defaults (`vendor/**` include) are a Core switch
- **Does not work:** a third-party adapter binary on PATH (`anvil-adapter-rails`) is visible in `anvil adapter list` but still rejected by `anvil adapter use`/`inspect` (closed known-framework set); adding a framework requires a Core release; template and adapter build phases are two sources that can drift
- **Coming:** adapter-owned templates via a `template` command (TS-007-038), PATH-based discovery for `inspect`/`use` (TS-007-039), server build wiring (TS-007-040), build contract target/strict parity (TS-007-041) — per ADR-020
- See [ADR-020](../docs/decisions/020-adapter-owned-pipeline-templates-and-discovery.md)
