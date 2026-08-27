// Evidence tests for the per-step contract-stability verification of the
// ordered extraction sequence (TS-017-03-02, T-010).
//
// The tests drive the engine's public exchange machinery (adapter.Coordinator
// over the real Process Runner) against fixture standard executables that
// carry extracted standard content — the stand-in for the anvil-standard-*
// repositories (ADR-025 §6.3). The fixtures answer the command-contract
// commands from per-command JSON documents, mirroring what the standard
// repositories' executables return; the Core never imports framework
// packages (ADR-009 §8.1, ADR-026 decision 4).
//
// TestContractStability verifies every extraction step against both
// framework standards (laravel, flutter), asserting the recorded
// invocations at the fixture's subprocess boundary (invocation fidelity);
// a regression at any step fails the test, which blocks the next step.
// TestContractStability_Regression demonstrates the blocking mechanism: a
// standard executable that violates the contract is rejected at the step,
// and the step fails.
package contractstability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// fixtureFrameworks are the framework standards whose extracted content
// the verification exercises — the first-party standards of the repository
// split (anvil-standard-laravel, anvil-standard-flutter; ADR-025 §4.1).
var fixtureFrameworks = []string{"laravel", "flutter"}

// fixtureContent returns the per-command JSON documents a fixture standard
// executable answers for one framework. The documents mirror the content a
// delivery lifecycle standard carries after extraction (ADR-021 §5.4;
// ADR-025 §6.3): the capability declaration (lifecycle phases and
// verification checks), activation and rollback results, verification
// outcomes, config extension keys under the framework's namespace, config
// validation, and template pipeline definitions.
func fixtureContent(framework string) map[string]string {
	phases := `["migrate","config_cache"]`
	checks := `[{"name":"vendor_present","description":"vendor directory is present"},{"name":"env_configured","description":"environment is configured"}]`
	buildPhase, buildCommand := "composer", "composer install"
	if framework == "flutter" {
		phases = `["pub_get","codegen"]`
		checks = `[{"name":"pubspec_valid","description":"pubspec.yaml is valid"},{"name":"artifacts_resolved","description":"dependencies are resolved"}]`
		buildPhase, buildCommand = "pub", "flutter pub get"
	}
	return map[string]string{
		"capabilities": fmt.Sprintf(
			`{"capabilities":{"activation_phases":%s,"verification_checks":%s,"deployment_model":"server","build_phases":[%q]}}`,
			phases, checks, buildPhase),
		"activate":  `{"success":true,"output":"fixture standard activation phase"}`,
		"verify":    `{"name":"vendor_present","passed":true,"details":"fixture standard check passed"}`,
		"extension": fmt.Sprintf(`{"extension":{"framework":%q,"keys":[{"name":"framework.%s.php_version","description":"declared PHP version","default":"8.2","required":false},{"name":"framework.%s.queue_connection","description":"queue connection","default":"database","required":false}]}}`, framework, framework, framework),
		"validate":  `{"valid":true}`,
		"template":  fmt.Sprintf(`{"build":{"Pipeline":{"Name":"build","Stages":[{"Name":"test","Tasks":[{"Name":"install","Command":%q,"Args":["install"]}]}]}},"CI":{"Pipeline":{"Name":"ci","Stages":[{"Name":"quality","Tasks":[{"Name":"lint","Command":"lint"}]}]}}}`, buildCommand),
	}
}

// writeFixtureStandard writes the fixture standard executable for framework
// into dir: the executable — named anvil-standard-<framework> per the
// executable naming contract (ADR-025 §3.4) — plus the per-command JSON
// content documents it answers. It returns the executable path.
//
// The fixture records every invocation at the subprocess boundary: when
// the ANVIL_FIXTURE_INVOCATION_LOG environment variable is set, each
// invocation appends one "<command> <payload>" line to the log file. The
// invocation log is the evidence that the lifecycle exchange actually
// happened — the test asserts against it, so a regression that skips an
// invocation (a rollback call, a declared phase, a declared check) fails
// the verification (invocation fidelity).
func writeFixtureStandard(t *testing.T, dir, framework string) string {
	t.Helper()
	name := StandardExecutableName(framework)
	path := filepath.Join(dir, name)

	// The fixture answers each command from a JSON document next to the
	// executable; the script embeds the documents' absolute paths so the
	// working directory does not matter.
	var builder strings.Builder
	builder.WriteString("#!/bin/sh\n")
	builder.WriteString("# Contract-stability fixture standard executable (TS-017-03-02):\n")
	builder.WriteString("# a stand-in for the extracted standard content of anvil-standard-<framework>\n")
	builder.WriteString("# (ADR-025 §6.3). Answers the command-contract commands from the JSON\n")
	builder.WriteString("# content documents next to the executable, and records every invocation\n")
	builder.WriteString("# (command + JSON payload) in the file named by ANVIL_FIXTURE_INVOCATION_LOG\n")
	builder.WriteString("# when the environment variable is set.\n")
	builder.WriteString("log() {\n")
	builder.WriteString("  [ -z \"$ANVIL_FIXTURE_INVOCATION_LOG\" ] && return 0\n")
	builder.WriteString("  printf '%s %s\\n' \"$1\" \"${2:-}\" >> \"$ANVIL_FIXTURE_INVOCATION_LOG\"\n")
	builder.WriteString("}\n")
	builder.WriteString("case \"$1\" in\n")
	for _, command := range []string{"capabilities", "activate", "verify", "extension", "validate", "template"} {
		doc := filepath.Join(dir, name+"."+command+".json")
		content := fixtureContent(framework)[command]
		if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture content %s: %v", command, err)
		}
		fmt.Fprintf(&builder, "  %s) log %q \"$2\" ; cat %q ;;\n", command, command, doc)
	}
	builder.WriteString("  *) echo \"fixture standard: unknown command $1\" >&2; exit 2 ;;\n")
	builder.WriteString("esac\n")
	builder.WriteString("exit 0\n")

	if err := os.WriteFile(path, []byte(builder.String()), 0o755); err != nil {
		t.Fatalf("write fixture standard executable %s: %v", name, err)
	}
	return path
}

// corruptFixture overwrites one per-command JSON document of the fixture
// standard executable for framework with content, simulating a standard
// whose extracted content regressed.
func corruptFixture(t *testing.T, dir, framework, command, content string) {
	t.Helper()
	name := StandardExecutableName(framework)
	doc := filepath.Join(dir, name+"."+command+".json")
	if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
		t.Fatalf("corrupt fixture content %s: %v", command, err)
	}
}

// TestContractStability verifies the engine-working condition of every
// extraction step against the extracted standard content of both first-party
// framework standards (ANVIL_V2_EXTRACTION_SEQUENCE §4–§5; TS-017-03-02
// DoD):
//
//   - verification runs at each extraction step;
//   - lifecycle commands pass against extracted standards from the first
//     extraction — the full subprocess contract (capability declaration,
//     activation, rollback, verification, config extension, config
//     validation) is exercised at every step, and the fixture's invocation
//     log proves every declared phase was activated, the first declared
//     phase was rolled back, and every declared check was invoked
//     (invocation fidelity);
//   - step-specific engine-working conditions hold (template generation
//     through the installed standard; config keys/defaults from the
//     installed standard; standard executables resolved per the executable
//     resolution contract);
//   - a regression at any step fails the step — the test failure blocks the
//     next step in CI (extraction-sequence PR sequencing, §6).
func TestContractStability(t *testing.T) {
	dir := t.TempDir()
	for _, framework := range fixtureFrameworks {
		writeFixtureStandard(t, dir, framework)
	}

	for _, step := range AllSteps() {
		for _, framework := range fixtureFrameworks {
			t.Run(fmt.Sprintf("%s/%s", step, framework), func(t *testing.T) {
				executable := filepath.Join(dir, StandardExecutableName(framework))

				// Step 4: standards execute through standard executables
				// resolved per the executable resolution contract
				// (ADR-025 §3.4; ANVIL_V2_EXTRACTION_SEQUENCE §4.4) — the
				// lifecycle exchange runs over the resolved executable's
				// subprocess boundary, never in-process.
				if step == StepAdapterBinaries {
					// The fixture directory is prepended to PATH so the
					// standard executable resolves per the naming contract
					// while the fixture script's own commands (cat, sh)
					// keep resolving from the system PATH.
					t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
					resolved, err := ResolveStandardExecutable(framework)
					if err != nil {
						t.Fatalf("step 4: standard executable %q does not resolve per the executable resolution contract: %v", StandardExecutableName(framework), err)
					}
					if resolved != executable {
						t.Fatalf("step 4: resolved executable %q, want %q", resolved, executable)
					}
					executable = resolved
				}

				// Point the fixture's invocation log at a fresh file and
				// assert the recorded invocations after the verification:
				// the lifecycle exchange must have actually happened at
				// the subprocess boundary (invocation fidelity).
				logPath := filepath.Join(t.TempDir(), "invocations.log")
				t.Setenv(envFixtureInvocationLog, logPath)

				if err := VerifyStep(context.Background(), step, executable, framework); err != nil {
					t.Errorf("verification failed: %v", err)
					return
				}
				assertInvocationFidelity(t, logPath, framework, step)
			})
		}
	}
}

// envFixtureInvocationLog names the environment variable the fixture
// standard executable reads to locate its invocation log file. The value
// is passed through the real subprocess boundary (execution.NewRunner
// inherits the parent environment; command-contract.md §5).
const envFixtureInvocationLog = "ANVIL_FIXTURE_INVOCATION_LOG"

// invocationEntry is one recorded invocation at the fixture's subprocess
// boundary: the contract command name and the JSON payload the runtime
// passed (nil when the command carries no payload).
type invocationEntry struct {
	command string
	payload map[string]any
}

// readInvocationLog parses the fixture's invocation log file into entries.
func readInvocationLog(t *testing.T, logPath string) []invocationEntry {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fixture invocation log: %v", err)
	}
	var entries []invocationEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		entry := invocationEntry{command: parts[0]}
		if len(parts) == 2 && parts[1] != "" {
			if err := json.Unmarshal([]byte(parts[1]), &entry.payload); err != nil {
				t.Fatalf("fixture invocation log payload is not JSON: %v (line %q)", err, line)
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

// declaredFixture returns the capability declaration the fixture content
// for framework declares — the declared phases and checks the invocation
// fidelity assertion holds the exchange to.
func declaredFixture(framework string) contracts.CapabilityDeclaration {
	var result contracts.CapabilityResult
	if err := json.Unmarshal([]byte(fixtureContent(framework)["capabilities"]), &result); err != nil {
		panic(fmt.Sprintf("unmarshal fixture capabilities: %v", err)) // fixture content is fixed at test compile time
	}
	return result.Declaration
}

// assertInvocationFidelity asserts the recorded invocations at the fixture
// subprocess boundary against the declared capability surface
// (command-contract.md §4): the capability declaration was requested,
// every declared activation phase was activated, the first declared phase
// was rolled back through the same declared-capability rule (C4), every
// declared verification check was invoked, and the config extension and
// validation exchanges happened. For step 2, the template exchange is
// additionally required — template generation flows through the installed
// standard (TS-015-01-02). A missing invocation means the lifecycle
// exchange did not happen and the verification must fail (invocation
// fidelity).
func assertInvocationFidelity(t *testing.T, logPath, framework string, step Step) {
	t.Helper()
	decl := declaredFixture(framework)
	entries := readInvocationLog(t, logPath)

	activated := make(map[string]bool)
	rolledBack := make(map[string]bool)
	verified := make(map[string]bool)
	capabilities, extension, validate, template := false, false, false, false
	for _, entry := range entries {
		switch entry.command {
		case contracts.CommandCapabilities:
			capabilities = true
		case contracts.CommandActivation:
			phase, _ := entry.payload["phase"].(string)
			operation, _ := entry.payload["operation"].(string)
			switch contracts.PhaseOperation(operation) {
			case contracts.PhaseOperationActivate:
				activated[phase] = true
			case contracts.PhaseOperationRollback:
				rolledBack[phase] = true
			}
		case contracts.CommandVerification:
			check, _ := entry.payload["check"].(string)
			verified[check] = true
		case contracts.CommandConfigExtension:
			extension = true
		case contracts.CommandConfigValidation:
			validate = true
		case contracts.CommandTemplate:
			template = true
		}
	}

	if !capabilities {
		t.Error("capability declaration exchange did not happen at the subprocess boundary")
	}
	for _, phase := range decl.ActivationPhases {
		if !activated[phase] {
			t.Errorf("declared activation phase %q was not activated — the lifecycle exchange must invoke every declared phase (command-contract.md §4.2)", phase)
		}
	}
	if len(decl.ActivationPhases) > 0 && !rolledBack[decl.ActivationPhases[0]] {
		t.Errorf("declared activation phase %q was not rolled back — rollback must execute through the same declared-capability rule as activation (C4)", decl.ActivationPhases[0])
	}
	for _, check := range decl.VerificationChecks {
		if !verified[check.Name] {
			t.Errorf("declared verification check %q was not invoked — the verification exchange must invoke every declared check (command-contract.md §4.4)", check.Name)
		}
	}
	if !extension {
		t.Error("config extension exchange did not happen at the subprocess boundary")
	}
	if !validate {
		t.Error("config validation exchange did not happen at the subprocess boundary")
	}
	if step == StepTemplateContent && !template {
		t.Error("template exchange did not happen at the subprocess boundary — template generation must flow through the installed standard (TS-015-01-02, A10)")
	}
}

// TestContractStability_RegressionAtAnyStepBlocksNextStep demonstrates the
// blocking mechanism of the extraction sequence (ANVIL_V2_EXTRACTION_SEQUENCE
// §3.1, §5): a regression in any contract exchange at any step is detected
// and fails the step — the verification blocks the next step. Each case
// corrupts one exchange of an otherwise conforming fixture standard
// executable and asserts that VerifyStep rejects it.
func TestContractStability_RegressionAtAnyStepBlocksNextStep(t *testing.T) {
	cases := []struct {
		name    string
		step    Step
		corrupt func(t *testing.T, dir, framework string)
	}{
		{
			name: "activation returns invalid JSON",
			step: StepFrameworkPackages,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "activate", "not-json")
			},
		},
		{
			name: "activation reports failure",
			step: StepFrameworkPackages,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "activate", `{"success":false,"error":"fixture regression"}`)
			},
		},
		{
			name: "verification reports failure",
			step: StepFrameworkPackages,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "verify", `{"name":"vendor_present","passed":false,"details":"fixture regression"}`)
			},
		},
		{
			name: "capability declaration is empty",
			step: StepFrameworkPackages,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "capabilities", `{"capabilities":{}}`)
			},
		},
		{
			name: "standard does not answer the extension command",
			step: StepFrameworkPackages,
			corrupt: func(t *testing.T, dir, framework string) {
				// Remove the extension document: the fixture's cat fails,
				// the subprocess exits non-zero, the exchange fails.
				name := StandardExecutableName(framework)
				if err := os.Remove(filepath.Join(dir, name+".extension.json")); err != nil {
					t.Fatalf("remove extension document: %v", err)
				}
			},
		},
		{
			name: "config extension violates namespace isolation",
			step: StepTemplateContent, // every step re-checks the contract surface
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "extension",
					`{"extension":{"framework":`+fmt.Sprintf("%q", framework)+`,"keys":[{"name":"framework.other.php_version","description":"wrong namespace","default":"8.2"}]}}`)
			},
		},
		{
			name: "config validation rejects extended values",
			step: StepConfigKnowledge,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "validate", `{"valid":false,"errors":["fixture regression"]}`)
			},
		},
		{
			name: "template returns an invalid pipeline definition",
			step: StepTemplateContent,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "template", `{"build":{"Pipeline":{"Name":"","Stages":[]}}}`)
			},
		},
		{
			name: "template returns no pipeline definitions",
			step: StepTemplateContent,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "template", `{}`)
			},
		},
		{
			name: "config extension declares no keys",
			step: StepConfigKnowledge,
			corrupt: func(t *testing.T, dir, framework string) {
				corruptFixture(t, dir, framework, "extension",
					`{"extension":{"framework":`+fmt.Sprintf("%q", framework)+`,"keys":[]}}`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, framework := range fixtureFrameworks {
				writeFixtureStandard(t, dir, framework)
				tc.corrupt(t, dir, framework)

				executable := filepath.Join(dir, StandardExecutableName(framework))
				if err := VerifyStep(context.Background(), tc.step, executable, framework); err == nil {
					t.Errorf("regression %q against %s was not detected — the step must fail and block the next step", tc.name, framework)
				}
			}
		})
	}
}
