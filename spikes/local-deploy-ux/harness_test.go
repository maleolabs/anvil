package spklocaldeployux

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

func tempArtifactsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ux-artifacts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

// ── AC1: dry-run human + JSON konsisten ───────────────────────────────

func TestAC1_DryRunHumanJSONConsistent(t *testing.T) {
	dir := tempArtifactsDir(t)
	var logBuf bytes.Buffer
	res, err := RunDryRun(DryRunParams{
		ProjectID:    "spike-test-project",
		TargetEnv:    "staging",
		Version:      "0.1.0",
		SizeMB:       1,
		ArtifactsDir: dir,
		Logger:       &logBuf,
	})
	if err != nil {
		t.Fatalf("RunDryRun: %v", err)
	}
	if res.Manifest.ArtifactID == "" || res.Manifest.Checksum == "" {
		t.Fatal("manifest missing identity/checksum")
	}
	if !res.VerifyResult.Passed {
		t.Fatalf("verify FAIL want PASS: %+v", res.VerifyResult.Checks)
	}
	if err := ValidateHumanJSONConsistency(res); err != nil {
		t.Fatalf("consistency: %v", err)
	}
	if res.JSONEnvelope.Version != "1" || res.JSONEnvelope.Status != "success" {
		t.Errorf("envelope version=%q status=%q want 1/success", res.JSONEnvelope.Version, res.JSONEnvelope.Status)
	}
	// JSON data must contain same artifact_id
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.JSONOutput), &env); err != nil {
		t.Fatalf("unmarshal json output: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(env["data"], &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["artifact_id"] != res.Manifest.ArtifactID {
		t.Errorf("artifact_id mismatch json %v vs manifest %q", data["artifact_id"], res.Manifest.ArtifactID)
	}
	if !strings.Contains(res.HumanOutput, res.Manifest.ArtifactID) {
		t.Error("human missing artifact_id")
	}
	if !strings.Contains(res.HumanOutput, "dry-run") && !strings.Contains(strings.ToLower(res.HumanOutput), "dry-run") {
		t.Error("human missing dry-run marker")
	}
	// human must show verify PASS
	if !strings.Contains(res.HumanOutput, "PASS") {
		t.Error("human missing PASS")
	}
}

// AC1 dry-run JSON envelope contract (version:1, status:success|error)
func TestAC1_DryRunJSONEnvelopeContract(t *testing.T) {
	dir := tempArtifactsDir(t)
	res, err := RunDryRun(DryRunParams{TargetEnv: "production", SizeMB: 1, ArtifactsDir: dir, Version: "1.2.3"})
	if err != nil {
		t.Fatalf("RunDryRun: %v", err)
	}
	var env output.OutputEnvelope
	if err := json.Unmarshal([]byte(res.JSONOutput), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if env.Version != "1" {
		t.Errorf("version = %q want 1", env.Version)
	}
	if env.Status != "success" {
		t.Errorf("status = %q want success", env.Status)
	}
}

// ── AC2: network error actionable ─────────────────────────────────────

func TestAC2_NetworkErrorsActionable(t *testing.T) {
	cases := []struct {
		kind       DeployErrorKind
		wantExit   int
		wantRetry  bool
		wantTarget bool
	}{
		{KindTimeout, output.ExitCodeGeneral, true, true},
		{KindUnreachable, output.ExitCodeGeneral, true, true},
	}
	for _, c := range cases {
		e := ClassifiedError(c.kind, "deploy@staging.example.com", &fakeNetErr{"dial timeout"})
		if e.ExitCode() != c.wantExit {
			t.Errorf("%s exit=%d want %d", c.kind, e.ExitCode(), c.wantExit)
		}
		h := FormatAppErrorHuman(e)
		if c.wantTarget && !strings.Contains(h, "staging.example.com") {
			t.Errorf("%s human missing target: %q", c.kind, h)
		}
		if c.wantRetry && !strings.Contains(strings.ToLower(h), "retry") {
			t.Errorf("%s human missing retry suggestion: %q", c.kind, h)
		}
		// JSON error envelope never leaks raw net detail with secret? we use WriteJSONError
		var buf bytes.Buffer
		_ = output.WriteJSONError(&buf, e.Message)
		if buf.Len() == 0 {
			t.Errorf("%s json empty", c.kind)
		}
	}
}

type fakeNetErr struct{ msg string }

func (e *fakeNetErr) Error() string { return e.msg }

// ── AC3: auth fail no leak, exit non-zero ─────────────────────────────

func TestAC3_AuthFailRedactedNoLeak(t *testing.T) {
	t.Setenv("DEPLOY_SSH_KEY", "super-secret-key-material-XYZ")
	kinds := []DeployErrorKind{KindAuthFail, KindPermissionDenied}
	for _, k := range kinds {
		underlying := &fakeNetErr{"key rejected: super-secret-key-material-XYZ at /home/user/.ssh/id_ed25519"}
		e := ClassifiedError(k, "deploy@staging.example.com", underlying)
		if e.ExitCode() == 0 {
			t.Errorf("%s exit 0 want non-zero", k)
		}
		h := FormatAppErrorHuman(e)
		if strings.Contains(h, "super-secret-key-material-XYZ") {
			t.Errorf("%s leaked raw key in human: %q", k, h)
		}
		if strings.Contains(h, "/home/user/.ssh/id_ed25519") {
			t.Errorf("%s leaked raw path in human: %q", k, h)
		}
		if err := AssertNoSecretLeak(h); err != nil {
			t.Errorf("%s leak check: %v", k, err)
		}
		// also ensure RedactSecrets scrubs
		if got := RedactSecrets(h); strings.Contains(got, "super-secret-key-material-XYZ") {
			t.Errorf("%s RedactSecrets missed: %q", k, got)
		}
	}
}

func TestAC3_RedactSecretsTable(t *testing.T) {
	cases := []struct{ in, wantContains string }{
		{"connect with DEPLOY_SSH_KEY=abc host=x", "***REDACTED***"},
		{"key path /home/me/.ssh/id_ed25519", "[REDACTED_PATH]"},
		{"file /tmp/my.pem", "[REDACTED_PATH]"},
		{"normal error without secret", "normal error"},
	}
	for _, c := range cases {
		got := RedactSecrets(c.in)
		if !strings.Contains(got, c.wantContains) && c.wantContains != "normal error" {
			t.Errorf("RedactSecrets(%q)=%q want contain %q", c.in, got, c.wantContains)
		}
	}
}

// ── AC4: progress + help ──────────────────────────────────────────────

func TestAC4_ProgressPushPercentAndVerifyStep(t *testing.T) {
	var buf bytes.Buffer
	RenderProgressSample(&buf, "artifact.tar.gz", 1024*1024, true)
	out := buf.String()
	// push % ticks
	for _, pct := range []string{"0%", "25%", "50%", "75%", "100%"} {
		if !strings.Contains(out, pct) {
			t.Errorf("progress missing %s in %q", pct, out)
		}
	}
	// verification step indicator
	if !strings.Contains(out, "Verify") {
		t.Error("progress missing Verify step")
	}
	if !strings.Contains(out, "PASS") {
		t.Error("progress missing PASS")
	}
	// reporter must have recorded steps
	p := NewDeployProgress(&bytes.Buffer{})
	p.StartDeploy("staging")
	p.CompleteBuild(10)
	p.StartVerify()
	p.CompleteVerify(true, 6, 5)
	p.PushTicks("artifact.tar.gz", 1024)
	p.CompleteDeploy(true)
	if len(p.Steps()) == 0 {
		t.Error("no steps recorded")
	}
}

func TestAC4_HelpDocumentsDryRunAndExitCodes(t *testing.T) {
	help := DeployHelpText()
	for _, needle := range []string{"--target", "--dry-run", "--json", "dry-run", "Exit codes", "0", "1", "2", "4", "Push", "Verify"} {
		if !strings.Contains(help, needle) {
			t.Errorf("help missing %q", needle)
		}
	}
	if !ContainsDryRunInHelp(help) {
		t.Error("ContainsDryRunInHelp false")
	}
}

// error matrix sanity
func TestErrorMatrixExitCodesDocumented(t *testing.T) {
	for _, s := range ErrorMatrix {
		if s.ExitCode != output.ExitCodeGeneral && s.ExitCode != output.ExitCodeConfig && s.ExitCode != output.ExitCodePrecondition && s.ExitCode != output.ExitCodeSuccess {
			t.Errorf("matrix %s has unknown exit code %d", s.Kind, s.ExitCode)
		}
		if s.ExitCode == 0 {
			t.Errorf("matrix %s exit 0 want non-zero for errors", s.Kind)
		}
		// every non-config kind with target must have ShowSSHTarget true
		if s.Kind == KindTimeout || s.Kind == KindUnreachable || s.Kind == KindAuthFail {
			if !s.ShowSSHTarget {
				t.Errorf("matrix %s should show SSH target", s.Kind)
			}
		}
	}
}

// json vs human consistency for error samples (smoke)
func TestErrorHumanAndJSONBothPresent(t *testing.T) {
	e := ClassifiedError(KindTimeout, "deploy@staging.example.com", &fakeNetErr{"i/o timeout"})
	h := FormatAppErrorHuman(e)
	if !strings.Contains(h, "Error:") || !strings.Contains(h, "Reason:") || !strings.Contains(h, "Resolution:") {
		t.Errorf("error 3-part format broken: %q", h)
	}
	var buf bytes.Buffer
	_ = output.WriteJSONError(&buf, e.Message)
	var env output.OutputEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("json envelope parse: %v", err)
	}
	if env.Status != "error" || env.Version != "1" {
		t.Errorf("json envelope status/version = %q/%q", env.Status, env.Version)
	}
}
