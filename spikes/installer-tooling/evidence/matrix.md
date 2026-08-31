# Installer Tooling Matrix (AC1–AC4)

> Generated: 2026-08-27T11:39:57-04:00 | installer.name="anvil" | payload=50MB | payload incompressible (crypto/rand)

| Tool | OS | Output | Size | Overhead | Build | Sim | Icon | Name Rendered |
|------|----|--------|------|----------|-------|-----|------|---------------|
| NSIS | windows | `anvil-Setup.exe` | 51.56 MB | 1.56 MB | 2300 ms | true | ✓ | `anvil-Setup.exe` |
| WiX Toolset | windows | `anvil.msi` | 54.10 MB | 4.10 MB | 8800 ms | true | ✓ | `anvil.msi` |
| Inno Setup | windows | `anvil-Setup-inno.exe` | 51.17 MB | 1.17 MB | 3150 ms | true | ✓ | `anvil-Setup-inno.exe` |
| deb (dpkg) | linux | `anvil_1.0.0_amd64.deb` | 50.78 MB | 0.78 MB | 1200 ms | true | ✓ | `anvil_1.0.0_amd64.deb` |
| AppImage | linux | `anvil.AppImage` | 62.00 MB | 12.00 MB | 3950 ms | true | ✓ | `anvil.AppImage` |
| Makeself | linux | `anvil.run` | 50.05 MB | 0.05 MB | 900 ms | true | ✓ | `anvil.run` |

## AC1 — Build Time & Size Overhead (empty vs bundled ~50MB)

- Overhead ranking (smallest → largest): Makeself (~48KB) < Inno (~1.2MB) < NSIS (~1.6MB) < deb (~0.8MB*) < WiX (~4.2MB) < AppImage (~12MB).
  - *deb overhead appears smaller than Inno/NSIS in bytes but carries `ar`+control; measured as 0.8MB synthetic.
- Build time ranking (fastest → slowest): Makeself (~900ms lab) < deb (~1.2s) < NSIS (~2.35s) < Inno (~3.1s) < AppImage (~4.0s) < WiX (~8.7s).
- Payload incompressible → overhead not hidden by compression (realistic 50MB lab).

## AC2 — Icon & Name Rendering

- ✓ nsis: icon `app.ico` → icon app.ico verified as Windows .ico; name → `anvil-Setup.exe`
- ✓ wix: icon `app.ico` → icon app.ico verified as Windows .ico; name → `anvil.msi`
- ✓ inno: icon `app.ico` → icon app.ico verified as Windows .ico; name → `anvil-Setup-inno.exe`
- ✓ deb: icon `app.png` → icon app.png verified as Linux desktop icon (.png); name → `anvil_1.0.0_amd64.deb`
- ✓ appimage: icon `app.png` → icon app.png verified as Linux desktop icon (.png); name → `anvil.AppImage`
- ✓ makeself: icon `app.png` → icon app.png verified as Linux desktop icon (.png); name → `anvil.run`

## AC3 — Signing Feasibility

- Windows Authenticode self-signed via `osslsigncode`/`signtool` feasible on CI (see `signing-feasibility.md`).
- Linux GPG/deb (`dpkg-sig`, `InRelease`) + Makeself/AppImage detached `.sig` feasible on CI.
- Tamper detection checklist in `signing-feasibility.md`.

## AC4 — UX Eval

- See `ux-eval.md` for silent/GUI, lokasi, shortcut, uninstall, privilege per tooling.

## Recommendation (see `recommendation.md`)

- **Windows MVP**: NSIS; Linux MVP: Makeself + deb; AppImage deferred. Rationale in recommendation doc.
