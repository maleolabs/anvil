# Limitations and Deferrals

Everything below is **verified current behavior**. Each item states what works today and what is coming. When a limitation is lifted, update the corresponding page and remove the entry here.

## 1. Adapter binary is not shipped by the installer

- **Status:** limitation
- **Works today:** the adapter executable `anvil-adapter-laravel` exists in the repository and is resolved by Anvil on `PATH` (convention: `anvil-adapter-<framework>`)
- **Does not work:** `install.sh` and release builds install only the `anvil` binary — the adapter binary is **not** distributed
- **Impact:** on a fresh server, `server release install/activate/rollback` on a Laravel project fails with `adapter executable "anvil-adapter-laravel" not found on PATH`
- **Workaround:** build manually once per machine: `go build -o anvil-adapter-laravel ./cmd/laravel-adapter && sudo mv anvil-adapter-laravel /usr/local/bin/`
- **Coming:** adapter binaries bundled with install.sh / release assets

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

## 4. `anvil init --framework flutter` is not supported

- **Status:** planned, not implemented
- **Works today:** `anvil init my-app --framework laravel` (implemented); `anvil init my-app` (plain, no framework)
- **Does not work:** `--framework flutter` errors with `framework "flutter" is not yet supported (template not available)`; no files are created
- **Coming:** Flutter adapter (templates, verification, deployment model) per the roadmap

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
- See [build.md](adapters/laravel/build.md)
