# Spike 4 — Security Verification & Tamper Detection (`anvil-cli/spk:installer-security:1`)

**Timebox 3 hari — parallel**

## Objective
Validate checksum verification sebelum extract, installer payload integrity, secret redaction, offline install.

## AC mapping

| AC | Requirement | Implementation | Evidence |
|---|---|---|---|
| AC1 | Installer verify manifest checksum sha256 sebelum extract — tampered bit-flip ditolak dengan guidance actionable | `verifier.go:VerifyBeforeExtract` wraps `internal/artifact.VerifyArtifact` (6 checks) FAIL-closed, no extract on fail | `evidence/tamper.log` + `harness.log` |
| AC2 | Installer payload integrity: installer binary checksum vs embedded manifest, detect repack tampering | `VerifyInstallerPayloadIntegrity` compares `FileSHA256(installer)` binding + `VerifyArtifact(embedded)` identity-from-content | `evidence/payload-integrity.log` |
| AC3 | Secret redaction: DB credentials / env tidak leak di log | `RedactInstallerLog` = `output.RedactSecrets` + `SanitizeLogLine` + DB env values | `evidence/redaction.log` |
| AC4 | Offline verification: tanpa internet tetap verify & install | `VerifyOffline` = filesystem-only `VerifyArtifact` (os.Open + gzip + tar + sha256), no `net/http` | `evidence/offline.log` |

## Threat model (summary)

- **Assets**: `artifact.tar.gz` (manifest + deployable content), installer wrapper `.run/.exe`, DB creds from prompt.
- **Trust boundary**: build host (trusted) → artifact file (untrusted transport) → target host (verify before extract).
- **T1 Tampered artifact**: bit-flip / truncated tar → checksum mismatch → AC1.
- **T2 Repacked installer**: attacker swaps payload → AC2 binding mismatch.
- **T3 Secret leak**: log sinks → AC3 redaction.
- **T4 Offline/registry poisoning**: installer must not phone home to bypass verify → AC4.
- **T5 Path traversal**: `safeExtractPath` already in `artifact.VerifyArtifact`.

Mitigations reuse `internal/artifact.VerifyArtifact`, `ComputeChecksum` (identity-from-content sha256), `internal/output.RedactSecrets/SanitizeLogLine`.

## Run

```bash
go run ./spikes/installer-security/cmd/spike --repo . --evidence spikes/installer-security/evidence --out /tmp/anvil-installer-sec
go test ./spikes/installer-security -v -count=1
```

Evidence after run: `tamper.log`, `payload-integrity.log`, `redaction.log`, `offline.log`, `signing-feasibility.md`, `summary.json`, `matrix.md`, `harness.log`.

## Code signing feasibility (out-of-MVP)

See `evidence/signing-feasibility.md` (also `verifier.go:SigningFeasibility()`):

- **Windows**: `signtool` / `osslsigncode`, EV cert + RFC3161 timestamp, `signtool verify /pa`.
- **Linux Makeself**: `gpg --detach-sign --armor installer.run` + `gpg --verify` offline; minisign alternative.
- **Recommendation**: MVP ships checksum-gated FAIL-closed + payload binding; signing added when HSM cert available — do not block MVP on cert procurement.

## Constraints satisfied

- Reuses `internal/artifact.VerifyArtifact`, `internal/output.RedactSecrets/SanitizeLogLine`.
- Identity-from-content checksum (`artifact.ComputeChecksum` sha256 sorted relPath+content).
- Follows spikes evidence pattern (tamper logs, redaction checks, summary.json).
- Does NOT modify EKA.

## Next steps

- Wire `VerifyBeforeExtract` into real `anvil installer build` / install path (pre-extract gate).
- Persist `installer.run.checksum.json` binding at build time.
- Gate logs through `RedactInstallerLog` everywhere installer prompt collects DB env.
- Add signing CI job when HSM cert available.
