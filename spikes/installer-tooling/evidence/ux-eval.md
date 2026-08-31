# AC4 UX Evaluation — Silent vs GUI, Location, Shortcut, Uninstall, Privilege

| Tool | Silent | Silent Flag | GUI | Choose Loc | Shortcut | Uninstall | Admin | Default Location |
|------|--------|-------------|-----|------------|----------|-----------|-------|------------------|
| NSIS | true | `/S` | true | true | true | true (`uninstall.exe generated + ARP entry (Add/Remove Programs)`) | optional | $PROGRAMFILES\<Name> (per-machine) or $LOCALAPPDATA\<Name> (per-user) |
| WiX Toolset | true | `msiexec /qn /i app.msi` | true | true | true | true (`msiexec /x {ProductCode} + ARP; native Windows Installer transactional uninstall/rollback`) | yes | ProgramFilesFolder\<Name> (requires elevation) |
| Inno Setup | true | `/SILENT or /VERYSILENT (+ /SUPPRESSMSGBOXES, /NORESTART)` | true | true | true | true (`unins000.exe + ARP entry; [UninstallDelete] section`) | optional | {autopf}\<Name> (or {localappdata}\<Name> if lowest) |
| deb (dpkg) | true | `dpkg -i / DEBIAN_FRONTEND=noninteractive apt-get install -y` | false | false | true | true (`dpkg -r <pkg> / apt remove <pkg>; prerm/postrm scripts`) | yes | /opt/<name> or /usr/share/<name> (system-wide; root required) |
| AppImage | true | `chmod +x app.AppImage && ./app.AppImage (or --appimage-extract)` | false | false | true | true (`rm app.AppImage; desktop integration removed via appimaged --remove`) | no | Anywhere (~/Applications, ~/bin, /opt) — user-chosen; no privileged install |
| Makeself | true | `./app.run -- --silent (or --target /path --noexec + manual extract)` | false | true | true | true (`rm -rf <install-dir> + rm ~/.local/share/applications/<name>.desktop (startup script provides --uninstall)`) | no | ~/.local/<name> (per-user, no root) or /opt/<name> (if run as root) |

## Detail per Tooling

### NSIS

- **Silent**: true — `/S`
- **GUI**: true
- **Pilih lokasi**: true
- **Shortcut**: true
- **Uninstall**: true — uninstall.exe generated + ARP entry (Add/Remove Programs)
- **Admin privilege**: optional — default `$PROGRAMFILES\<Name> (per-machine) or $LOCALAPPDATA\<Name> (per-user)`
- **Notes**: Modern UI 2; per-user fallback avoids UAC; mature community.

### WiX Toolset

- **Silent**: true — `msiexec /qn /i app.msi`
- **GUI**: true
- **Pilih lokasi**: true
- **Shortcut**: true
- **Uninstall**: true — msiexec /x {ProductCode} + ARP; native Windows Installer transactional uninstall/rollback
- **Admin privilege**: yes — default `ProgramFilesFolder\<Name> (requires elevation)`
- **Notes**: Best for enterprise GPO deployment; steep learning curve (XML/heat); deterministic GUIDs needed.

### Inno Setup

- **Silent**: true — `/SILENT or /VERYSILENT (+ /SUPPRESSMSGBOXES, /NORESTART)`
- **GUI**: true
- **Pilih lokasi**: true
- **Shortcut**: true
- **Uninstall**: true — unins000.exe + ARP entry; [UninstallDelete] section
- **Admin privilege**: optional — default `{autopf}\<Name> (or {localappdata}\<Name> if lowest)`
- **Notes**: Simplest scripting ([Setup]/[Files]/[Icons]); less flexible than NSIS for complex logic.

### deb (dpkg)

- **Silent**: true — `dpkg -i / DEBIAN_FRONTEND=noninteractive apt-get install -y`
- **GUI**: false
- **Pilih lokasi**: false
- **Shortcut**: true
- **Uninstall**: true — dpkg -r <pkg> / apt remove <pkg>; prerm/postrm scripts
- **Admin privilege**: yes — default `/opt/<name> or /usr/share/<name> (system-wide; root required)`
- **Notes**: Best for apt repo distribution; not portable across distros without repo; no GUI location chooser.

### AppImage

- **Silent**: true — `chmod +x app.AppImage && ./app.AppImage (or --appimage-extract)`
- **GUI**: false
- **Pilih lokasi**: false
- **Shortcut**: true
- **Uninstall**: true — rm app.AppImage; desktop integration removed via appimaged --remove
- **Admin privilege**: no — default `Anywhere (~/Applications, ~/bin, /opt) — user-chosen; no privileged install`
- **Notes**: Zero-install portable; heavy runtime overhead; update via AppImageUpdate; not ideal for system-wide deployment.

### Makeself

- **Silent**: true — `./app.run -- --silent (or --target /path --noexec + manual extract)`
- **GUI**: false
- **Pilih lokasi**: true
- **Shortcut**: true
- **Uninstall**: true — rm -rf <install-dir> + rm ~/.local/share/applications/<name>.desktop (startup script provides --uninstall)
- **Admin privilege**: no — default `~/.local/<name> (per-user, no root) or /opt/<name> (if run as root)`
- **Notes**: Lowest overhead; best for single-binary distribution; no package manager integration; user must trust shell script.

## UX Ranking (MVP lens)

- **Most flexible location (no admin)**: Makeself, AppImage (per-user `~`), NSIS/Inno per-user fallback.
- **Best silent for CI/automation**: NSIS (`/S`), deb (`DEBIAN_FRONTEND=noninteractive`), Makeself (`-- --silent`), WiX (`/qn`).
- **Best native uninstall**: WiX (transactional MSI), deb (apt), NSIS/Inno (ARP + uninstaller).
- **Worst for location choice**: deb, AppImage (fixed or user-placed file).
