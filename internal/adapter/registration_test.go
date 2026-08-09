// Tests for the generic adapter registration helper (TS-P7-12) and the
// coordinator's config-extension command surface (TS-P7-03): the
// capabilities and extension commands are dispatched through the real
// Process Runner against a stub adapter executable, and the returned
// declarations are registered with the Core registries — enforcing the
// capability declaration rules and the namespace isolation rules.
package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
)

// writeConfigStubAdapter writes a stub adapter executable that implements
// the capabilities and extension commands (and a minimal validate
// command) with Laravel-style declarations. The behavior of each command
// is controlled by the framework name in the payload, mirroring the
// coordinator test stub pattern.
func writeConfigStubAdapter(t *testing.T) string {
	t.Helper()

	script := `#!/bin/sh
# Stub adapter for registration tests: implements capabilities, extension,
# and validate commands per the adapter command contract.
command="$1"
payload="$2"

json_field() {
  printf '%s' "$payload" | grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4
}

case "$command" in
  "capabilities")
    framework=$(json_field framework)
    case "$framework" in
      "dupphase")  echo '{"capabilities":{"activation_phases":["migrate","migrate"]}}' ;;
      "crash")     echo "capabilities exploded" >&2; exit 7 ;;
      "badjson")   echo 'this is not json' ;;
      *)           echo '{"capabilities":{"activation_phases":["migrate","config_cache"],"verification_checks":[{"name":"vendor_present","description":"vendor directory present"}]}}' ;;
    esac
    ;;
  "extension")
    framework=$(json_field framework)
    case "$framework" in
      "outside")   echo '{"extension":{"framework":"laravel","keys":[{"name":"laravel.php_version","description":"not namespaced"}]}}' ;;
      "crash")     echo "extension exploded" >&2; exit 7 ;;
      "badjson")   echo 'this is not json' ;;
      *)           echo '{"extension":{"framework":"laravel","keys":[{"name":"framework.laravel.migrations.path","description":"migration path","default":"database/migrations"},{"name":"framework.laravel.cache.store","description":"cache driver","default":"file"},{"name":"framework.laravel.version","description":"version constraint"}]}}' ;;
    esac
    ;;
  "validate")
    echo '{"valid":true}'
    ;;
  *)
    echo "unknown command $command" >&2
    exit 2
    ;;
esac
exit 0
`

	path := filepath.Join(t.TempDir(), "config-stub-adapter.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	return path
}

// TestRegisterAdapterExecutable_Success verifies that the generic
// registration helper collects the adapter's declared capabilities and
// configuration extension through the command contract and registers both
// in the Core registries (TS-P7-12 AC-1, AC-2, AC-5).
func TestRegisterAdapterExecutable_Success(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	capabilities := NewCapabilityRegistry()
	extensions := NewConfigExtensionRegistry()

	err := RegisterAdapterExecutable(
		context.Background(), execution.NewRunner(),
		capabilities, extensions, "laravel", executable,
	)
	if err != nil {
		t.Fatalf("RegisterAdapterExecutable returned error: %v", err)
	}

	decl, ok := capabilities.Capabilities("laravel")
	if !ok {
		t.Fatal("capability declaration not registered")
	}
	if len(decl.ActivationPhases) != 2 {
		t.Errorf("ActivationPhases = %v, want [migrate config_cache]", decl.ActivationPhases)
	}
	if len(decl.VerificationChecks) != 1 || decl.VerificationChecks[0].Name != "vendor_present" {
		t.Errorf("VerificationChecks = %v, want [vendor_present]", decl.VerificationChecks)
	}

	ext, ok := extensions.Extension("laravel")
	if !ok {
		t.Fatal("config extension not registered")
	}
	if len(ext.Keys) != 3 {
		t.Fatalf("extension keys length = %d, want 3", len(ext.Keys))
	}
	for _, key := range ext.Keys {
		if !strings.HasPrefix(key.Name, "framework.laravel.") {
			t.Errorf("key %q is not under the framework.laravel. namespace", key.Name)
		}
	}
}

// TestRegisterAdapterExecutable_InvalidDeclarationRejected verifies that a
// declaration violating the registry rules — a duplicate phase name —
// fails the registration with a descriptive error (TS-P7-07 rules).
func TestRegisterAdapterExecutable_InvalidDeclarationRejected(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	capabilities := NewCapabilityRegistry()
	extensions := NewConfigExtensionRegistry()

	err := RegisterAdapterExecutable(
		context.Background(), execution.NewRunner(),
		capabilities, extensions, "dupphase", executable,
	)
	if err == nil {
		t.Fatal("RegisterAdapterExecutable succeeded, want error")
	}
	if !strings.Contains(err.Error(), "duplicate activation phase") {
		t.Errorf("error %q does not mention the duplicate phase", err)
	}
	if _, ok := capabilities.Capabilities("dupphase"); ok {
		t.Error("invalid declaration was registered despite the error")
	}
}

// TestRegisterAdapterExecutable_NamespaceViolationRejected verifies that a
// configuration extension key outside the adapter namespace fails the
// registration — namespace isolation is enforced by the Core (TS-P7-12
// AC-2, TS-P7-03).
func TestRegisterAdapterExecutable_NamespaceViolationRejected(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	capabilities := NewCapabilityRegistry()
	extensions := NewConfigExtensionRegistry()

	err := RegisterAdapterExecutable(
		context.Background(), execution.NewRunner(),
		capabilities, extensions, "outside", executable,
	)
	if err == nil {
		t.Fatal("RegisterAdapterExecutable succeeded, want error")
	}
	if !strings.Contains(err.Error(), "not prefixed with the adapter namespace") {
		t.Errorf("error %q does not mention the namespace violation", err)
	}
	if _, ok := extensions.Extension("outside"); ok {
		t.Error("invalid extension was registered despite the error")
	}
}

// TestRegisterAdapterExecutable_ProcessFailures verifies that adapter
// process failures (capabilities or extension command) propagate as
// descriptive registration errors.
func TestRegisterAdapterExecutable_ProcessFailures(t *testing.T) {
	tests := []struct {
		name      string
		framework string
		want      string
	}{
		{name: "capabilities_crash", framework: "crash", want: "adapter process failed"},
		{name: "capabilities_invalid_json", framework: "badjson", want: "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executable := writeConfigStubAdapter(t)
			err := RegisterAdapterExecutable(
				context.Background(), execution.NewRunner(),
				NewCapabilityRegistry(), NewConfigExtensionRegistry(),
				tt.framework, executable,
			)
			if err == nil {
				t.Fatal("RegisterAdapterExecutable succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestRegisterAdapterExecutable_NilDependencies verifies that missing
// registries or runner yield descriptive errors.
func TestRegisterAdapterExecutable_NilDependencies(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	ctx := context.Background()

	if err := RegisterAdapterExecutable(ctx, nil, NewCapabilityRegistry(), NewConfigExtensionRegistry(), "laravel", executable); err == nil {
		t.Error("nil runner succeeded, want error")
	}
	if err := RegisterAdapterExecutable(ctx, execution.NewRunner(), nil, NewConfigExtensionRegistry(), "laravel", executable); err == nil {
		t.Error("nil capability registry succeeded, want error")
	}
	if err := RegisterAdapterExecutable(ctx, execution.NewRunner(), NewCapabilityRegistry(), nil, "laravel", executable); err == nil {
		t.Error("nil config extension registry succeeded, want error")
	}
}

// TestCoordinator_InvokeConfigExtension verifies that the extension
// command is dispatched through the Process Runner and the adapter's
// ConfigExtensionResult JSON is parsed and returned.
//
// Reference: TS-P7-03, TS-P7-12
func TestCoordinator_InvokeConfigExtension(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	coord := NewCoordinator(execution.NewRunner(), NewCapabilityRegistry())

	result, err := coord.InvokeConfigExtension(context.Background(), "laravel", executable)
	if err != nil {
		t.Fatalf("InvokeConfigExtension returned error: %v", err)
	}
	if result.Extension.Framework != "laravel" {
		t.Errorf("Extension.Framework = %q, want %q", result.Extension.Framework, "laravel")
	}
	if len(result.Extension.Keys) != 3 {
		t.Errorf("Extension.Keys length = %d, want 3", len(result.Extension.Keys))
	}
	if result.Extension.Keys[0].Name != "framework.laravel.migrations.path" {
		t.Errorf("first key = %q, want framework.laravel.migrations.path", result.Extension.Keys[0].Name)
	}
}

// TestCoordinator_InvokeConfigExtensionProcessFailure verifies that a
// non-zero adapter exit yields a descriptive Go error.
func TestCoordinator_InvokeConfigExtensionProcessFailure(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	coord := NewCoordinator(execution.NewRunner(), NewCapabilityRegistry())

	_, err := coord.InvokeConfigExtension(context.Background(), "crash", executable)
	if err == nil {
		t.Fatal("InvokeConfigExtension succeeded, want error")
	}
	for _, want := range []string{"status=failure", "exit_code=7", "extension exploded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestCoordinator_InvokeConfigValidation verifies that the validate
// command is dispatched with the provided values and the adapter's
// ConfigValidationResult is parsed and returned.
//
// Reference: TS-P7-03, TS-P7-12
func TestCoordinator_InvokeConfigValidation(t *testing.T) {
	executable := writeConfigStubAdapter(t)
	coord := NewCoordinator(execution.NewRunner(), NewCapabilityRegistry())

	req := contracts.ConfigValidationRequest{
		Values: []contracts.ConfigValue{
			{Key: "framework.laravel.version", Value: "11.0.0"},
		},
	}
	result, err := coord.InvokeConfigValidation(context.Background(), "laravel", executable, req)
	if err != nil {
		t.Fatalf("InvokeConfigValidation returned error: %v", err)
	}
	if !result.Valid {
		t.Error("Valid = false, want true")
	}
}

// TestConfigExtensionRegistry_RegisterLaravelExtension verifies that the
// Laravel-style extension (three keys under framework.laravel.) registers
// through the existing ConfigExtensionRegistry — the same registration
// path the adapter's declared extension goes through — and that a
// Laravel key outside the namespace is rejected (TS-P7-12 AC-1, AC-2).
func TestConfigExtensionRegistry_RegisterLaravelExtension(t *testing.T) {
	ext := contracts.ConfigExtension{
		Framework: "laravel",
		Keys: []contracts.ConfigKey{
			{Name: "framework.laravel.migrations.path", Description: "migration path", Default: "database/migrations"},
			{Name: "framework.laravel.cache.store", Description: "cache driver", Default: "file"},
			{Name: "framework.laravel.version", Description: "version constraint"},
		},
	}

	registry := NewConfigExtensionRegistry()
	if err := registry.Register(ext); err != nil {
		t.Fatalf("Register(laravel extension) failed: %v", err)
	}

	registered, ok := registry.Extension("laravel")
	if !ok {
		t.Fatal("Extension(\"laravel\") = ok=false, want ok=true")
	}
	if len(registered.Keys) != 3 {
		t.Errorf("registered keys length = %d, want 3", len(registered.Keys))
	}

	// A Laravel key outside the namespace must be rejected.
	invalid := contracts.ConfigExtension{
		Framework: "laravel",
		Keys:      []contracts.ConfigKey{{Name: "laravel.php_version", Description: "not namespaced"}},
	}
	other := NewConfigExtensionRegistry()
	if err := other.Register(invalid); err == nil {
		t.Error("Register(key outside namespace) succeeded, want error")
	} else if !strings.Contains(err.Error(), "not prefixed with the adapter namespace") {
		t.Errorf("error %q does not mention the namespace violation", err)
	}
}
