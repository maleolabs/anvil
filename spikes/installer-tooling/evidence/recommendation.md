# Recommendation — Installer Tooling Winner (Spike 2)

> Evidence: `matrix.md` / `matrix.csv` — payload=50MB, installer.name="anvil", generated 2026-08-27 11:39

## Winners

### Windows MVP: **NSIS**

- **Why NSIS over WiX/Inno**: fastest build (2.35s lab vs WiX 8.7s), smallest CLI-friendly overhead (1.6MB vs WiX 4.2MB), mature `/S` silent, Modern UI 2 GUI, `Choose Location`, shortcut + uninstaller + ARP out-of-box, per-user fallback avoids UAC (`$LOCALAPPDATA`), scripting flexible enough for anvil `installer.name` + `.ico` + `Exec` payload without XML/GUID ceremony.
- **When to prefer WiX**: enterprise GPO/AD distribution, SCCM, transactional MSI rollback, corporate compliance requires MSI. Keep WiX as **enterprise profile** behind feature flag — not MVP default.
- **Inno position**: excellent fallback if NSIS scripting hits limit; overhead slightly smaller (1.2MB) but less flexible for complex preflight checks than NSIS. Ranked #2 Windows.

### Linux MVP: **Makeself (.run) + deb** (dual)

- **Makeself (.run)** — primary for MVP single-server deploy (Anvil's local-deploy-ssh model): lowest overhead (~48KB), fastest build (~0.9s lab), `--target` location choice, `~/.local` no-admin fallback, `chmod +x && ./app.run` UX trivial for operator, GPG detached sig feasible.
- **deb** — secondary for managed fleet: native `apt` distribution, smallest managed overhead (0.8MB), `.desktop` shortcut + `dpkg -r` uninstall, GPG `InRelease` trust chain. Requires root (`/opt`) — complement Makeself per-user path.
- **AppImage deferred**: 12MB runtime bloat dominates 50MB payload (24% overhead vs Makeself 0.09%); no package-manager integration; useful later for desktop GUI distribution, not for server CLI artifact.

## Matrix Summary

| Rank | Tool | Overhead | Build (50MB) | Privilege | Silent |
|------|------|----------|--------------|-----------|--------|
| 1 | Makeself | 48 KB | ~900 ms | no (~/ ) | --silent |
| 2 | deb | 0.8 MB | ~1.2 s | yes (/opt) | -y |
| 3 | NSIS | 1.6 MB | ~2.35 s | optional | /S |
| 4 | Inno | 1.2 MB | ~3.1 s | optional | /SILENT |
| 5 | AppImage | 12 MB | ~4.0 s | no | n/a |
| 6 | WiX | 4.2 MB | ~8.7 s | yes | /qn |

## Trade-offs & Risks

- **NSIS vs WiX**: NSIS loses MSI transactional rollback & GPO; mitigate via `anvil rollback` + idempotent deploy. WiX cost is XML complexity & 3.7× slower CI.
- **Makeself trust**: operator must verify `.sig`/checksum before `chmod +x` — no OS enforcement; mitigate via docs + `anvil verify` checksum gate (already in artifact-manifest).
- **deb root requirement**: `apt install` needs sudo; mitigate via Makeself fallback for non-root servers.
- **AppImage bloat**: defer until desktop distribution needed; revisit if Wayland sandbox requirements emerge.
- **Signing production**: self-signed spike certs trigger SmartScreen/apt warnings; production needs CA EV (Windows) + repo keyring distribution (Linux) — track as follow-up spk:signing-prod.

## Next Steps (post-spike)

1. Implement `internal/installer` — NSIS + Makeself builders wired to `anvil build --installer` (use `builders/*` as reference, not import spike directly).
2. Add Windows runner (GitHub Actions `windows-latest`) with `makensis` + `osslsigncode` for real Authenticode smoke test.
3. Add `dpkg-deb` real build path when `dpkg-deb` present (`SPIKE_REAL_LINUX=1`); golden-file test for `.desktop` + `control`.
4. Wire `anvil.yaml#installer.name` + `installer.icon` (.ico/.png) into manifest + installer filename (AC2). Add icon validation gate (`eka validate` warning if icon missing).
5. File follow-up spike: `spk:signing-prod` — CA EV procurement, GPG keyring distribution, `anvil verify` tamper gate integration.
6. Update `anvil-cli/fnd:anvil-installer` with conclusion (NSIS + Makeself) and link to `spikes/installer-tooling/evidence/matrix.md`.

## Evidence Index

- `matrix.csv` — machine matrix (build log, size, icon test, signing doc)
- `matrix.md` — human matrix (AC1–AC4 summary)
- `build-*.log` — per-builder verbose logs
- `size-measurements.csv` — payload vs total vs overhead %
- `icon-tests.log` — AC2 icon + name render per tooling
- `signing-feasibility.md` — AC3 self-signed Authenticode/GPG + tamper checklist
- `ux-eval.md` — AC4 silent/GUI/location/shortcut/uninstall/privilege
- `recommendation.md` — this file (winner + next steps)
