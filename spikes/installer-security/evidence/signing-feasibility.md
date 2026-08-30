# Code Signing Feasibility (out-of-MVP, documented)

## Windows (NSIS .exe)
- Tool: signtool.exe / osslsigncode on Linux CI
- Cert: EV Code Signing (HSM-backed) + RFC3161 timestamp
- Command: signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f cert.pfx installer.exe
- Verify: signtool verify /pa installer.exe or Get-AuthenticodeSignature

## Linux (Makeself .run)
- Sign whole file: gpg --detach-sign --armor installer.run -> installer.run.asc
- Verify offline: gpg --verify installer.run.asc installer.run

## Recommendation
- MVP: identity-from-content sha256 via artifact.ComputeChecksum = content-addressable integrity; tamper detection without PKI.
- Signing adds non-repudiation + OS trust dialogs but requires HSM, rotation, CI secrets.
- Ship MVP with VerifyBeforeExtract FAIL-closed + payload integrity binding; add signing when HSM cert available.
