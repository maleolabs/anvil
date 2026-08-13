package conformance

import (
	"fmt"
)

// addVerificationContractChecks registers the verification-contract
// checks (verification-contract.md; verification-contract.schema.json).
//
// The checks assert the runtime's observable gate and evidence behavior:
// gates are mandatory and unskippable (G1, G2), verification precedes
// advancement (G3), failed verification rejects the operation (G6), a
// claim is not evidence (E1), evidence is re-checkable (E2), and
// outcomes merge into the runtime's verification report and are
// recorded as lifecycle evidence (E3, E4).
func (h *Harness) addVerificationContractChecks() {
	const contract = "verification-contract"

	// V-01: Verification is mandatory (G1): no lifecycle operation
	// proceeds from unverified inputs; verification is a transition that
	// cannot be skipped, not an optional command (verification-
	// contract.md §4.1; lifecycle-model.md §6.2 R1).
	h.add(Check{
		ID:          "V-01",
		Contract:    contract,
		Requirement: "verification-contract.md §4.1 G1",
		Title:       "verification is a mandatory gate",
		Expected:    "Installing an artifact that fails verification is rejected: no Release is created from unverified inputs.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content V-01 original\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			tampered, err := rt.TamperPayload(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			if _, err := rt.Install(tampered); err == nil {
				return Fail("installation consumed an unverified artifact — verification is a mandatory, unskippable gate (G1)")
			}

			ready, err := rt.ReleasesIn(StageReady)
			if err != nil {
				return Fail(fmt.Sprintf("ReleasesIn(Ready) returned an error: %v", err))
			}
			if len(ready) != 0 {
				return Fail(fmt.Sprintf("%d Release(s) were created from unverified input (G1)", len(ready)))
			}
			return Pass()
		},
	})

	// V-02: Gates are enforced, not requested (G2): the lifecycle cannot
	// be bypassed — an activation attempt on a Release whose artifact is
	// no longer consumable is rejected and the Release does not advance
	// (verification-contract.md §4.1 G2; lifecycle-model.md §6.2 R2).
	h.add(Check{
		ID:          "V-02",
		Contract:    contract,
		Requirement: "verification-contract.md §4.1 G2",
		Title:       "gates are enforced, not requested",
		Expected:    "An activation attempt that cannot proceed from a consumable verified artifact is rejected with an error; the Release's stage does not advance and no success is recorded.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content V-02\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			rel, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			if err := corruptFile(rel.ArtifactPath); err != nil {
				return Fail("fixture: " + err.Error())
			}

			err = rt.Activate(rel.ID)
			if err == nil {
				return Fail("activation proceeded from an artifact whose integrity was not established — gates are enforced, not requested (G2)")
			}

			stage, stageErr := rt.StageOf(rel.ID)
			if stageErr != nil {
				return Fail(fmt.Sprintf("StageOf returned an error: %v", stageErr))
			}
			if stage != StageReady {
				return Fail(fmt.Sprintf("after the rejected activation the Release stage = %q, want %q — a rejected gate must not advance the lifecycle (G2)", stage, StageReady))
			}
			return Pass()
		},
	})

	// V-03: A claim is not evidence (E1): the manifest declaration is
	// the claim; the verification operation recomputes the hash from the
	// content and compares — the recomputation is the evidence. An
	// artifact whose content was altered after packaging fails
	// verification even though its embedded manifest claim is unchanged
	// (verification-contract.md §5.1; artifact-manifest.md §5.1).
	h.add(Check{
		ID:          "V-03",
		Contract:    contract,
		Requirement: "verification-contract.md §5.1 E1",
		Title:       "a claim is not evidence — deviation is detected by recomputation",
		Expected:    "After the artifact payload is altered, the embedded manifest (the claim) is unchanged, but verification fails: the deviation is detected by recomputing the content hash, not by consulting the claim.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content V-03 original\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			claimBefore, err := rt.ReadManifest(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			tampered, err := rt.TamperPayload(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			claimAfter, err := rt.ReadManifest(tampered)
			if err != nil {
				return Fail(fmt.Sprintf("ReadManifest on the tampered artifact returned an error — the claim must remain readable: %v", err))
			}
			if claimAfter.ArtifactID != claimBefore.ArtifactID || claimAfter.Checksum != claimBefore.Checksum {
				return Fail("the tampered artifact's embedded claim changed — the fixture must preserve the claim so the deviation is detectable only by recomputation")
			}

			report, err := rt.Verify(tampered)
			if err != nil {
				return Fail(fmt.Sprintf("Verify returned an error instead of a failing report: %v", err))
			}
			if report.Passed {
				return Fail("verification passed although the payload content deviates from the manifest claim — a claim is not evidence; the recomputation must detect the deviation (E1)")
			}
			return Pass()
		},
	})

	// V-04: Evidence is re-checkable (E2): any consumer can re-verify
	// using the same embedded evidence (verification-contract.md §5.2;
	// artifact-manifest.md §5.1). Through the interface, re-checkability
	// is observable as outcome stability: verification is a
	// recomputation over the artifact and its embedded evidence, so
	// repeated runs over the same artifact and evidence agree.
	h.add(Check{
		ID:          "V-04",
		Contract:    contract,
		Requirement: "verification-contract.md §5.2 E2",
		Title:       "evidence is re-checkable",
		Expected:    "Verification is a recomputation over the artifact and its embedded evidence: repeated verification runs over the same artifact and the same embedded evidence agree on the outcome — the outcome is stable and re-derivable from the evidence.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content V-04\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			first, err := rt.Verify(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("first Verify returned an error: %v", err))
			}
			second, err := rt.Verify(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("second Verify returned an error: %v", err))
			}
			if first.Passed != second.Passed {
				return Fail(fmt.Sprintf("re-verification changed the outcome: first pass=%v, second pass=%v — evidence must be re-checkable from the same embedded evidence (E2)", first.Passed, second.Passed))
			}
			if !first.Passed {
				return Fail("verification of an intact artifact failed on the first run")
			}
			return Pass()
		},
	})

	// V-05: Verification outcomes merge into the runtime's verification
	// report and are recorded as lifecycle evidence — persisted,
	// queryable, and re-checkable (verification-contract.md §5.3 E3/E4).
	h.add(Check{
		ID:          "V-05",
		Contract:    contract,
		Requirement: "verification-contract.md §5.3 E3, E4",
		Title:       "verification outcomes are recorded as lifecycle evidence",
		Expected:    "A verified artifact's outcome is recorded as persisted, queryable lifecycle evidence carrying the verification result and the identity a consumer can re-verify against.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content V-05\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			if err := rt.RegisterVerified(artifact.Path); err != nil {
				return Fail(fmt.Sprintf("recording the verification outcome returned an error: %v", err))
			}

			evidence, ok := rt.RegistrationEvidence(artifact.Manifest.ArtifactID)
			if !ok {
				return Fail(fmt.Sprintf("no recorded lifecycle evidence for artifact %q — outcomes must be recorded, not left in the verifying process (E3/E4)", artifact.Manifest.ArtifactID))
			}
			if evidence.VerificationResult != "passed" {
				return Fail(fmt.Sprintf("recorded verification result = %q, want %q", evidence.VerificationResult, "passed"))
			}
			if evidence.ArtifactID != artifact.Manifest.ArtifactID {
				return Fail(fmt.Sprintf("recorded evidence identity = %q, want %q — the record must be re-checkable against the artifact's identity (E2)", evidence.ArtifactID, artifact.Manifest.ArtifactID))
			}
			if evidence.RegisteredAt == "" {
				return Fail("recorded evidence carries no timestamp — the record must be queryable lifecycle evidence (E4)")
			}
			return Pass()
		},
	})
}
