package spklocaldeployux

import (
	"fmt"
	"io"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/output"
)

// ── Deploy Progress Harness (AC4) ─────────────────────────────────────

// DeployPhase models the mocked deploy lifecycle phases.
type DeployPhase string

const (
	PhaseBuild    DeployPhase = "build"
	PhaseVerify   DeployPhase = "verify"
	PhasePush     DeployPhase = "push"
	PhaseInstall  DeployPhase = "install"
	PhaseActivate DeployPhase = "activate"
)

// PhaseStep represents a single progress step (for evidence).
type PhaseStep struct {
	Phase    DeployPhase `json:"phase"`
	Label    string      `json:"label"`
	Percent  int         `json:"percent"` // 0-100 for push; 100 for others
	Duration time.Duration `json:"duration"`
	Status   string      `json:"status"` // "running" | "done" | "fail"
}

// DeployProgress emits human progress lines (push % + verification steps).
// It wraps internal/output PlainStepReporter + custom push ticks so the spike
// demonstrates both the shared reporter and granular push % indicator.
type DeployProgress struct {
	w           io.Writer
	reporter    output.StepReporter
	steps       []PhaseStep
	start       time.Time
	useReporter bool
}

// NewDeployProgress creates a progress reporter writing to w.
// When w is nil, it's a no-op (still records steps for tests).
func NewDeployProgress(w io.Writer) *DeployProgress {
	var r output.StepReporter
	if w != nil {
		r = output.NewPlainStepReporter(w)
	}
	return &DeployProgress{w: w, reporter: r, start: time.Now(), useReporter: r != nil}
}

// StartDeploy announces the deploy workflow (uses shared StepReporter Start).
func (p *DeployProgress) StartDeploy(targetEnv string) {
	if p.useReporter {
		p.reporter.Start(fmt.Sprintf("Deploy --target %s", targetEnv))
		p.reporter.SetTotal(4)
	} else if p.w != nil {
		fmt.Fprintf(p.w, "Deploy --target %s\n", targetEnv)
	}
	p.steps = append(p.steps, PhaseStep{Phase: PhaseBuild, Label: "build", Status: "running"})
}

// CompleteBuild marks build done via reporter.
func (p *DeployProgress) CompleteBuild(d time.Duration) {
	if p.useReporter {
		p.reporter.StepComplete("Build artifact", d)
	} else if p.w != nil {
		fmt.Fprintf(p.w, "  Step: Build artifact ✓ (%s)\n", output.FormatDuration(d))
	}
	for i := range p.steps {
		if p.steps[i].Phase == PhaseBuild {
			p.steps[i].Status = "done"
			p.steps[i].Duration = d
			p.steps[i].Percent = 100
		}
	}
}

// StartVerify emits verification step start (plain + reporter).
func (p *DeployProgress) StartVerify() {
	if p.useReporter {
		p.reporter.StepStart("Verify artifact")
	} else if p.w != nil {
		fmt.Fprintf(p.w, "  Step: Verify artifact...\n")
	}
	p.steps = append(p.steps, PhaseStep{Phase: PhaseVerify, Label: "verify", Status: "running"})
}

// CompleteVerify emits PASS/FAIL per check (AC4 verification step indicator).
func (p *DeployProgress) CompleteVerify(passed bool, checks int, d time.Duration) {
	label := "Verify artifact"
	if passed {
		if p.useReporter {
			p.reporter.StepComplete(label, d)
		} else if p.w != nil {
			fmt.Fprintf(p.w, "  Step: %s ✓ (%s)\n", label, output.FormatDuration(d))
		}
	} else {
		if p.useReporter {
			p.reporter.StepFailed(label, d, fmt.Errorf("verify FAIL %d checks", checks))
		} else if p.w != nil {
			fmt.Fprintf(p.w, "  Step: %s ✗ (%s): verify FAIL\n", label, output.FormatDuration(d))
		}
	}
	if p.w != nil {
		status := output.StatusPass
		msg := fmt.Sprintf("Verify %d checks PASS", checks)
		if !passed {
			status = output.StatusFail
			msg = "Verify FAIL"
		}
		output.PrintStatus(p.w, status, msg)
	}
	for i := range p.steps {
		if p.steps[i].Phase == PhaseVerify && p.steps[i].Status == "running" {
			if passed {
				p.steps[i].Status = "done"
			} else {
				p.steps[i].Status = "fail"
			}
			p.steps[i].Duration = d
			p.steps[i].Percent = 100
		}
	}
}

// PushTicks emits progressive % lines for artifact push (AC4 push %).
// totalBytes is artifact size; it simulates chunks and writes each tick.
func (p *DeployProgress) PushTicks(artifactBase string, totalBytes int64) {
	if p.w != nil {
		fmt.Fprintf(p.w, "  Step: Push %s...\n", artifactBase)
	}
	// emit 0%, 25%, 50%, 75%, 100% — isolated, no real I/O
	percents := []int{0, 25, 50, 75, 100}
	for _, pct := range percents {
		done := totalBytes * int64(pct) / 100
		if p.w != nil {
			fmt.Fprintf(p.w, "    Push %s %d%% (%d/%d bytes)\n", artifactBase, pct, done, totalBytes)
		}
		// small deterministic sleep to make duration non-zero (keeps tests fast)
		time.Sleep(2 * time.Millisecond)
	}
	if p.w != nil {
		fmt.Fprintf(p.w, "  Step: Push %s ✓ (%s)\n", artifactBase, output.FormatDuration(10*time.Millisecond))
	}
	p.steps = append(p.steps, PhaseStep{Phase: PhasePush, Label: fmt.Sprintf("push %s", artifactBase), Percent: 100, Status: "done", Duration: 10 * time.Millisecond})
}

// CompleteDeploy finishes the workflow via reporter.
func (p *DeployProgress) CompleteDeploy(success bool) {
	dur := time.Since(p.start)
	if !p.useReporter {
		if p.w != nil {
			if success {
				fmt.Fprintf(p.w, "Deploy complete (%s)\n", output.FormatDuration(dur))
			} else {
				fmt.Fprintf(p.w, "Deploy failed (%s)\n", output.FormatDuration(dur))
			}
		}
		return
	}
	if success {
		p.reporter.Complete("Deploy complete", dur)
	} else {
		p.reporter.Failed("Deploy", dur)
	}
}

// RenderProgressSample returns a sample progress log for evidence (calls all phases).
func RenderProgressSample(w io.Writer, artifactBase string, totalBytes int64, verifyPassed bool) {
	p := NewDeployProgress(w)
	p.StartDeploy("staging")
	p.CompleteBuild(320 * time.Millisecond)
	p.StartVerify()
	p.CompleteVerify(verifyPassed, 6, 45*time.Millisecond)
	// show push ticks only when verify passed (realistic ordering: verify before push in dry-run,
	// push before verify in full deploy — for evidence we show both variants via flag)
	p.PushTicks(artifactBase, totalBytes)
	p.CompleteDeploy(verifyPassed)
}

// Steps returns recorded steps (for assertions).
func (p *DeployProgress) Steps() []PhaseStep { return p.steps }

// ── Help Snapshot Helper ──────────────────────────────────────────────

// RenderHelpTo writes the mock `anvil deploy --help` text to w and returns it.
func RenderHelpTo(w io.Writer) string {
	help := DeployHelpText()
	fmt.Fprint(w, help)
	return help
}

// ContainsDryRunInHelp checks help contains required flags.
func ContainsDryRunInHelp(help string) bool {
	return strings.Contains(help, "--dry-run") && strings.Contains(help, "--target") && strings.Contains(help, "--json")
}
