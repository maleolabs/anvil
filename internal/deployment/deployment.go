// Package deployment defines the Deployment bounded context for Anvil.
//
// The Deployment domain owns targets, transport, authentication,
// orchestration, and compatibility negotiation. It communicates with
// the Server Runtime domain only through published contracts — it must
// not depend on Runtime Registry, State, or filesystem internals.
//
// Reference: TS-P10-01, TS-P10-02, EPIC-010, ADR-015, Decision 006
package deployment
