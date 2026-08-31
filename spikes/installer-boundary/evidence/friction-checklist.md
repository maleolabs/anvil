# Friction Checklist — Installer Boundary (auto-generated)

## Before installer (manual)
- scp / git clone to server
- edit .env (DB_HOST, APP_KEY)
- composer install
- php artisan migrate --force   (manual, error prone)
- php artisan db:seed           (super-admin manual)
- php artisan storage:link      (symlink manual, Windows needs admin)
- fix perms / chown
- diverged Windows (NSIS) vs Linux (Makeself) steps
- Steps: 7+ manual, no idempotency, no rollback

## After dumb-wrapper + standard-owned setup
- user runs installer (zip/shell) -> chooses lokasi
- installer extracts payload/artifact.tar.gz (verified checksum identity-from-content)
- installer triggers: anvil standard setup --install-root <lokasi>
- standard hook owns: migrate --force, db:seed (super-admin), storage:link
- idempotent: second run detects .anvil-install-state.json -> noop verify
- cancel mid-extract: staged tmp removed, no corrupt, retry safe
- migrate fail: rollback artifact + actionable error (check DB_HOST)

## Result (this run)
- artifact_id: 92f634fb0a75d5c12921961b7aeebf3d3f61eb0af9f1d73fc681362c455b1130
- installer: /tmp/spike-installer-out-1193667/anvil-installer-linux-makeself-1.0.0.zip (linux-makeself)
- installRoot: /tmp/spike-installer-install-1193667
- idempotent: true
- cancel recovered: true
- migrate rollback: true
