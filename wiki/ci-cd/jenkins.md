# Jenkins Integration

This guide covers integrating Anvil with Jenkins for CI and CD workflows.

## Prerequisites

- Jenkins server with Pipeline plugin installed
- Jenkinsfile in repository root (or configured pipeline job)
- Target server configured for Anvil deployment (for CD workflows)
- Jenkins agents with shell access

## Basic Jenkinsfile

Declarative pipeline with build, test, and deploy stages.

```groovy
// Jenkinsfile
pipeline {
    agent any

    environment {
        ANVIL_DEPLOY_TARGET = 'production'
        ANVIL_PROJECT_ID = 'my-project'
    }

    stages {
        // Install Anvil CLI
        stage('Setup') {
            steps {
                sh 'curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh'
            }
        }

        // Build the application
        stage('Build') {
            steps {
                sh 'anvil pipeline build'
            }
        }

        // Run quality gates
        stage('Test') {
            steps {
                sh 'anvil pipeline ci'
            }
        }

        // Package into immutable artifact
        stage('Package') {
            steps {
                sh 'anvil artifact package'
            }
        }

        // Deploy to target server — upload only (install/activate run on the server runtime)
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                // Install and activate run ON the server runtime (local aliases of `anvil server release ...`),
                // not on the Jenkins agent (EPIC-011 §8): CI uploads only.
            }
        }
    }

    post {
        always {
            // Clean up workspace
            cleanWs()
        }
        failure {
            echo 'Pipeline failed. Check the logs for details.'
        }
        success {
            echo 'Pipeline completed successfully.'
        }
    }
}
```

## Stage definitions

| Stage | Purpose | Anvil commands |
|---|---|---|
| **Setup** | Install Anvil CLI | `curl ... install.sh \| sh` |
| **Build** | Compile application | `anvil pipeline build` |
| **Test** | Run quality gates (lint, static analysis, tests) | `anvil pipeline ci` |
| **Package** | Create immutable artifact | `anvil artifact package` |
| **Deploy** | Verify and upload the artifact (SSH) | `anvil artifact verify`, `anvil deployment upload` |

## Laravel-specific example

Laravel projects require PHP, Composer, and Node.js on the Jenkins agent.

```groovy
// Jenkinsfile
pipeline {
    agent any

    environment {
        ANVIL_DEPLOY_TARGET = 'production'
        ANVIL_PROJECT_ID = 'my-laravel-app'
    }

    stages {
        // Install PHP, Node.js, and Anvil
        stage('Setup') {
            steps {
                // Install PHP extensions and Composer
                sh '''
                    apt-get update && apt-get install -y php8.2-cli php8.2-mbstring php8.2-xml php8.2-curl php8.2-zip unzip
                    curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer
                '''
                // Install Node.js
                sh '''
                    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
                    apt-get install -y nodejs
                '''
                // Install Anvil
                sh 'curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh'
            }
        }

        // Build: Composer install, npm install, npm run build, artisan caches
        stage('Build') {
            steps {
                sh 'anvil pipeline build'
            }
        }

        // Test: Run PHPUnit, ESLint, etc.
        stage('Test') {
            steps {
                sh 'anvil pipeline ci'
            }
        }

        // Package artifact
        stage('Package') {
            steps {
                sh 'anvil artifact package'
            }
        }

        // Deploy to production — upload only (install/activate run on the server runtime)
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                // Install and activate run ON the server runtime (local aliases of `anvil server release ...`),
                // not on the Jenkins agent (EPIC-011 §8): CI uploads only.
            }
        }
    }
}
```

## Flutter-specific example

Flutter projects require the Flutter SDK on the Jenkins agent.

```groovy
// Jenkinsfile
pipeline {
    agent any

    environment {
        ANVIL_DEPLOY_TARGET = 'production'
        ANVIL_PROJECT_ID = 'my-flutter-app'
        FLUTTER_HOME = '/opt/flutter'
    }

    stages {
        // Install Flutter SDK and Anvil
        stage('Setup') {
            steps {
                // Install Flutter SDK
                sh '''
                    git clone https://github.com/flutter/flutter.git -b stable $FLUTTER_HOME
                    export PATH="$FLUTTER_HOME/bin:$PATH"
                    flutter precache
                '''
                // Install Anvil
                sh 'curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh'
            }
        }

        // Get Flutter dependencies
        stage('Dependencies') {
            steps {
                sh 'export PATH="$FLUTTER_HOME/bin:$PATH" && flutter pub get'
            }
        }

        // Build Flutter application
        stage('Build') {
            steps {
                sh 'export PATH="$FLUTTER_HOME/bin:$PATH" && anvil pipeline build'
            }
        }

        // Run tests
        stage('Test') {
            steps {
                sh 'export PATH="$FLUTTER_HOME/bin:$PATH" && anvil pipeline ci'
            }
        }

        // Package artifact
        stage('Package') {
            steps {
                sh 'anvil artifact package'
            }
        }

        // Deploy to production — upload only (install/activate run on the server runtime)
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                // Install and activate run ON the server runtime (local aliases of `anvil server release ...`),
                // not on the Jenkins agent (EPIC-011 §8): CI uploads only.
            }
        }
    }
}
```

## Credentials configuration

Store sensitive values in Jenkins Credentials Manager.

### Adding credentials

1. Go to **Manage Jenkins > Credentials > System > Global credentials**
2. Click **Add Credentials**
3. Add the following:

| Credential ID | Type | Description |
|---|---|---|
| `anvil-server-host` | Secret text | Target server hostname or IP |
| `anvil-server-user` | Secret text | SSH user for deployment |
| `anvil-ssh-key` | SSH username with private key | SSH key for authentication |
| `anvil-server-port` | Secret text | SSH port (optional; defaults to 22) |
| `anvil-deploy-target` | Secret text | Anvil target identifier |

### Using credentials in Jenkinsfile

```groovy
pipeline {
    agent any

    environment {
        DEPLOY_SERVER_HOST = credentials('anvil-server-host')
        DEPLOY_SERVER_USER = credentials('anvil-server-user')
        DEPLOY_SSH_KEY     = credentials('anvil-ssh-key')
        DEPLOY_SERVER_PORT = credentials('anvil-server-port')
        ANVIL_DEPLOY_TARGET = credentials('anvil-deploy-target')
    }

    stages {
        stage('Deploy') {
            steps {
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
            }
        }
    }
}
```

### Using SSH credentials for remote deployment

If your Jenkins agent needs SSH access to the target server (upload only — install
and activate run ON the server runtime, not on the Jenkins agent; EPIC-011 §8):

```groovy
stage('Deploy') {
    steps {
        sshagent(credentials: ['anvil-ssh-key']) {
            sh '''
                export DEPLOY_SERVER_HOST=$DEPLOY_SERVER_HOST
                export DEPLOY_SERVER_USER=$DEPLOY_SERVER_USER
                anvil deployment upload $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
            '''
        }
    }
}
```

After the upload succeeds, run the release lifecycle on the target server:

```bash
anvil server release install $ANVIL_PROJECT_ID .anvil/artifacts/*.tar.gz   # or: anvil deployment install <target-id> <path>
anvil server release activate $ANVIL_PROJECT_ID <release-id>                # or: anvil deployment activate <target-id> <project-id> <release-id>
```

## Notes

- Anvil installs to `/usr/local/bin/anvil` and is available immediately after the install step.
- Use `stash`/`unstash` or archive artifacts to pass the packaged artifact between stages if running on different agents.
- The `Deploy` stage is gated by `when { branch 'main' }` to prevent accidental deployments.
- `anvil deployment install/activate/rollback/info` are local target-centric aliases of the `server release` operations: they run on the server runtime and require a locally initialized server. A Jenkins agent runs upload only (EPIC-011 §8: CI is "not involved" in the release lifecycle).
- For pinned versions, replace `latest/download` with `download/vX.Y.Z` in the install URL.
- Artifacts in `.anvil/artifacts/` are immutable. The embedded `manifest.json` is the authoritative identity.
- See [Jenkins Pipeline documentation](https://www.jenkins.io/doc/book/pipeline/) for advanced pipeline features.

## See also

- [CI/CD overview](README.md)
- [GitHub Actions](github-actions.md)
- [GitLab CI](gitlab-ci.md)
- [Laravel adapter](../adapters/laravel/)
