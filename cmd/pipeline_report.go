package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/output"
)

// maxStepOutputBytes caps the amount of a task's captured stdout/stderr that
// is rendered per stream in the pipeline run report. Streams larger than this
// are truncated with an explicit marker so oversized output cannot flood the
// terminal.
//
// Reference: TS-006-012 §7
const maxStepOutputBytes = 2000

// printPipelineReport formats and writes the execution report to the command's
// output stream. It displays the pipeline name, status, duration, and a tree
// of stages and tasks with their results.
//
// Each task's captured stdout and stderr (execution.TaskResult.Stdout /
// Stderr) are rendered as indented output blocks below the task line when
// non-empty. Failed tasks additionally show their error string and exit code.
// Status and error values are colored (green success, red failure) when the
// output stream is an interactive terminal; plain otherwise.
//
// Reference: TS-006-012, TS-008-009
func printPipelineReport(cmd *cobra.Command, report *execution.ExecutionReport) {
	out := styleFor(cmd).W

	fmt.Fprintf(out, "Pipeline: %s\n", report.PipelineName)
	fmt.Fprintf(out, "Status: %s\n", stepStatusColor(out, report.Status))
	fmt.Fprintf(out, "Duration: %v\n", report.Duration)
	fmt.Fprintln(out)

	if len(report.Stages) == 0 {
		fmt.Fprintln(out, "Stages: (none)")
		return
	}

	fmt.Fprintln(out, "Stages:")
	for i, stage := range report.Stages {
		stageGlyph := "├─"
		if i == len(report.Stages)-1 {
			stageGlyph = "└─"
		}
		fmt.Fprintf(out, "  %s %s [%s]\n", stageGlyph, stage.Name, stepStatusColor(out, stage.Status))

		for j, task := range stage.Tasks {
			taskGlyph := "├─"
			if j == len(stage.Tasks)-1 {
				taskGlyph = "└─"
			}
			fmt.Fprintf(out, "    %s %s [%s] (%v)\n", taskGlyph, task.Name, stepStatusColor(out, task.Status), task.Duration)

			if task.Stdout != "" {
				writeStepOutput(out, "stdout", task.Stdout)
			}
			if task.Stderr != "" {
				writeStepOutput(out, "stderr", task.Stderr)
			}
			if task.Error != "" {
				fmt.Fprintf(out, "      %s\n", output.Red(out, "Error: "+task.Error))
			}
			if task.Status == "skipped" && task.SkipReason != "" {
				// Platform-aware skip reason (ADR-018, TS-P7-23): explain
				// why the task did not run.
				fmt.Fprintf(out, "      %s\n", output.Yellow(out, "Skipped: "+task.SkipReason))
			}
			if task.Status == "failure" {
				fmt.Fprintf(out, "      Exit code: %d\n", task.ExitCode)
			}
		}
	}
}

// stepStatusColor returns status rendered for terminal display: green for
// success, red for failure, plain for any other status. Colors are applied
// only when out is an interactive terminal with colors enabled.
//
// Reference: TS-008-009
func stepStatusColor(out io.Writer, status string) string {
	switch status {
	case "success":
		return output.Green(out, status)
	case "failure":
		return output.Red(out, status)
	default:
		return status
	}
}

// writeStepOutput renders a single captured output stream (stdout or stderr)
// as an indented block below the task line:
//
//	stdout:
//	  hello
//
// Streams larger than maxStepOutputBytes are truncated with an explicit
// marker showing the original size. A single trailing newline is trimmed
// before splitting into lines; an entirely empty line list writes nothing.
//
// Reference: TS-006-012 §7
func writeStepOutput(out io.Writer, label, stream string) {
	fmt.Fprintf(out, "      %s:\n", label)

	total := len(stream)
	if total > maxStepOutputBytes {
		stream = stream[:maxStepOutputBytes]
	}

	stream = strings.TrimSuffix(stream, "\n")
	lines := strings.Split(stream, "\n")
	if len(lines) == 1 && lines[0] == "" {
		return
	}

	for _, line := range lines {
		fmt.Fprintf(out, "        %s\n", line)
	}

	if total > maxStepOutputBytes {
		fmt.Fprintf(out, "        ... (truncated, %d bytes total)\n", total)
	}
}
