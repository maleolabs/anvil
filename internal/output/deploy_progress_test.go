package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeployProgress_PushTicksVisible(t *testing.T) {
	var buf bytes.Buffer
	p := NewDeployProgress(&buf)
	p.StartDeploy("staging")
	p.CompleteBuild(10)
	p.StartVerify()
	p.CompleteVerify(true, 6, 5)
	p.PushTicks("artifact.tar.gz", 1024*1024)
	p.CompleteDeploy(true)
	out := buf.String()
	for _, pct := range []string{"0%", "25%", "50%", "75%", "100%"} {
		if !strings.Contains(out, pct) {
			t.Errorf("progress missing %s in %q", pct, out)
		}
	}
	if !strings.Contains(out, "Push artifact.tar.gz") {
		t.Error("progress missing Push artifact line")
	}
	if len(p.Steps()) == 0 {
		t.Error("no steps recorded")
	}
	// verify step per-check visible via CompleteVerify PASS
	if !strings.Contains(out, "PASS") {
		t.Error("progress missing PASS for verify")
	}
}

func TestDeployProgress_VerifyPerCheckVisible(t *testing.T) {
	var buf bytes.Buffer
	p := NewDeployProgress(&buf)
	p.StartVerify()
	p.CompleteVerify(true, 6, 10)
	// also render per-check lines
	checks := []VerifyCheckVisible{
		{Name: "checksum", Passed: true, Details: "sha256 ok"},
		{Name: "manifest", Passed: true, Details: "schema PASS"},
		{Name: "immutability", Passed: false, Details: "tampered"},
	}
	p.RenderVerifyChecks(checks)
	out := buf.String()
	if !strings.Contains(out, "checksum: sha256 ok") {
		t.Errorf("per-check checksum not visible in %q", out)
	}
	if !strings.Contains(out, "immutability: tampered") && !strings.Contains(out, "[FAIL] immutability") {
		t.Errorf("per-check FAIL not visible in %q", out)
	}
	// should contain both PASS and FAIL
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "FAIL") {
		t.Errorf("per-check PASS/FAIL not both visible in %q", out)
	}
}

func TestEmitPushProgress_ZeroToHundred(t *testing.T) {
	var buf bytes.Buffer
	EmitPushProgress(&buf, "my.tar.gz", 2048)
	out := buf.String()
	for _, pct := range []string{"0%", "25%", "50%", "75%", "100%"} {
		if !strings.Contains(out, pct) {
			t.Errorf("EmitPushProgress missing %s in %q", pct, out)
		}
	}
	if !strings.Contains(out, "my.tar.gz") {
		t.Error("missing artifact base")
	}
	// ensure bytes accounted
	if !strings.Contains(out, "2048") {
		t.Error("missing total bytes")
	}
}

func TestRenderDeployProgressSample_Evidence(t *testing.T) {
	var buf bytes.Buffer
	RenderDeployProgressSample(&buf, "artifact.tar.gz", 1024*1024, true)
	out := buf.String()
	for _, needle := range []string{"Deploy --target staging", "Build artifact", "Verify artifact", "Push artifact", "0%", "100%", "Deploy complete"} {
		if !strings.Contains(out, needle) {
			t.Errorf("sample missing %q in %q", needle, out)
		}
	}
}

func TestDeployProgress_StepsRecording(t *testing.T) {
	p := NewDeployProgress(nil) // nil writer still records
	p.StartDeploy("prod")
	p.CompleteBuild(10)
	p.StartVerify()
	p.CompleteVerify(false, 6, 5)
	p.PushTicks("a.tar.gz", 100)
	p.CompleteDeploy(false)
	if len(p.Steps()) < 3 {
		t.Errorf("steps len=%d want >=3", len(p.Steps()))
	}
}
