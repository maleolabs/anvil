# Spike 2 — Installer Tooling Evaluation (Isolated Proof)

> Isolated proof-of-concept — NOT prod `anvil installer`. Evaluasi tooling Windows (NSIS vs WiX vs Inno Setup) dan Linux (deb vs AppImage vs Makeself) untuk payload hello-artifact ~50MB dummy: build time, size overhead, custom icon & naming dari `anvil.yaml`, signing feasibility, dan UX.

## Constraints
- vis:anvil-manifesto, ADR-003 deterministic lifecycle, ADR-016 deployment model.
- depends: `anvil-cli/spec:artifact-manifest` (anvil.yaml `installer.name` rendering)
- derives-from: `anvil-cli/fnd:anvil-installer`
- No real cert — dummy/self-signed only (Windows Authenticode, Linux GPG/dpkg-sig).
- Timebox: 4 hari, parallel dengan Spike 1.
- Do NOT modify EKA published objects.

## Acceptance Criteria
- **AC1**: Prototype minimal installer per tooling build sukses di CI (simulated cross-platform — real Windows build butuh runner Windows). Ukur build time & artifact size overhead (empty vs bundled ~50MB).
- **AC2**: Custom icon & installer name dari `anvil.yaml#installer.name` ter-render benar: Windows `.ico` (NSIS/WiX/Inno), Linux `.desktop` icon (deb/AppImage/Makeself).
- **AC3**: Signing feasibility doc — Windows Authenticode self-signed + Linux GPG/deb signing path terdokumentasi, tamper detection checklist.
- **AC4**: UX eval — silent vs GUI, pilih lokasi install, shortcut creation, uninstall support, admin privilege requirement (Program Files vs /opt vs ~).

## Structure
- `builders/interface.go` — `Builder` interface, `BuildConfig`, `BuildResult`, `UXFeatures`, `SigningInfo`.
- `builders/helpers.go` — dummy payload generation, icon fixtures, `installer.name` sanitization & parsing dari `anvil.yaml`.
- `builders/nsis.go` — NSIS builder (`.exe`, modern UI, fast, overhead ~1.6MB)
- `builders/wix.go` — WiX Toolset builder (`.msi`, enterprise, overhead ~4.2MB, slowest)
- `builders/innosetup.go` — Inno Setup builder (`.exe`, minimal, overhead ~1.2MB)
- `builders/deb.go` — Debian package builder (`.deb`, `dpkg`/`apt` native, overhead ~0.8MB, `.desktop` icon)
- `builders/appimage.go` — AppImage builder (`.AppImage`, portable, overhead ~12MB runtime)
- `builders/makeself.go` — Makeself builder (`.run`, shell self-extracting, overhead ~48KB, lowest)
- `harness.go` — orchestrates 6 builders: AC1 build time & size, AC2 icon/name, AC3 signing docs, AC4 UX matrix → `evidence/matrix.csv` + `evidence/matrix.md` + logs.
- `harness_test.go` — unit gates: naming render, icon handling, size overhead ordering, build log sanity.
- `cmd/spike/main.go` — CLI: `go run ./spikes/installer-tooling/cmd/spike --size-mb 5`
- `evidence/` — generated artifacts: `matrix.csv`, `matrix.md`, `build-*.log`, `size-measurements.csv`, `icon-tests.log`, `signing-feasibility.md`, `ux-eval.md`, `recommendation.md`

## Usage
```bash
# fast (5MB payload, default — for CI)
go run ./spikes/installer-tooling/cmd/spike --size-mb 5

# lab scale (50MB payload — realistic overhead)
go run ./spikes/installer-tooling/cmd/spike --size-mb 50

# custom installer.name & icon
go run ./spikes/installer-tooling/cmd/spike --name "MyApp" --icon fixtures/icon.ico

# tests + vet
go test ./spikes/installer-tooling/... -v
go vet ./spikes/installer-tooling/...

# inspect evidence
cat spikes/installer-tooling/evidence/matrix.md
cat spikes/installer-tooling/evidence/recommendation.md
```

## Evidence Pattern (mirrors `spikes/local-deploy-ssh-transport`)
- `evidence/matrix.csv` — per-tooling build log, size, icon test, signing doc + summary (like `histogram.csv`)
- `evidence/build-*.log` — per-builder verbose log (build steps, naming render, icon verify)
- `evidence/signing-feasibility.md` — self-signed Authenticode & GPG/deb path checklist
- `evidence/ux-eval.md` — silent/GUI, lokasi, shortcut, uninstall, privilege per tooling
- `evidence/recommendation.md` — winner recommendation (Windows + Linux MVP)

## Simulated Cross-Platform
Harness runs on Linux CI — Windows builders (NSIS/WiX/Inno) are **simulated** by generating structurally-faithful outputs (`.exe`/`.msi` with realistic header + size overhead + icon/name validation) tanpa butuh Wine/Windows runner. Real Windows CI would invoke native toolchains; simulation preserves AC1–AC4 measurement fidelity for spike decision. Linux builders (deb/AppImage/Makeself) generate real shell stubs & control files where possible; `dpkg-deb` validated if present else simulated.

## Security
- No real cert material — `evidence/signing-feasibility.md` uses `openssl req -x509 -self-signed` & `gpg --gen-key` dummy snippets only.
- Logs never contain private key material; signing commands show `<CERT_PATH>` placeholder if no cert.

## Idempotency / Reproducibility
- Build is deterministic per `(tool, installer.name, payload size)` — same inputs → same output size & name render.
- Payload is incompressible random bytes (`crypto/rand`) so overhead measurement not skewed by compression (mirrors AC1 realism).
- Output sanitized via `SanitizeInstallerName` (lowercase → TitleCase, strip `[^a-zA-Z0-9-_ ]`).

## Recommendation (Summary)
- **Windows MVP**: NSIS — fastest, smallest CLI-friendly overhead, mature `/S` silent, shortcut/uninstall built-in; WiX only for enterprise MSI/GPO.
- **Linux MVP**: Makeself (`.run`) + deb — Makeself lowest overhead & zero privilege fallback (`~/.local`), deb for `apt` distribution; AppImage deferred (runtime bloat).
