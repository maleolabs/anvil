# Spike 4 -- Security Verification & Tamper Detection Matrix (AC1-4)

| AC | Gate | Result | Evidence |
|---|---|---|---|
| AC1 | VerifyBeforeExtract (sha256 identity-from-content) tamper bit-flip rejected with actionable guidance | true | tamper.log |
| AC1 | identity checksum cfeae3c7a23063d0 |
| AC2 | Installer payload integrity (installer SHA vs embedded manifest) | true | payload-integrity.log |
| AC2 | Repack tampering detected | true | payload-integrity.log |
| AC3 | Secret redaction (RedactSecrets/SanitizeLogLine, DB creds not leak) | true (6 samples) | redaction.log |
| AC4 | Offline verification (no registry HTTP, filesystem-only) | true (noRegistry=true) | offline.log |
| Signing | Windows Authenticode + Linux gpg feasibility documented (out-of-MVP) | done | signing-feasibility.md |
