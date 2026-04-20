package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PythonVersionCheck verifies that Python is available and that the local
// version is consistent with the version configured in cog.yaml.
type PythonVersionCheck struct{}

func (c *PythonVersionCheck) Name() string        { return "env-python-version" }
func (c *PythonVersionCheck) Group() Group        { return GroupEnvironment }
func (c *PythonVersionCheck) Description() string { return "Python version" }

func (c *PythonVersionCheck) Check(ctx *CheckContext) ([]Finding, error) {
	if ctx.PythonPath == "" {
		return []Finding{{
			Severity:    SeverityWarning,
			Message:     "Python not found in PATH",
			Remediation: "Install Python 3.10+ or ensure it is on your PATH",
		}}, nil
	}

	execCtx, cancel := context.WithTimeout(ctx.ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(execCtx, ctx.PythonPath, "--version").Output()
	if err != nil {
		return []Finding{{
			Severity:    SeverityWarning,
			Message:     fmt.Sprintf("could not determine Python version: %v", err),
			Remediation: "Ensure your Python installation is working correctly",
		}}, nil
	}

	// Output is "Python 3.12.1\n"
	version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "Python"))
	localMajorMinor := majorMinor(version)

	// If cog.yaml specifies a Python version, compare
	if ctx.Config != nil && ctx.Config.Build != nil && ctx.Config.Build.PythonVersion != "" {
		configMajorMinor := majorMinor(ctx.Config.Build.PythonVersion)
		if configMajorMinor != "" && localMajorMinor != "" && configMajorMinor != localMajorMinor {
			return []Finding{{
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"local Python is %s but cog.yaml specifies %s",
					localMajorMinor, configMajorMinor,
				),
				Remediation: "This is usually fine -- Docker builds use the configured version. Update cog.yaml or your local Python if needed.",
			}}, nil
		}
	}

	return nil, nil
}

func (c *PythonVersionCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}

// majorMinor extracts "3.12" from "3.12.1" or "3.12".
func majorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
