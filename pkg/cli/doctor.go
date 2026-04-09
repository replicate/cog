package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/doctor"
	"github.com/replicate/cog/pkg/util/console"
)

func newDoctorCommand() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check your project for common issues and fix them",
		Long: `Diagnose and fix common issues in your Cog project.

By default, cog doctor reports problems without modifying any files.
Pass --fix to automatically apply safe fixes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), fix)
		},
		Args: cobra.NoArgs,
	}

	addConfigFlag(cmd)
	cmd.Flags().BoolVar(&fix, "fix", false, "Automatically apply fixes")

	return cmd
}

func runDoctor(ctx context.Context, fix bool) error {
	projectDir, err := config.GetProjectDir(configFilename)
	if err != nil {
		return err
	}

	if fix {
		console.Infof("Running cog doctor --fix...")
	} else {
		console.Infof("Running cog doctor...")
	}
	console.Info("")

	result, err := doctor.Run(ctx, doctor.RunOptions{
		Fix:        fix,
		ProjectDir: projectDir,
	}, doctor.AllChecks())
	if err != nil {
		return err
	}

	printDoctorResults(result, fix)

	if result.HasErrors() {
		return fmt.Errorf("doctor found errors")
	}

	return nil
}

func printDoctorResults(result *doctor.Result, fix bool) {
	var currentGroup doctor.Group
	errorCount := 0
	warningCount := 0
	fixedCount := 0

	for _, cr := range result.Results {
		// Print group header when group changes
		if cr.Check.Group() != currentGroup {
			currentGroup = cr.Check.Group()
			console.Infof("%s", string(currentGroup))
		}

		// Check errored internally
		if cr.Err != nil {
			console.Errorf("%s: %v", cr.Check.Description(), cr.Err)
			errorCount++
			continue
		}

		// No findings — passed
		if len(cr.Findings) == 0 {
			console.Successf("%s", cr.Check.Description())
			continue
		}

		// Has findings
		if cr.Fixed {
			console.Successf("Fixed: %s", cr.Check.Description())
			fixedCount += len(cr.Findings)
		} else {
			// Determine worst severity for the check header
			worstSeverity := cr.Findings[0].Severity
			for _, f := range cr.Findings[1:] {
				if f.Severity < worstSeverity {
					worstSeverity = f.Severity
				}
			}
			switch worstSeverity {
			case doctor.SeverityError:
				console.Errorf("%s", cr.Check.Description())
				errorCount++
			case doctor.SeverityWarning:
				console.Warnf("%s", cr.Check.Description())
				warningCount++
			default:
				console.Infof("%s", cr.Check.Description())
			}
		}

		// Print individual findings
		for _, f := range cr.Findings {
			location := ""
			if f.File != "" {
				if f.Line > 0 {
					location = fmt.Sprintf("%s:%d — ", f.File, f.Line)
				} else {
					location = fmt.Sprintf("%s — ", f.File)
				}
			}
			console.Infof("  %s%s", location, f.Message)

			if fix && !cr.Fixed && f.Remediation != "" {
				console.Infof("  (no auto-fix available)")
			}
		}
	}

	console.Info("")

	// Summary line
	if fix && fixedCount > 0 {
		msg := fmt.Sprintf("Fixed %d issue", fixedCount)
		if fixedCount != 1 {
			msg += "s"
		}
		if warningCount > 0 {
			msg += fmt.Sprintf(". %d warning", warningCount)
			if warningCount != 1 {
				msg += "s"
			}
			msg += " remaining"
		}
		if errorCount > 0 {
			msg += fmt.Sprintf(". %d unfixed error", errorCount)
			if errorCount != 1 {
				msg += "s"
			}
		}
		console.Infof("%s.", msg)
	} else if errorCount > 0 || warningCount > 0 {
		parts := []string{}
		if errorCount > 0 {
			s := fmt.Sprintf("%d error", errorCount)
			if errorCount != 1 {
				s += "s"
			}
			parts = append(parts, s)
		}
		if warningCount > 0 {
			s := fmt.Sprintf("%d warning", warningCount)
			if warningCount != 1 {
				s += "s"
			}
			parts = append(parts, s)
		}
		summary := "Found "
		for i, p := range parts {
			if i > 0 {
				summary += ", "
			}
			summary += p
		}
		summary += "."

		if !fix && errorCount > 0 {
			summary += ` Run "cog doctor --fix" to auto-fix.`
		}
		console.Infof("%s", summary)
	} else {
		console.Successf("no issues found")
	}
}
