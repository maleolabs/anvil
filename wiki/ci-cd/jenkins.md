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

        // Deploy to target server
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment install ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment activate ${ANVIL_DEPLOY_TARGET} ${ANVIL_PROJECT_ID} ${env.GIT_COMMIT}"
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
| **Deploy** | Verify, upload, install, and activate | `anvil artifact verify`, `anvil deployment upload/install/activate` |

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

        // Deploy to production
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment install ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment activate ${ANVIL_DEPLOY_TARGET} ${ANVIL_PROJECT_ID} ${env.GIT_COMMIT}"
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

        // Deploy to production
        stage('Deploy') {
            when {
                branch 'main'
            }
            steps {
                sh 'anvil artifact verify .anvil/artifacts/*.tar.gz'
                sh "anvil deployment upload ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment install ${ANVIL_DEPLOY_TARGET} .anvil/artifacts/*.tar.gz"
                sh "anvil deployment activate ${ANVIL_DEPLOY_TARGET} ${ANVIL_PROJECT_ID} ${env.GIT_COMMIT}"
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
| `anvil-deploy-target` | Secret text | Anvil target identifier |

### Using credentials in Jenkinsfile

```groovy
pipeline {
    agent any

    environment {
        ANVIL_SERVER_HOST = credentials('anvil-server-host')
        ANVIL_SERVER_USER = credentials('anvil-server-user')
        ANVIL_SSH_KEY     = credentials('anvil-ssh-key')
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

If your Jenkins agent needs SSH access to the target server:

```groovy
stage('Deploy') {
    steps {
        sshagent(credentials: ['anvil-ssh-key']) {
            sh '''
                export ANVIL_SERVER_HOST=$ANVIL_SERVER_HOST
                export ANVIL_SERVER_USER=$ANVIL_SERVER_USER
                anvil deployment upload $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
                anvil deployment install $ANVIL_DEPLOY_TARGET .anvil/artifacts/*.tar.gz
                anvil deployment activate $ANVIL_DEPLOY_TARGET $ANVIL_PROJECT_ID $GIT_COMMIT
            '''
        }
    }
}
```

## Notes

- Anvil installs to `/usr/local/bin/anvil` and is available immediately after the install step.
- Use `stash`/`unstash` or archive artifacts to pass the packaged artifact between stages if running on different agents.
- The `Deploy` stage is gated by `when { branch 'main' }` to prevent accidental deployments.
- For pinned versions, replace `latest/download` with `download/vX.Y.Z` in the install URL.
- Artifacts in `.anvil/artifacts/` are immutable. The embedded `manifest.json` is the authoritative identity.
- See [Jenkins Pipeline documentation](https://www.jenkins.io/doc/book/pipeline/) for advanced pipeline features.

## See also

- [CI/CD overview](README.md)
- [GitHub Actions](github-actions.md)
- [GitLab CI](gitlab-ci.md)
- [Laravel adapter](../adapters/laravel/)
