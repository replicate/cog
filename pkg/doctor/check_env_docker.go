package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// envCheckTimeout bounds how long an environment probe (docker info,
// python --version) can take before it's considered unresponsive.
const envCheckTimeout = 5 * time.Second

// DockerCheck verifies that Docker is installed and the daemon is reachable.
type DockerCheck struct{}

func (c *DockerCheck) Name() string        { return "env-docker" }
func (c *DockerCheck) Group() Group        { return GroupEnvironment }
func (c *DockerCheck) Description() string { return "Docker" }

func (c *DockerCheck) Check(ctx *CheckContext) ([]Finding, error) {
	execCtx, cancel := context.WithTimeout(ctx.ctx, envCheckTimeout)
	defer cancel()

	if err := exec.CommandContext(execCtx, "docker", "info").Run(); err != nil {
		return []Finding{{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("Docker is not available: %v", err),
			Remediation: "Install Docker (https://docs.docker.com/get-docker/) and ensure the daemon is running",
		}}, nil
	}

	return nil, nil
}

func (c *DockerCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}
