# GitHub Actions Integration

This guide covers integrating Anvil with GitHub Actions for CI and CD workflows.

## Prerequisites

- GitHub repository with Anvil project (`anvil.yaml` in root)
- GitHub Actions enabled on the repository
- Target server configured for Anvil deployment (for CD workflows)

## Basic CI workflow

Runs on every push and pull request. Executes quality gates and builds the application.

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  ci:
    runs-on: ubuntu-latest

    steps:
      # Check out the repository
      - name: Checkout
        uses: actions/checkout@v4

      # Install Anvil CLI
      - name: Install Anvil
        run: curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

      # Run quality gates (lint, static analysis, tests)
      - name: Run CI pipeline
        run: anvil pipeline ci

      # Build the application
      - name: Build
        run: anvil pipeline build

      # Package into an immutable artifact
      - name: Package artifact
        run: anvil artifact package

      # Upload artifact as GitHub Actions artifact for later use
      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: anvil-artifact
          path: .anvil/artifacts/*.tar.gz
          retention-days: 7
```

## Release workflow

Triggered when a GitHub release is published. Builds, packages, and deploys to a target server.

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  release:
    types: [published]

jobs:
  deploy:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install Anvil
        run: curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

      # Build the application
      - name: Build
        run: anvil pipeline build --env production

      # Package into an immutable artifact
      - name: Package artifact
        run: anvil artifact package

      # Verify artifact integrity before deployment
      - name: Verify artifact
        run: anvil artifact verify .anvil/artifacts/*.tar.gz

      # Upload artifact to target server
      - name: Upload to server
        run: anvil deployment upload production .anvil/artifacts/*.tar.gz
        env:
          ANVIL_SERVER_HOST: ${{ secrets.ANVIL_SERVER_HOST }}
          ANVIL_SERVER_USER: ${{ secrets.ANVIL_SERVER_USER }}
          ANVIL_SSH_KEY: ${{ secrets.ANVIL_SSH_KEY }}

      # Install artifact on target server
      - name: Install on server
        run: anvil deployment install production .anvil/artifacts/*.tar.gz

      # Activate the release
      - name: Activate release
        run: anvil deployment activate production my-project ${{ github.sha }}
```

## Laravel-specific example

Laravel projects require PHP, Composer, and Node.js on the CI runner.

```yaml
# .github/workflows/laravel-ci.yml
name: Laravel CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  ci:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      # Set up PHP with required extensions
      - name: Setup PHP
        uses: shivammathur/setup-php@v2
        with:
          php-version: '8.2'
          extensions: dom, curl, libxml, mbstring, zip, pcntl, pdo, sqlite, pdo_sqlite
          coverage: none

      # Set up Node.js for asset compilation
      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'

      # Install Anvil CLI
      - name: Install Anvil
        run: curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

      # Run CI pipeline (lint, tests)
      - name: Run CI pipeline
        run: anvil pipeline ci

      # Build (Composer install, npm install, npm run build, artisan caches)
      - name: Build
        run: anvil pipeline build

      # Package artifact
      - name: Package artifact
        run: anvil artifact package
```

The generated `.anvil/pipelines/build.yaml` for Laravel runs: Composer install --> npm install --> npm run build --> artisan cache commands. See [Laravel build pipeline](../adapters/laravel/build.md) for details.

## Flutter-specific example

Flutter projects require the Flutter SDK on the CI runner.

```yaml
# .github/workflows/flutter-ci.yml
name: Flutter CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  ci:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      # Set up Flutter SDK
      - name: Setup Flutter
        uses: subosito/flutter-action@v2
        with:
          channel: 'stable'
          cache: true

      # Install Anvil CLI
      - name: Install Anvil
        run: curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

      # Run CI pipeline
      - name: Run CI pipeline
        run: anvil pipeline ci

      # Build Flutter application
      - name: Build
        run: anvil pipeline build

      # Package artifact
      - name: Package artifact
        run: anvil artifact package
```

## Environment variables and secrets

Configure these in your GitHub repository settings under **Settings > Secrets and variables > Actions**.

### Repository secrets

| Secret | Description |
|---|---|
| `ANVIL_SERVER_HOST` | Target server hostname or IP |
| `ANVIL_SERVER_USER` | SSH user for deployment |
| `ANVIL_SSH_KEY` | Private SSH key for authentication |
| `ANVIL_DEPLOY_TARGET` | Anvil target identifier (e.g., `production`, `staging`) |

### Repository variables

| Variable | Description |
|---|---|
| `ANVIL_VERSION` | Pin a specific Anvil version (optional) |
| `ANVIL_PROJECT_ID` | Project identifier for deployment |

### Using secrets in workflows

```yaml
- name: Deploy
  run: anvil deployment upload ${{ vars.ANVIL_DEPLOY_TARGET }} .anvil/artifacts/*.tar.gz
  env:
    ANVIL_SERVER_HOST: ${{ secrets.ANVIL_SERVER_HOST }}
    ANVIL_SERVER_USER: ${{ secrets.ANVIL_SERVER_USER }}
    ANVIL_SSH_KEY: ${{ secrets.ANVIL_SSH_KEY }}
```

## Caching dependencies

Speed up CI runs by caching Composer and npm dependencies.

```yaml
# Cache Composer dependencies
- name: Cache Composer
  uses: actions/cache@v4
  with:
    path: vendor
    key: ${{ runner.os }}-composer-${{ hashFiles('**/composer.lock') }}
    restore-keys: ${{ runner.os }}-composer-

# Cache npm dependencies
- name: Cache npm
  uses: actions/cache@v4
  with:
    path: node_modules
    key: ${{ runner.os }}-npm-${{ hashFiles('**/package-lock.json') }}
    restore-keys: ${{ runner.os }}-npm-
```

## Notes

- Anvil installs to `/usr/local/bin/anvil` and is available immediately after the install step.
- The install script is idempotent — safe to run multiple times.
- For pinned versions, replace `latest/download` with `download/vX.Y.Z` in the install URL.
- Artifacts in `.anvil/artifacts/` are immutable and self-contained. The embedded `manifest.json` is the authoritative identity — filenames are not.
- See [GitHub Actions documentation](https://docs.github.com/en/actions) for advanced workflow features.

## See also

- [CI/CD overview](README.md)
- [GitLab CI](gitlab-ci.md)
- [Jenkins](jenkins.md)
- [Laravel adapter](../adapters/laravel/)
