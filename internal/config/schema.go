// Package config provides the canonical configuration schema definition
// for Anvil projects. The schema is the contract between Anvil and its
// consumers — every configuration value is validated against it.
//
// Reference: TS-P2-01, ADR-005, ADR-002, EPIC-002
package config

// SchemaVersion is the current version of the canonical schema.
// Incremented when breaking changes are introduced (key removals, type
// changes). Additions (new keys) are safe and do not require a version
// increment.
const SchemaVersion = "1.0.0"

// ValueType represents the allowed data types for configuration values.
type ValueType int

const (
	// TypeString represents a UTF-8 string value.
	TypeString ValueType = iota
	// TypeInteger represents a numeric integer value.
	TypeInteger
	// TypeBoolean represents a true/false value.
	TypeBoolean
	// TypeArray represents an ordered list of values of the same type.
	TypeArray
	// TypeObject represents a map of string keys to typed values.
	TypeObject
)

// String returns the human-readable name of the value type.
func (vt ValueType) String() string {
	switch vt {
	case TypeString:
		return "string"
	case TypeInteger:
		return "integer"
	case TypeBoolean:
		return "boolean"
	case TypeArray:
		return "array"
	case TypeObject:
		return "object"
	default:
		return "unknown"
	}
}

// ScopeLevel represents the configuration scope hierarchy level.
// Precedence (lowest to highest): Global, Project, Environment, Execution.
type ScopeLevel int

const (
	// ScopeGlobal represents compiled defaults (lowest precedence).
	ScopeGlobal ScopeLevel = iota
	// ScopeProject represents project-level configuration file values.
	ScopeProject
	// ScopeEnvironment represents environment-specific overrides.
	ScopeEnvironment
	// ScopeExecution represents single-command overrides (highest precedence).
	ScopeExecution
)

// String returns the human-readable name of the scope level.
func (sl ScopeLevel) String() string {
	switch sl {
	case ScopeGlobal:
		return "global"
	case ScopeProject:
		return "project"
	case ScopeEnvironment:
		return "environment"
	case ScopeExecution:
		return "execution"
	default:
		return "unknown"
	}
}

// SchemaEntry defines a single configuration key in the canonical schema.
// It specifies the key's type, allowed values, default, and constraints.
type SchemaEntry struct {
	// Key is the fully qualified dot-notation path (e.g. "project.name").
	Key string

	// Type is the expected value type for this key.
	Type ValueType

	// AllowedValues restricts values to a specific set. Nil means no
	// restriction beyond type checking.
	AllowedValues []string

	// Default is the compiled default value. Nil means no default exists
	// and the key is required-user-input.
	Default interface{}

	// Required indicates whether the key MUST have a value (either from
	// user input or a compiled default).
	Required bool

	// Description is a human-readable explanation of the key's purpose.
	Description string

	// Scope indicates which configuration level this key belongs to.
	Scope ScopeLevel
}

// Schema is the canonical configuration schema for an Anvil project.
// It defines every valid configuration key, its type, allowed values,
// defaults, and constraints.
type Schema struct {
	// Version identifies the schema version for evolution tracking.
	Version string

	// Entries is a map of key names to their schema definitions.
	// Keys are stored in dot-notation format (e.g. "project.name").
	Entries map[string]SchemaEntry
}

// CoreSchema returns the canonical schema with all Core configuration keys
// registered. This is the authoritative schema that all configuration
// operations reference.
//
// Core keys cover the following domains:
//   - Project metadata (EPIC-001)
//   - Artifact packaging (EPIC-003)
//   - Release lifecycle (EPIC-004)
//   - Runtime paths (EPIC-005)
//   - Global settings (EPIC-008)
func CoreSchema() Schema {
	return Schema{
		Version: SchemaVersion,
		Entries: map[string]SchemaEntry{
			// --- Project Metadata (EPIC-001) ---
			"project.name": {
				Key:         "project.name",
				Type:        TypeString,
				Required:    true,
				Description: "Unique project identity. Must be provided at initialization time.",
				Scope:       ScopeProject,
			},
			"project.version": {
				Key:         "project.version",
				Type:        TypeString,
				Default:     "1.0.0",
				Required:    false,
				Description: "Project version identifier in SemVer 2.0.0 format.",
				Scope:       ScopeProject,
			},
			"project.description": {
				Key:         "project.description",
				Type:        TypeString,
				Default:     "",
				Required:    false,
				Description: "Human-readable project description.",
				Scope:       ScopeProject,
			},

			// --- Artifact Packaging (EPIC-003) ---
			"artifact.include": {
				Key:     "artifact.include",
				Type:    TypeArray,
				Default: []string{"**/*"},
				AllowedValues: []string{
					"glob pattern: files to include in the artifact",
				},
				Required:    false,
				Description: "Glob patterns for files to include in the artifact.",
				Scope:       ScopeProject,
			},
			"artifact.exclude": {
				Key:  "artifact.exclude",
				Type: TypeArray,
				Default: []string{
					// Version control directories
					".git/**",
					".svn/**",
					".hg/**",
					// Anvil internal directories
					".anvil/**",
					// CI/CD configuration directories
					".github/**",
					".gitlab/**",
					".circleci/**",
					// IDE and editor configuration directories
					".idea/**",
					".vscode/**",
					// Dependency and cache directories
					"node_modules/**",
					"vendor/**",
					"__pycache__/**",
					// Test directories
					"tests/**",
					"spec/**",
					"__tests__/**",
					// Operating system artifacts
					".DS_Store",
					"Thumbs.db",
					// Log files
					"*.log",
				},
				Required:    false,
				Description: "Glob patterns for files to exclude from the artifact.",
				Scope:       ScopeProject,
			},
			"artifact.output": {
				Key:         "artifact.output",
				Type:        TypeString,
				Default:     ".anvil/artifacts",
				Required:    false,
				Description: "Directory path (relative to project root) where packaged artifacts are written.",
				Scope:       ScopeProject,
			},
			"artifact.manifest": {
				Key:         "artifact.manifest",
				Type:        TypeBoolean,
				Default:     true,
				Required:    false,
				Description: "Whether to generate a manifest file inside the artifact.",
				Scope:       ScopeProject,
			},

			// --- Release Lifecycle (EPIC-004) ---
			"release.max_retained": {
				Key:         "release.max_retained",
				Type:        TypeInteger,
				Default:     5,
				Required:    false,
				Description: "Maximum number of historical releases to retain on disk.",
				Scope:       ScopeProject,
			},
			"release.retention_policy": {
				Key:      "release.retention_policy",
				Type:     TypeString,
				Default:  "keep-last",
				Required: false,
				AllowedValues: []string{
					"keep-last",
				},
				Description: "Strategy for determining which releases to retain.",
				Scope:       ScopeProject,
			},
			"release.auto_verify": {
				Key:         "release.auto_verify",
				Type:        TypeBoolean,
				Default:     true,
				Required:    false,
				Description: "Whether to automatically verify artifact integrity after packaging.",
				Scope:       ScopeProject,
			},
			"release.version_schema": {
				Key:      "release.version_schema",
				Type:     TypeString,
				Default:  "semver",
				Required: false,
				AllowedValues: []string{
					"semver",
				},
				Description: "Versioning scheme for release identifiers.",
				Scope:       ScopeProject,
			},

			// --- Runtime Paths (EPIC-005) ---
			"runtime.install_root": {
				Key:         "runtime.install_root",
				Type:        TypeString,
				Default:     ".anvil/releases",
				Required:    false,
				Description: "Root directory for versioned release directories.",
				Scope:       ScopeProject,
			},
			"runtime.shared_resources": {
				Key:         "runtime.shared_resources",
				Type:        TypeString,
				Default:     ".anvil/shared",
				Required:    false,
				Description: "Directory for shared resources across releases.",
				Scope:       ScopeProject,
			},
			"runtime.active_symlink": {
				Key:         "runtime.active_symlink",
				Type:        TypeString,
				Default:     ".anvil/active",
				Required:    false,
				Description: "Symlink path pointing to the currently active release.",
				Scope:       ScopeProject,
			},
			"runtime.temp_dir": {
				Key:         "runtime.temp_dir",
				Type:        TypeString,
				Default:     ".anvil/tmp",
				Required:    false,
				Description: "Temporary directory used during activation.",
				Scope:       ScopeProject,
			},

			// --- Global Settings (EPIC-008) ---
			"global.log_level": {
				Key:     "global.log_level",
				Type:    TypeString,
				Default: "info",
				AllowedValues: []string{
					"debug", "info", "warn", "error",
				},
				Required:    false,
				Description: "Logging verbosity level.",
				Scope:       ScopeGlobal,
			},
			"global.output_format": {
				Key:     "global.output_format",
				Type:    TypeString,
				Default: "human",
				AllowedValues: []string{
					"human", "json",
				},
				Required:    false,
				Description: "Default output format for CLI commands.",
				Scope:       ScopeGlobal,
			},
			"global.no_color": {
				Key:         "global.no_color",
				Type:        TypeBoolean,
				Default:     false,
				Required:    false,
				Description: "Whether to disable coloured terminal output.",
				Scope:       ScopeGlobal,
			},
			"global.auto_progress": {
				Key:         "global.auto_progress",
				Type:        TypeBoolean,
				Default:     true,
				Required:    false,
				Description: "Whether to show progress indicators for long-running operations.",
				Scope:       ScopeGlobal,
			},

			// --- Installer Forms (STO:installer-pipeline-core v3) ---
			"installer.forms": {
				Key:         "installer.forms",
				Type:        TypeObject,
				Required:    false,
				Description: "Installer forms definition: map of formName -> {fields: [{name,type,required,minLength,pattern,confirmation,when}]} (6 types: text/email/password/select/number/textarea) via ADR-005.",
				Scope:       ScopeProject,
			},
			"installer.setup.super_admin_email": {
				Key:         "installer.setup.super_admin_email",
				Type:        TypeString,
				Default:     "admin@example.com",
				Required:    false,
				Description: "Setup superAdmin email, supports template {{forms.<form>.<field>}} with fallback hardcode.",
				Scope:       ScopeProject,
			},
			"installer.setup.super_admin_name": {
				Key:         "installer.setup.super_admin_name",
				Type:        TypeString,
				Default:     "Admin",
				Required:    false,
				Description: "Setup superAdmin name, supports template {{forms.<form>.<field>}}.",
				Scope:       ScopeProject,
			},
		},
	}
}

// GetSchema returns the canonical schema. It is the primary accessor for
// consumers that need the schema definition — validation engine (TS-P2-02),
// multi-source loader (TS-P2-04), and resolution engine (TS-P2-06).
func GetSchema() Schema {
	return CoreSchema()
}
