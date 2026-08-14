package runtime

import "os"

// ReadinessCheck represents the result of a single readiness check.
type ReadinessCheck struct {
	Name    string
	Passed  bool
	Details string
}

// ReadinessResult represents the consolidated readiness assessment.
type ReadinessResult struct {
	Ready  bool
	Checks []ReadinessCheck
}

// ReadinessChecker performs readiness checks against a Runtime environment.
//
// Reference: TS-P5-03, EPIC-005
type ReadinessChecker struct {
	config RuntimeConfig
}

// NewReadinessChecker creates a ReadinessChecker that validates a Runtime
// environment against the given configuration.
func NewReadinessChecker(cfg RuntimeConfig) *ReadinessChecker {
	return &ReadinessChecker{config: cfg}
}

// Check runs all readiness checks and returns a consolidated result.
//
// The assessment is ready if ALL checks pass. If ANY check fails, the
// assessment reports not ready with specific failure details.
//
// Reference: TS-P5-03 AC-1, AC-2, AC-3, AC-4
func (rc *ReadinessChecker) Check() ReadinessResult {
	return rc.checkAll()
}

// checkDirectories verifies that all directories defined in the Runtime
// configuration exist on the filesystem.
func (rc *ReadinessChecker) checkDirectories() ReadinessCheck {
	dirs := rc.config.AllDirs()
	check := ReadinessCheck{
		Name:   "directories",
		Passed: true,
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			check.Passed = false
			if check.Details != "" {
				check.Details += "; "
			}
			check.Details += "missing: " + dir
		}
	}

	if check.Passed {
		check.Details = "all directories exist"
	}

	return check
}

// checkConfig verifies that the Runtime configuration has valid (non-empty)
// values for all required fields.
func (rc *ReadinessChecker) checkConfig() ReadinessCheck {
	check := ReadinessCheck{
		Name:   "config",
		Passed: true,
	}

	if rc.config.InstallRoot == "" {
		check.Passed = false
		check.Details = "install_root is empty"
	} else if rc.config.EnvironmentName == "" {
		check.Passed = false
		check.Details = "environment_name is empty"
	} else if rc.config.DirNamingPattern == "" {
		check.Passed = false
		check.Details = "dir_naming_pattern is empty"
	}

	if check.Passed {
		check.Details = "config values are valid"
	}

	return check
}

// checkAll runs all individual checks and consolidates them into a single
// readiness result. The result is ready only when every check has passed.
func (rc *ReadinessChecker) checkAll() ReadinessResult {
	checks := []ReadinessCheck{
		rc.checkDirectories(),
		rc.checkConfig(),
	}

	ready := true
	for _, c := range checks {
		if !c.Passed {
			ready = false
			break
		}
	}

	return ReadinessResult{
		Ready:  ready,
		Checks: checks,
	}
}
