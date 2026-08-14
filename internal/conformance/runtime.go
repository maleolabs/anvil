// Package conformance implements the conformance harness of the delivery
// lifecycle specification corpus (EPIC-013 / TS-013-05-03).
//
// The harness validates a runtime's OBSERVABLE behavior against the
// published contracts of the specification corpus
// (docs/specification-corpus/, ADR-029 §3) for the declared contract
// version (version-line.md: Version 1.0.0, supported contract majors
// {1} — ADR-024 §3.1).
//
// Engine-path independence (ADR-029 §3; Transition Plan §5.10). The
// checks in this package validate behavior, not engine internals: they
// drive the runtime exclusively through the Runtime interface defined in
// runtime.go — a contract-level operation surface (package an artifact,
// install a verified artifact, activate, roll back, query release state,
// exchange lifecycle phases with a standard) — and assert on the
// observable outcomes. The checks never import an engine package and
// never reference an engine path; the Anvil Runtime binding
// (anvil_runtime.go) is the only file that maps the interface to the
// engine's public API. Any independent runtime implementation can be
// validated by implementing the same interface (Transition Plan §5.10).
//
// Conformance evidence. A failed check reports the contract, the
// expected behavior (from the contract), and the observed deviation, so
// results are re-checkable and failures are actionable (TS-013-05-03
// §2). The harness runs in the Core repository's CI as the drift
// mitigation between specification and engine (ADR-029 §3; Transition
// Plan §11.2).
//
// Scope bound (TS-013-05-03 §4). The checks assert only behaviors the
// published contracts define. No conformance requirement is invented
// here; where the runtime exposes no operation for a contract rule, the
// rule is not exercised and is not asserted.
package conformance

// DeclaredContractVersion is the contract version the harness validates
// against. It mirrors the corpus version-line declaration
// (docs/specification-corpus/version-line.md — Version 1.0.0, supported
// contract majors {1}) and the compatibility matrix record
// (compatibility-matrix.json — contract_version 1.0.0). When the
// version line advances, this declaration is updated by the release
// tooling that maintains the corpus (version-line.md §4–§6).
const DeclaredContractVersion = "1.0.0"

// Stage is a release state as defined by the lifecycle-model contract
// (lifecycle-model.md §6.1): exactly the 8 contract states, named by
// the contract. There is no "interrupted" state (schema invariants.
// interruptedIsNotAState).
type Stage string

// The contract release states (lifecycle-model.md §6.1; ADR-003 §4).
const (
	StageReady       Stage = "Ready"
	StageActivating  Stage = "Activating"
	StageActive      Stage = "Active"
	StageRollingBack Stage = "Rolling Back"
	StageRolledBack  Stage = "Rolled Back"
	StageArchived    Stage = "Archived"
	StageFailed      Stage = "Failed"
	StageRemoved     Stage = "Removed"
)

// Transition is one recorded stage transition attempt of a Release.
// Every attempt — successful and failed — is recorded so outcomes are
// observable and audit-trail decisions derive from state, never from
// memory (lifecycle-model.md §6.2 R8).
type Transition struct {
	// Timestamp is the RFC 3339 time the transition was attempted.
	Timestamp string

	// From is the source stage of the attempt.
	From Stage

	// To is the target stage of the attempt.
	To Stage

	// Outcome is "success" or the error description of a rejected or
	// failed attempt (lifecycle-model.md §6.5: no silent success).
	Outcome string
}

// ReleaseInfo is the observable state of one Release.
type ReleaseInfo struct {
	// ID is the release identity.
	ID string

	// ArtifactID is the content-derived identity of the artifact the
	// release was installed from (artifact-manifest.md §4.1).
	ArtifactID string

	// Version is the artifact version recorded in the manifest.
	Version string

	// Stage is the Release's current lifecycle stage.
	Stage Stage

	// ArtifactPath is the runtime's stored copy of the artifact. It is
	// the runtime's own artifact-store path, exposed so checks can
	// observe integrity behavior without knowing the store layout.
	ArtifactPath string
}

// RollbackOutcome is the observable outcome of a rollback operation.
type RollbackOutcome struct {
	// RolledBackRelease is the Release that was Active and is now
	// preserved as Rolled Back (lifecycle-model.md §5.2).
	RolledBackRelease *ReleaseInfo

	// RestoredRelease is the Release restored to Active by the forward
	// transition (lifecycle-model.md §5.2; ADR-003 §7.1).
	RestoredRelease *ReleaseInfo
}

// PackageInput carries the packaging parameters a runtime accepts
// (artifact-manifest.md §4.3: identical inputs → identical output).
type PackageInput struct {
	// SourceDir is the directory whose content becomes the artifact
	// payload.
	SourceDir string

	// OutputDir is the directory the runtime writes the artifact to.
	OutputDir string

	// Version is the project version recorded in the manifest.
	Version string

	// Source is the project name recorded in the manifest.
	Source string

	// ProjectID is the repository project identity recorded in the
	// manifest.
	ProjectID string
}

// ArtifactInfo is the observable outcome of a packaging operation.
type ArtifactInfo struct {
	// Path is the runtime-produced artifact file path.
	Path string

	// Manifest is the manifest embedded in the artifact
	// (artifact-manifest.md §4.2).
	Manifest ManifestInfo
}

// ManifestInfo is the embedded manifest content the contract declares
// an artifact must carry: the content-derived identity, the contract
// version, and the verification evidence (artifact-manifest.md §4.2).
type ManifestInfo struct {
	// ArtifactID is the content-derived identity — the hash of the
	// payload content (artifact-manifest.md §4.1).
	ArtifactID string

	// Version is the project version.
	Version string

	// CreatedAt is the ISO 8601 manifest creation timestamp.
	CreatedAt string

	// Source is the project name.
	Source string

	// Checksum is the integrity checksum over the deployable content —
	// the embedded evidence (artifact-manifest.md §5.1).
	Checksum string

	// ChecksumType identifies the checksum algorithm.
	ChecksumType string

	// ProjectID is the repository project identity.
	ProjectID string
}

// CheckOutcome is one verification check outcome in a verification
// report (verification-contract.md §5.3 E3: outcomes merge into the
// runtime's verification report).
type CheckOutcome struct {
	// Name identifies the check.
	Name string

	// Passed reports whether the check passed.
	Passed bool

	// Details describes what was validated and, on failure, what failed.
	Details string
}

// VerificationReport is the runtime's record of a verification run —
// re-checkable evidence, not a claim (verification-contract.md §5.1 E1).
type VerificationReport struct {
	// Passed is true only when every check passed.
	Passed bool

	// Checks are the per-check outcomes of this run.
	Checks []CheckOutcome
}

// RegistrationInfo is the recorded lifecycle evidence of a verified
// artifact (verification-contract.md §5.3 E4: outcomes are recorded as
// lifecycle evidence — persisted and queryable).
type RegistrationInfo struct {
	// ArtifactID is the content-derived identity of the artifact.
	ArtifactID string

	// Version is the artifact version.
	Version string

	// VerificationResult is the recorded verification outcome.
	VerificationResult string

	// RegisteredAt is when the evidence was recorded (RFC 3339).
	RegisteredAt string
}

// CapabilityDeclaration is the declared capability surface of a
// standard (command-contract.md §4.1): the lifecycle phases and
// verification checks the standard provides, declared before any of it
// is invoked. The runtime invokes only declared capability; undeclared
// capability is never called (C1; Manifesto §7).
type CapabilityDeclaration struct {
	// ActivationPhases are the declared framework phases, in declared
	// order (C2: declared order is the executed order).
	ActivationPhases []string

	// VerificationChecks are the declared verification checks by name.
	VerificationChecks []string

	// IrreversiblePhases are the declared phases whose rollback
	// reversal is irreversible — the declaration documents the
	// irreversibility (command-contract.md §4.3; schema
	// rollbackSemantics.irreversible). Irreversibility never blocks
	// rollback (C4): the runtime performs the rollback regardless.
	IrreversiblePhases []string
}

// SubprocessCall is one observable invocation at the runtime–standard
// exchange boundary (command-contract.md §5: standalone executables
// invoked as subprocesses; structured JSON over the subprocess
// boundary). The harness observes this log to assert the
// declared-capability rule and the JSON exchange — it does not assert
// how the runtime implemented the invocation.
type SubprocessCall struct {
	// Command is the contract command name passed to the subprocess
	// (e.g. "activate", "verify").
	Command string

	// Args are the full arguments passed to the subprocess; the
	// structured JSON payload is the trailing argument.
	Args []string

	// Stdout is the JSON document the subprocess returned, parsed by
	// the runtime as the authoritative result.
	Stdout string
}

// Workspace provides isolated scratch space for harness fixtures (source
// trees, artifact output, runtime roots). Implementations must create
// the directory and return its path.
type Workspace interface {
	// TempDir creates a fresh scratch directory with the given prefix.
	TempDir(prefix string) (string, error)
}

// Runtime is the observable operation surface the conformance checks
// validate against. It is deliberately contract-shaped — every method
// corresponds to a lifecycle operation the published contracts define,
// and every returned value is a contract-level fact (stage, identity,
// evidence, declared capability). Checks exercise behavior through this
// surface only; they never reach into engine internals.
//
// An independent runtime implementation (Transition Plan §5.10) is
// validated by implementing this interface; the checks run unchanged.
type Runtime interface {
	// --- artifact-manifest contract surface (artifact-manifest.md) ---

	// Package produces an artifact from source content under the
	// manifest contract: content-derived identity, embedded manifest,
	// deterministic output (§4).
	Package(in PackageInput) (ArtifactInfo, error)

	// ReadManifest reads the manifest embedded inside the artifact at
	// the defined location (§4.2).
	ReadManifest(artifactPath string) (ManifestInfo, error)

	// Verify runs the integrity gate: it recomputes the content hash
	// from the artifact's content and compares it to the manifest
	// declaration — the claim is the manifest, the recomputation is the
	// evidence (§5.1).
	Verify(artifactPath string) (VerificationReport, error)

	// RequireVerified is the mandatory gate: it returns an error when
	// the artifact is not verified, so no lifecycle operation can
	// proceed from unverified inputs (§5; R1).
	RequireVerified(artifactPath string) error

	// TamperPayload returns a copy of the artifact at artifactPath whose
	// deployable payload content differs from the content its embedded
	// manifest declares — the state of an artifact whose content was
	// altered after packaging (artifact-manifest.md §5.1). The embedded
	// manifest (the claim) is preserved unchanged, so the deviation is
	// detectable only by recomputation. The artifact's internal layout
	// is the runtime's own knowledge; the harness does not assume one,
	// and the runtime implements this fixture using its own layout.
	// It is a fixture hook for the integrity-gate checks, not a
	// lifecycle operation.
	TamperPayload(artifactPath string) (string, error)

	// --- lifecycle-model contract surface (lifecycle-model.md) ---

	// Install is the installation operation: a verified artifact is
	// adopted by manifest identity and a Release is created directly in
	// Ready — an operation, not a state transition (§6.3). Installation
	// is idempotent by content identity (R7): installing the same
	// artifact twice must not create a second Release.
	Install(artifactPath string) (ReleaseInfo, error)

	// Activate executes the activation phase sequence for the Release
	// (§5.1). It must reject a Release that is not Ready (R2: illegal
	// transitions are rejected, not advised against) and must leave
	// exactly one Release Active (R3).
	Activate(releaseID string) error

	// Rollback restores the previously Active Release by a forward
	// transition (R5): the rollback target is the Archived Release with
	// the newest archival timestamp among Releases that were previously
	// Active, and it must be in state Archived — otherwise the
	// operation is rejected (§5.2). The rolled-back Release is
	// preserved for inspection.
	Rollback() (RollbackOutcome, error)

	// ReconcileInterruptedRollback reconciles Releases stuck in the
	// transitional Rolling Back stage (§6.5): an interrupted operation
	// is never silently reported as success (R6). It returns the IDs of
	// the reconciled Releases.
	ReconcileInterruptedRollback() ([]string, error)

	// PersistInterruptedRollback is a fixture hook: it persists a
	// Release in the Rolling Back transitional stage — exactly the
	// persisted state a crash mid-rollback leaves behind (§6.5) — so
	// the recovery rule can be exercised.
	PersistInterruptedRollback(releaseID string) error

	// StageOf returns the persisted, authoritative stage of a Release
	// (R8: lifecycle facts derive from state).
	StageOf(releaseID string) (Stage, error)

	// ReleasesIn lists the Releases currently in the given stage.
	ReleasesIn(stage Stage) ([]ReleaseInfo, error)

	// Active returns the Release currently in the Active stage, or nil
	// when no Release is Active.
	Active() (*ReleaseInfo, error)

	// HistoryOf returns the recorded transition history of a Release —
	// every attempt, successful and failed, in order (R8).
	HistoryOf(releaseID string) ([]Transition, error)

	// --- verification-contract evidence surface
	// (verification-contract.md §5.3) ---

	// RegisterVerified records a verified artifact's verification
	// outcome as lifecycle evidence (E3/E4): persisted, queryable, and
	// re-checkable.
	RegisterVerified(artifactPath string) error

	// RegistrationEvidence returns the recorded lifecycle evidence for
	// an artifact identity, if recorded (E4).
	RegistrationEvidence(artifactID string) (RegistrationInfo, bool)

	// --- standard command-contract exchange surface
	// (command-contract.md §4–§5) ---

	// RegisterCapability records a standard's declared capability
	// surface (C1: the runtime invokes only declared capability). An
	// empty declaration is legal (§4.1: declaring nothing proceeds with
	// generic operations).
	RegisterCapability(framework string, decl CapabilityDeclaration) error

	// DeclaredPhases returns the declared activation phases of a
	// framework in declared order, and whether a declaration exists.
	DeclaredPhases(framework string) ([]string, bool)

	// InvokePhase invokes one activation phase operation (activate or
	// rollback) through the runtime–standard exchange. It must reject
	// an invocation that is not declared — undeclared capability is
	// never called (C1) — and the exchange must be structured JSON over
	// the subprocess boundary (§5).
	InvokePhase(framework, phase, operation string) error

	// InvokeCheck invokes one verification check through the exchange.
	// It must reject an undeclared check (C1).
	InvokeCheck(framework, check string) error

	// SubprocessCalls returns the observable invocation log at the
	// runtime–standard subprocess boundary.
	SubprocessCalls() []SubprocessCall
}
