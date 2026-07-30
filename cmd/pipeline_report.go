package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/execution"
)

// printPipelineReport formats and writes the execution report to the command's
// output stream. It displays the pipeline name, status, duration, and a tree
// of stages and tasks with their results.
func printPipelineReport(cmd *cobra.Command, report *execution.ExecutionReport) {
	fmt.Fprintf(cmd.OutOrStdout(), "Pipeline: %s\n", report.PipelineName)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", report.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "Duration: %v\n", report.Duration)
	fmt.Fprintln(cmd.OutOrStdout())

	if len(report.Stages) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Stages: (none)")
		return
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Stages:")
	for _, stage := range report.Stages {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s]\n", stage.Name, stage.Status)
		for _, task := range stage.Tasks {
			fmt.Fprintf(cmd.OutOrStdout(), "    ├─ %s [%s] (%v)\n", task.Name, task.Status, task.Duration)
			if task.Error != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "    └─ Error: %s\n", task.Error)
			} else if task.Status == "failure" {
				fmt.Fprintf(cmd.OutOrStdout(), "    └─ Exit code: %d\n", task.ExitCode)
			}
		}
	}
}
