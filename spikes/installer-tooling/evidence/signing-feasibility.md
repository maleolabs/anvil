# AC3 Signing Feasibility — Windows Authenticode & Linux GPG/deb

> No real cert needed — spike uses dummy/self-signed only. Do NOT commit private keys.

## Summary

| Tool | OS | Method | Feasible on CI | Verify |
|------|----|--------|----------------|--------|
| NSIS | windows | Authenticode via signtool.exe / osslsigncode | true | `osslsigncode verify app-signed.exe  OR  signtool verify /pa app-signed.exe` |
| WiX Toolset | windows | Authenticode on .msi via signtool / osslsigncode (same as EXE) | true | `osslsigncode verify app-signed.msi; signtool verify /pa app-signed.msi` |
| Inno Setup | windows | Authenticode via signtool/osslsigncode on resulting .exe (SignedUninstaller optional) | true | `osslsigncode verify app-signed.exe` |
| deb (dpkg) | linux | GPG detached (.dsc) + dpkg-sig + apt SecureApt (Release.gpg / InRelease) | true | `dpkg-sig --verify app.deb; gpg --verify InRelease; debsig-verify --policy generic app.deb` |
| AppImage | linux | GPG detached .sig + optional embedded update info (AppImageUpdate signature) | true | `gpg --verify app.AppImage.sig app.AppImage; AppImageUpdate --verify-signature (if embedded)` |
| Makeself | linux | GPG detached .sig + SHA256 checksum file (same directory) | true | `gpg --verify app.run.sig app.run && sha256sum -c app.run.sha256; gpg --verify app.run.sha256.asc` |

## Per-Tool Detail

### NSIS (nsis)

- **Method**: Authenticode via signtool.exe / osslsigncode
- **Self-signed (spike)**: `openssl req -x509 -newkey rsa:2048 -keyout dummy.key -out dummy.crt -days 1 -nodes -subj "/CN=Anvil Spike" && osslsigncode sign -certs dummy.crt -key dummy.key -in app.exe -out app-signed.exe`
- **Verify**: `osslsigncode verify app-signed.exe  OR  signtool verify /pa app-signed.exe`
- **Tamper detect**: Authenticode digest (SHA256) embedded; Windows SmartScreen warns on tamper; verify fails if binary modified after signing.
- **CI feasible**: true — Self-signed cert triggers SmartScreen on real Windows unless cert installed to Trusted Root; spike uses dummy cert only.

### WiX Toolset (wix)

- **Method**: Authenticode on .msi via signtool / osslsigncode (same as EXE)
- **Self-signed (spike)**: `osslsigncode sign -certs dummy.crt -key dummy.key -in app.msi -out app-signed.msi && msiexec /a app-signed.msi /qb TARGETDIR=C:\temp\verify`
- **Verify**: `osslsigncode verify app-signed.msi; signtool verify /pa app-signed.msi`
- **Tamper detect**: MSI Authenticode covers entire MSI stream; tamper breaks signature; MsiVerifyPackage would fail.
- **CI feasible**: true — Self-signed same SmartScreen caveat as NSIS; WiX msi benefits from EV cert for enterprise trust.

### Inno Setup (inno)

- **Method**: Authenticode via signtool/osslsigncode on resulting .exe (SignedUninstaller optional)
- **Self-signed (spike)**: `osslsigncode sign -certs dummy.crt -key dummy.key -in app.exe -out app-signed.exe`
- **Verify**: `osslsigncode verify app-signed.exe`
- **Tamper detect**: Same Authenticode digest as NSIS; Inno SignedUninstaller adds inner signature for uninstaller.
- **CI feasible**: true — Inno can sign uninstaller separately via [Setup] SignedUninstaller=yes + SignTool directive.

### deb (dpkg) (deb)

- **Method**: GPG detached (.dsc) + dpkg-sig + apt SecureApt (Release.gpg / InRelease)
- **Self-signed (spike)**: `gpg --batch --gen-key dummy-gen && dpkg-sig -k <keyID> --sign builder app.deb && ar t app.deb # verify _gpgbuilder; apt-ftparchive release . > Release && gpg --clearsign -o InRelease Release`
- **Verify**: `dpkg-sig --verify app.deb; gpg --verify InRelease; debsig-verify --policy generic app.deb`
- **Tamper detect**: dpkg-sig embeds GPG signature in ar member _gpgbuilder; tamper breaks signature; apt SecureApt validates Release/InRelease checksums (SHA256) for entire repo.
- **CI feasible**: true — Self-signed GPG key must be distributed via apt-key/keyring package; spike uses throwaway key (no real distribution).

### AppImage (appimage)

- **Method**: GPG detached .sig + optional embedded update info (AppImageUpdate signature)
- **Self-signed (spike)**: `gpg --detach-sign app.AppImage  # produces app.AppImage.sig; appimagetool --sign app.AppImage (if key configured)`
- **Verify**: `gpg --verify app.AppImage.sig app.AppImage; AppImageUpdate --verify-signature (if embedded)`
- **Tamper detect**: Detached GPG sig covers whole AppImage; tamper fails gpg --verify; no built-in OS enforcement like Authenticode.
- **CI feasible**: true — Self-signed same caveat as deb; detached sig must be distributed alongside AppImage; no central trust store.

### Makeself (makeself)

- **Method**: GPG detached .sig + SHA256 checksum file (same directory)
- **Self-signed (spike)**: `gpg --detach-sign app.run && sha256sum app.run > app.run.sha256 && gpg --clearsign app.run.sha256`
- **Verify**: `gpg --verify app.run.sig app.run && sha256sum -c app.run.sha256; gpg --verify app.run.sha256.asc`
- **Tamper detect**: Detached GPG sig OR checksum mismatch detects tamper; no OS-level enforcement — user must verify manually before chmod +x.
- **CI feasible**: true — Self-signed same distribution caveat; Makeself --gpg-extra can embed GPG check in extractor; spike uses detached sig.

## Tamper Detection Checklist (all toolings)

- [ ] Signature embedded/detached present (`osslsigncode verify`, `gpg --verify`, `dpkg-sig --verify`).
- [ ] Digest covers entire payload — modifying 1 byte after signing must fail verification.
- [ ] Certificate/key provenance documented (self-signed spike throws SmartScreen/apt warning — production needs CA/EV or repo keyring).
- [ ] Installer refuses tampered artifact (Windows SmartScreen / apt SecureApt / manual `sha256sum -c`).
- [ ] No private key material in logs or repo (CI secret via `ANVIL_SIGNING_KEY` env, redacted).
- [ ] Rotation plan: cert expiry ≤1y, GPG key ≤2y, re-sign on release.

## Self-Signed Dummy Commands (spike-only)

```bash
# Windows — generate dummy cert (1 day) & sign
openssl req -x509 -newkey rsa:2048 -keyout /tmp/dummy.key -out /tmp/dummy.crt -days 1 -nodes -subj "/CN=Anvil Spike"
osslsigncode sign -certs /tmp/dummy.crt -key /tmp/dummy.key -in app.exe -out app-signed.exe
osslsigncode verify app-signed.exe

# Linux deb — throwaway GPG key & sign
cat > /tmp/gpg-batch <<'EOF'
%no-protection
Key-Type: RSA
Key-Length: 2048
Subkey-Type: RSA
Subkey-Length: 2048
Name-Real: Anvil Spike
Name-Email: spike@anvil.test
Expire-Date: 1d
EOF
gpg --batch --gen-key /tmp/gpg-batch
dpkg-sig -k <keyID> --sign builder app.deb && dpkg-sig --verify app.deb

# Makeself/AppImage — detached sig
gpg --detach-sign app.run && gpg --verify app.run.sig app.run
sha256sum app.run > app.run.sha256 && gpg --clearsign app.run.sha256
```
