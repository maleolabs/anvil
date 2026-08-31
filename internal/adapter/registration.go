// Generic Core-side adapter registration (TS-P7-12): when a project
// selects an adapter, the Core collects the adapter executable's declared
// capabilities and configuration extension through the command contract
// and registers them in the Core registries — enforcing the declaration
// rules (TS-P7-07) and the namespace isolation rules (TS-P7-03,
// ADR-005 §4.4). The helper is framework-agnostic: it contains no
// framework-specific values (ADR-009 §9.6); the framework name and the
// executable path come from the caller (project configuration and
// executable resolution).
package adapter

import (
	"context"
	"errors"
	"fmt"

	"maleolabs.com/anvil/internal/execution"
)

// RegisterAdapterExecutable collects one adapter executable's declared
// capabilities (capabilities command, TS-P7-07) and configuration
// extension (extension command, TS-P7-03) through the command contract
// and registers both in the provided registries.
//
// Registration order:
//  1. The capabilities command is dispatched and the returned
//     CapabilityDeclaration is registered in capabilities — enforcing the
//     capability declaration rules (valid framework segment, one adapter
//     per framework, non-empty and non-duplicate names).
//  2. The extension command is dispatched and the returned
//     ConfigExtension is registered in extensions — enforcing the
//     namespace isolation rules (every key under
//     "framework.<framework>.", no reserved or empty segments).
//
// Any violation — an undeclared framework name, a duplicate registration,
// a declaration outside its namespace — fails the registration with a
// descriptive error, so a misbehaving adapter can never corrupt the Core
// registries. The helper is stateless: the caller owns the registries and
// decides their lifetime (per execution context, matching the stateless
// adapter model of ADR-009 §9.8).
//
// Reference: TS-P7-03, TS-P7-07, TS-P7-12, ADR-005 §4.4, ADR-009 §9.6
func RegisterAdapterExecutable(
	ctx context.Context,
	runner execution.Runner,
	capabilities *CapabilityRegistry,
	extensions *ConfigExtensionRegistry,
	framework, executable string,
) error {
	if runner == nil {
		return errors.New("cannot register adapter executable: Process Runner is nil")
	}
	if capabilities == nil {
		return errors.New("cannot register adapter executable: capability registry is nil")
	}
	if extensions == nil {
		return errors.New("cannot register adapter executable: config extension registry is nil")
	}

	coord := NewCoordinator(runner, capabilities)

	capResult, err := coord.InvokeCapabilities(ctx, framework, executable)
	if err != nil {
		return fmt.Errorf("cannot register adapter executable for framework %q: %w", framework, err)
	}
	if err := capabilities.Register(framework, capResult.Declaration); err != nil {
		return fmt.Errorf("cannot register adapter executable for framework %q: %w", framework, err)
	}

	extResult, err := coord.InvokeConfigExtension(ctx, framework, executable)
	if err != nil {
		return fmt.Errorf("cannot register adapter executable for framework %q: %w", framework, err)
	}
	if err := extensions.Register(extResult.Extension); err != nil {
		return fmt.Errorf("cannot register adapter executable for framework %q: %w", framework, err)
	}

	return nil
}
