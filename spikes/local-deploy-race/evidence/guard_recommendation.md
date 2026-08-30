# Guard Recommendation — Local vs CI Deploy (Input untuk ADR, diperbarui dari spike race)

## Context
Spike local-deploy-race membuktikan runtime.OperationLock flock mencegah dual-active saat local dan CI deploy bersamaan ke target yang sama. Satu pemenang deterministik, yang kalah ditolak dengan error jelas, state /.anvil tidak corrupt, retry tidak duplicate. Implementasi sto:local-deploy-guard menindaklanjuti rekomendasi ini dengan guard per-env, audit HMAC hash-chain 0600, dan binding SSH principal.

## Rekomendasi Guard (final, sudah diimplementasikan di internal/deployment/guard.go + audit.go)

### 1. Dev Environment (allow local)
- anvil deploy --target dev boleh dari local tanpa gate tambahan — CheckDeployGuard WithDryRun(dev) PASS tanpa --confirm.
- Rationale: dev butuh iterasi cepat, risiko overwrite rendah, rollback first-class tetap tersedia.
- Guard minimal: warning log "deploying from local to dev" + audit trail (user=SSH principal, host, artifact_id) — audit JSON-lines HMAC hash-chain mode 0600.
- Tidak perlu confirm prompt di dev, tapi tetap enforce locking (flock) agar concurrent local+CI tidak race.
- Test: `go test -run TestGuard_DevAllow` — dev tanpa confirm PASS.

### 2. Staging Environment (soft gate)
- anvil deploy --target staging boleh dari local atau CI, tapi harus eksplisit flag --confirm.
- Pesan error staging tanpa --confirm: `deploy to "staging" requires --confirm: staging is protected, re-run with --confirm: anvil deploy --target staging --confirm`.
- Dry-run (`--dry-run`) adalah verification-only dan tidak butuh --confirm (konsisten dengan spike UX).
- Tidak ada allowlist tambahan untuk staging; hanya --confirm. Env var ANVIL_ALLOW_LOCAL_STAGING=1 opsional diabaikan (explicit --confirm lebih traceable).
- Test: `go test -run TestGuard_StagingRequiresConfirm` — staging tanpa --confirm REJECT, plus --confirm PASS.

### 3. Prod Environment (require allowlist + confirm prompt) — CI-only default
- anvil deploy --target prod hanya via CI secara default. Local langsung ditolak dengan pesan CI-only: `prod deploy from local rejected: CI-only default — allowlist required. Add your SSH principal (user) to server.targets.prod.allowlist in anvil.yaml or run via CI`.
- Deteksi CI: env CI, GITHUB_ACTIONS, GITLAB_CI, CIRCLECI, JENKINS_URL, TEAMCITY_VERSION, TF_BUILD, BITBUCKET_COMMIT, ANVIL_CI — salah satu true => CI mode.
- Allowlist: anvil.yaml `server.targets[prod].allowlist: ["deploy", "deploy@prod.example.com", "SHA256:xxx", "*"]` atau boolean `server.targets[prod].allowLocal: true`, atau env `ANVIL_PROD_ALLOW_LOCAL=true`. Principal binding ke SSH authenticated principal (creds.User, lowercased, atau user@host), BUKAN string DeployUser spoofable — audit DeployUser diisi dari SSH principal, bukan arg CLI.
- Jika allowlist terpenuhi, tetap require `--confirm` + interactive confirm prompt: `Confirm deploy local artifact to prod? (yes/no):` — timeout 30s default no. Non-interactive (CI) bypass prompt tapi tetap audit + locking. Framework-free (no server.targets) legacy compat: skip prompt, hanya butuh --confirm.
- Audit: prod deploy log harus capture deployer identity (SSH principal / CI job) via AuditLogger HMAC — entry field `user` adalah SSH principal, bukan spoofable string. Redaction via output.SanitizeLogLine / RedactSecrets (tidak leak DEPLOY_SSH_KEY, private key, full path). File `audit.log` JSON-lines mode 0600, key `audit.hmac.key` 0600, hash-chain HMAC-SHA256 prev_hash -> hash.
- Test: `go test -run TestGuard_ProdAllowlistEnforcement` — prod non-CI tanpa allowlist REJECT bahkan dengan --confirm; prod dengan allowlist + --confirm + stdin yes PASS; prod CI tanpa allowlist dengan --confirm PASS; prod prompt timeout / "no" REJECT.

### 4. Mekanisme Teknis (sudah diimplementasikan)
- Enforce via CheckDeployGuard single entry point — tidak ada RBAC bypass (AC4). Semua path `anvil deploy` memanggil guard; no string DeployUser bypass, hanya SSH principal.
- Locking tetap via ServerReleaseCoordinator.Install/Activate dengan runtime.OperationLock flock — tidak perlu perubahan, guard hanya pre-flight.
- Idempotency: Install sudah reject duplicate artifact_id already installed — retry aman.
- State dumps (runtime-state.json + .anvil/state/releases/*.json) tetap authoritative; observability anvil status query via server.QueryLifecycleStatus (read-only, ADR-036).
- Audit: internal/deployment/audit.go — NewAuditLogger(<installRoot>) menulis JSON-lines O_APPEND|O_SYNC 0600, key per-installRoot audit.hmac.key 0600, HMAC chain verified via VerifyChain(). Redacted via output.RedactSecrets/SanitizeLogLine. DeployUser binding via creds.User.
- Config: internal/config/server.go — ServerTarget.Allowlist []string + AllowLocal bool, parse dari flat keys allowlist/allowList/allowLocalDeploys/allow_local_deploys dan allowLocal/allow_local/allowLocalDeploy (boolean). Framework-free (no targets) dianggap allowlist legacy untuk prod — tidak break existing tests.

### 5. Evidence Spike (tetap valid) + Implementasi
- Concurrent run logs + state dumps before/after + lock behavior proof ada di spikes/local-deploy-race/evidence/race.log dan summary.json — tetap valid, tidak diubah.
- Lock file <installRoot>/runtime-state.lock mode 0600, holder record di runtime-state.json.operation_lock — tetap.
- Audit baru: audit.log mode 0600, chain HMAC, redacted — bukti di `go test -run TestAudit_HMACChain` dan `TestAudit_Redacted`.
- Evidence harness: spikes/local-deploy-race/harness.go BuildGuardRecommendation() sekarang merefer implementasi final; file ini diperbarui sebagai bukti AC3.

## Decision Input untuk ADR (resolved)
- Opsi A (rekomendasi — dipilih & diimplementasikan): dev allow local, prod require allowlist+confirm (hybrid C dari FND), staging --confirm soft gate, audit HMAC + SSH binding.
- Opsi B: preview-only channel (local hanya ke dev/staging, prod CI-only tanpa allowlist) — ditolak, allowlist memberi kompromi hybrid yang sama dengan keamanan lebih halus.
- ADR baru adr:local-deploy-transport §4 Guard per-env sudah mencakup keputusan ini (dev allow, staging --confirm, prod allowlist+confirm+CI-only). Guard ini implementasinya.

## Verifikasi AC
- AC1: dev deploy tanpa confirm PASS, staging tanpa --confirm REJECT, prod allowlist enforcement — `go test ./internal/deployment -run Guard` + `go test ./cmd -run ProtectedRequiresConfirm` + `go test ./internal/config -run Allowlist`.
- AC2: audit JSON-lines 0600 HMAC chain, redacted — `go test ./internal/deployment -run Audit` + manual check `stat -c %a audit.log` = 600, `audit.hmac.key` 600.
- AC3: guard_recommendation.md diperbarui dari spike race — file ini.
- AC4: no RBAC bypass — guard single entry point CheckDeployGuard, binding ke SSH principal (creds.User), tidak ada role string bypass; audit user = SSH principal.

## Referensi Implementasi
- internal/deployment/guard.go — ClassifyEnv, IsCI, CheckDeployGuard, promptProdConfirm, isProdAllowlisted (SSH binding).
- internal/deployment/audit.go — AuditLogger HMAC chain 0600, redacted.
- internal/config/server.go — ServerTarget allowlist parsing.
- cmd/deploy.go — wire guard + audit (deny/allow), redacted error + redaction.
