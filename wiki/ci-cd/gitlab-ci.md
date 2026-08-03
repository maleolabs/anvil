# GitLab CI Integration

This guide covers integrating Anvil with GitLab CI/CD for CI and CD workflows.

## Prerequisites

- GitLab repository with Anvil project (`anvil.yaml` in root)
- GitLab CI/CD enabled (`.gitlab-ci.yml` in repository root)
- Target server configured for Anvil deployment (for CD workflows)

## Basic `.gitlab-ci.yml`

Defines three stages: build, test, and deploy.

```yaml
# .gitlab-ci.yml
stages:
  - build
  - test
  - deploy

# Default image and before_script for all jobs
default:
  image: ubuntu:latest
  before_script:
    # Install Anvil CLI in every job
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

# Build stage: compile the application and package artifact
build:
  stage: build
  script:
    - anvil pipeline build
    - anvil artifact package
  artifacts:
    paths:
      - .anvil/artifacts/
    expire_in: 7 days

# Test stage: run quality gates
test:
  stage: test
  script:
    - anvil pipeline ci

# Deploy stage: upload and activate on target server
deploy:
  stage: deploy
  script:
    - anvil artifact verify .anvil/artifacts/*.tar.gz
    - anvil deployment upload $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment install $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment activate $ANVIL_DEPLOY_TARGET $ANVIL_PROJECT_ID $CI_COMMIT_SHA
  environment:
    name: production
  only:
    - main
  when: manual
```

## Stage definitions

| Stage | Purpose | Anvil commands |
|---|---|---|
| **build** | Compile application, package artifact | `anvil pipeline build`, `anvil artifact package` |
| **test** | Run quality gates (lint, static analysis, tests) | `anvil pipeline ci` |
| **deploy** | Verify, upload, install, and activate on server | `anvil artifact verify`, `anvil deployment upload/install/activate` |

Artifacts from the `build` stage are passed to `deploy` via GitLab CI artifacts.

## Laravel-specific example

Laravel projects require PHP, Composer, and Node.js.

```yaml
# .gitlab-ci.yml
stages:
  - build
  - test
  - deploy

default:
  before_script:
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

# Build stage with Laravel dependencies
build:
  stage: build
  image: php:8.2-cli
  before_script:
    # Install system dependencies
    - apt-get update && apt-get install -y git unzip libzip-dev
    # Install PHP extensions
    - docker-php-ext-install zip pdo pdo_mysql
    # Install Composer
    - curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer
    # Install Node.js
    - apt-get install -y nodejs npm
    # Install Anvil
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
  script:
    - anvil pipeline build
    - anvil artifact package
  cache:
    key:
      files:
        - composer.lock
        - package-lock.json
    paths:
      - vendor/
      - node_modules/
  artifacts:
    paths:
      - .anvil/artifacts/
    expire_in: 7 days

# Test stage
test:
  stage: test
  image: php:8.2-cli
  before_script:
    - apt-get update && apt-get install -y git unzip libzip-dev
    - docker-php-ext-install zip pdo pdo_mysql
    - curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer
    - apt-get install -y nodejs npm
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
  script:
    - anvil pipeline ci

# Deploy stage
deploy:
  stage: deploy
  image: ubuntu:latest
  script:
    - anvil artifact verify .anvil/artifacts/*.tar.gz
    - anvil deployment upload $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment install $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment activate $ANVIL_DEPLOY_TARGET $ANVIL_PROJECT_ID $CI_COMMIT_SHA
  environment:
    name: production
  only:
    - main
  when: manual
```

## Flutter-specific example

Flutter projects require the Flutter SDK.

```yaml
# .gitlab-ci.yml
stages:
  - build
  - test
  - deploy

default:
  before_script:
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

# Build stage with Flutter
build:
  stage: build
  image: ghcr.io/cirruslabs/flutter:stable
  before_script:
    # Install Anvil
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
  script:
    - flutter pub get
    - anvil pipeline build
    - anvil artifact package
  cache:
    key:
      files:
        - pubspec.lock
    paths:
      - .dart_tool/
      - .pub-cache/
  artifacts:
    paths:
      - .anvil/artifacts/
    expire_in: 7 days

# Test stage
test:
  stage: test
  image: ghcr.io/cirruslabs/flutter:stable
  before_script:
    - curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
  script:
    - flutter pub get
    - anvil pipeline ci

# Deploy stage
deploy:
  stage: deploy
  image: ubuntu:latest
  script:
    - anvil artifact verify .anvil/artifacts/*.tar.gz
    - anvil deployment upload $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment install $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
    - anvil deployment activate $ANVIL_DEPLOY_TARGET $ANVIL_PROJECT_ID $CI_COMMIT_SHA
  environment:
    name: production
  only:
    - main
  when: manual
```

## Variables and secrets

Configure these in your GitLab project under **Settings > CI/CD > Variables**.

### CI/CD variables

| Variable | Description | Type |
|---|---|---|
| `ANVIL_SERVER_HOST` | Target server hostname or IP | Variable (masked) |
| `ANVIL_SERVER_USER` | SSH user for deployment | Variable (masked) |
| `ANVIL_SSH_KEY` | Private SSH key for authentication | File (protected) |
| `ANVIL_DEPLOY_TARGET` | Anvil target identifier (e.g., `production`) | Variable |
| `ANVIL_PROJECT_ID` | Project identifier for deployment | Variable |

### Built-in GitLab CI variables

These are available automatically:

| Variable | Description |
|---|---|
| `CI_COMMIT_SHA` | Commit SHA — useful as release ID |
| `CI_COMMIT_REF_NAME` | Branch or tag name |
| `CI_PIPELINE_ID` | Pipeline ID |
| `CI_ENVIRONMENT_NAME` | Current environment name |

### Example usage

```yaml
deploy:
  script:
    - anvil deployment activate $ANVIL_DEPLOY_TARGET $ANVIL_PROJECT_ID $CI_COMMIT_SHA
  environment:
    name: $CI_ENVIRONMENT_NAME
```

## Notes

- Anvil installs to `/usr/local/bin/anvil` and is available immediately after the install step.
- Use GitLab CI artifacts to pass the packaged artifact from `build` to `deploy` stages.
- The `deploy` job is set to `when: manual` to prevent accidental deployments. Remove this to auto-deploy on main.
- For pinned versions, replace `latest/download` with `download/vX.Y.Z` in the install URL.
- Artifacts in `.anvil/artifacts/` are immutable. The embedded `manifest.json` is the authoritative identity.
- See [GitLab CI documentation](https://docs.gitlab.com/ee/ci/) for advanced pipeline features.

## See also

- [CI/CD overview](README.md)
- [GitHub Actions](github-actions.md)
- [Jenkins](jenkins.md)
- [Laravel adapter](../adapters/laravel/)
