package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// addArtifactManifestChecks registers the artifact-manifest contract
// checks (artifact-manifest.md; artifact-manifest.schema.json; ADR-004).
//
// The checks assert the runtime's observable packaging and verification
// behavior: content-derived identity (§4.1), the embedded manifest
// (§4.2), deterministic output (§4.3), path-independent identity
// (§4.4), and verification-before-trust (§5).
func (h *Harness) addArtifactManifestChecks() {
	const contract = "artifact-manifest"

	// A-01: An artifact's identity is a cryptographic hash of its
	// payload content — the same content produces the same identity,
	// different content produces a different identity (artifact-
	// manifest.md §4.1).
	h.add(Check{
		ID:          "A-01",
		Contract:    contract,
		Requirement: "artifact-manifest.md §4.1",
		Title:       "artifact identity is derived from payload content",
		Expected:    "Two packaging runs over identical payload content produce the same content-derived identity; different payload content produces a different identity.",
		Run: func(rt Runtime, ws Workspace) *Result {
			first, err := packageSource(rt, ws, "1.0.0", "<?php\n// identical content\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			second, err := packageSource(rt, ws, "1.0.0", "<?php\n// identical content\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if first.Manifest.ArtifactID == "" {
				return Fail("packaged artifact has an empty content-derived identity")
			}
			if first.Manifest.ArtifactID != second.Manifest.ArtifactID {
				return Fail(fmt.Sprintf("identical payload content produced identities %q and %q — identity must derive from content alone (§4.1)", first.Manifest.ArtifactID, second.Manifest.ArtifactID))
			}

			other, err := packageSource(rt, ws, "1.0.0", "<?php\n// different content\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if other.Manifest.ArtifactID == first.Manifest.ArtifactID {
				return Fail("different payload content produced the same identity — identity must be content-derived and content-determining (§4.1)")
			}
			return Pass()
		},
	})

	// A-02: Deterministic output — identical inputs produce identical
	// identity; no run-specific data enters the content hash
	// (artifact-manifest.md §4.3).
	h.add(Check{
		ID:          "A-02",
		Contract:    contract,
		Requirement: "artifact-manifest.md §4.3",
		Title:       "packaging is deterministic over identical inputs",
		Expected:    "Packaging the same source content twice, at different times, produces the same content hash — and therefore the same identity.",
		Run: func(rt Runtime, ws Workspace) *Result {
			src, err := ws.TempDir("src-")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := os.WriteFile(filepath.Join(src, "index.php"), []byte("<?php\n// deterministic\n"), 0o644); err != nil {
				return Fail("fixture: " + err.Error())
			}
			outFirst, err := ws.TempDir("out-")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			outSecond, err := ws.TempDir("out-")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			first, err := rt.Package(PackageInput{SourceDir: src, OutputDir: outFirst, Version: "1.0.0", Source: conformanceProjectID, ProjectID: conformanceProjectID})
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			second, err := rt.Package(PackageInput{SourceDir: src, OutputDir: outSecond, Version: "1.0.0", Source: conformanceProjectID, ProjectID: conformanceProjectID})
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if first.Manifest.ArtifactID != second.Manifest.ArtifactID {
				return Fail(fmt.Sprintf("identical inputs produced identities %q and %q — packaging must be a pure function of its inputs (§4.3)", first.Manifest.ArtifactID, second.Manifest.ArtifactID))
			}
			return Pass()
		},
	})

	// A-03: The manifest is embedded inside the artifact at a defined,
	// discoverable location and declares the identity, the contract
	// version, and the integrity evidence (artifact-manifest.md §4.2).
	h.add(Check{
		ID:          "A-03",
		Contract:    contract,
		Requirement: "artifact-manifest.md §4.2",
		Title:       "the manifest is embedded in the artifact and self-describing",
		Expected:    "The artifact carries its manifest inside itself; the manifest declares the content-derived identity, a version, and the integrity evidence (checksum), readable without any external sidecar.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.2.3", "<?php\n// content A-03\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			manifest, err := rt.ReadManifest(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("ReadManifest returned an error — the manifest must be embedded at a defined, discoverable location (§4.2): %v", err))
			}
			if manifest.ArtifactID == "" {
				return Fail("embedded manifest does not declare the content-derived identity (§4.2)")
			}
			if manifest.Version != "1.2.3" {
				return Fail(fmt.Sprintf("embedded manifest version = %q, want %q", manifest.Version, "1.2.3"))
			}
			if manifest.Checksum == "" || manifest.ChecksumType == "" {
				return Fail("embedded manifest does not declare the integrity evidence (checksum + algorithm) (§4.2, §5.1)")
			}
			return Pass()
		},
	})

	// A-04: Filenames and paths carry no identity — the artifact's
	// identity is invariant under rename or relocation
	// (artifact-manifest.md §4.4).
	h.add(Check{
		ID:          "A-04",
		Contract:    contract,
		Requirement: "artifact-manifest.md §4.4",
		Title:       "identity is invariant under renaming",
		Expected:    "Renaming the artifact file does not change the identity declared in its embedded manifest — identity comes from content, not from filenames or paths.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content A-04\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			before, err := rt.ReadManifest(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			renamed := filepath.Join(filepath.Dir(artifact.Path), "renamed-artifact.tar.gz")
			if err := os.Rename(artifact.Path, renamed); err != nil {
				return Fail("fixture: " + err.Error())
			}
			after, err := rt.ReadManifest(renamed)
			if err != nil {
				return Fail(fmt.Sprintf("ReadManifest after rename returned an error: %v", err))
			}
			if after.ArtifactID != before.ArtifactID {
				return Fail(fmt.Sprintf("identity changed under rename: %q before, %q after — filenames and paths carry no identity (§4.4)", before.ArtifactID, after.ArtifactID))
			}
			return Pass()
		},
	})

	// A-05: Verification is the integrity gate: an intact artifact is
	// verified by recomputing the content hash and comparing it to the
	// manifest declaration (artifact-manifest.md §5.1) — the report is
	// per-check evidence, not a bare claim.
	h.add(Check{
		ID:          "A-05",
		Contract:    contract,
		Requirement: "artifact-manifest.md §5.1",
		Title:       "an intact artifact passes the integrity gate with re-checkable evidence",
		Expected:    "Verification of an intact artifact passes, reports per-check outcomes, and the mandatory gate (RequireVerified) accepts it.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content A-05\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			report, err := rt.Verify(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("Verify returned an error: %v", err))
			}
			if !report.Passed {
				return Fail(fmt.Sprintf("verification of an intact artifact failed: %s", formatCheckOutcomes(report.Checks)))
			}
			if len(report.Checks) == 0 {
				return Fail("verification report carries no per-check outcomes — evidence must be re-checkable, not a bare claim (§5.1, E2)")
			}
			for _, check := range report.Checks {
				if !check.Passed {
					return Fail(fmt.Sprintf("verification report has a failing check %q while the artifact is intact", check.Name))
				}
			}

			if err := rt.RequireVerified(artifact.Path); err != nil {
				return Fail(fmt.Sprintf("RequireVerified rejected an intact artifact: %v", err))
			}
			return Pass()
		},
	})

	// A-06: Verification-before-trust (G6/R1): an artifact whose
	// content was altered after packaging fails verification, and no
	// lifecycle operation consumes it (artifact-manifest.md §5.3; §4.2:
	// a manifest that no longer matches makes the artifact invalid).
	h.add(Check{
		ID:          "A-06",
		Contract:    contract,
		Requirement: "artifact-manifest.md §5.3, §4.2; verification-contract G6",
		Title:       "an altered artifact fails verification and is rejected before any lifecycle operation",
		Expected:    "Verification of an artifact whose payload content was altered after packaging fails (the recomputed hash no longer matches the manifest claim); the mandatory gate rejects it, and installation does not create a Release from it.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := packageSource(rt, ws, "1.0.0", "<?php\n// content A-06 original\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			tampered, err := rt.TamperPayload(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			report, err := rt.Verify(tampered)
			if err != nil {
				return Fail(fmt.Sprintf("Verify returned an error instead of a failing report: %v", err))
			}
			if report.Passed {
				return Fail("verification passed on an artifact whose payload was altered after packaging — the integrity gate must recompute and compare (a claim is not evidence, §5.1)")
			}

			if err := rt.RequireVerified(tampered); err == nil {
				return Fail("RequireVerified accepted an altered artifact — no lifecycle operation may proceed from unverified inputs (R1)")
			}

			if _, err := rt.Install(tampered); err == nil {
				return Fail("installation consumed an altered artifact — an artifact that fails verification is rejected before any lifecycle operation (§5.3)")
			}
			ready, err := rt.ReleasesIn(StageReady)
			if err != nil {
				return Fail(fmt.Sprintf("ReleasesIn(Ready) returned an error: %v", err))
			}
			if len(ready) != 0 {
				return Fail(fmt.Sprintf("a Release was created from an unverified artifact: %v", ready))
			}
			return Pass()
		},
	})
}

// packageSource creates a source tree with one file and packages it via
// the runtime under test. Fixture setup, not an assertion.
func packageSource(rt Runtime, ws Workspace, version, content string) (ArtifactInfo, error) {
	src, err := ws.TempDir("src-")
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("create source dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.php"), []byte(content), 0o644); err != nil {
		return ArtifactInfo{}, fmt.Errorf("write source file: %w", err)
	}
	out, err := ws.TempDir("out-")
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("create output dir: %w", err)
	}
	return rt.Package(PackageInput{
		SourceDir: src,
		OutputDir: out,
		Version:   version,
		Source:    conformanceProjectID,
		ProjectID: conformanceProjectID,
	})
}

// formatCheckOutcomes renders a verification report's checks for
// diagnostics.
func formatCheckOutcomes(checks []CheckOutcome) string {
	var parts []string
	for _, check := range checks {
		parts = append(parts, fmt.Sprintf("%s=%v(%s)", check.Name, check.Passed, check.Details))
	}
	return strings.Join(parts, "; ")
}
