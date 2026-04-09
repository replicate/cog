package doctor

import (
	"fmt"
	"os/exec"
)

// DockerCheck verifies that Docker is installed and the daemon is reachable.
type DockerCheck struct{}

func (c *DockerCheck) Name() string        { return "env-docker" }
func (c *DockerCheck) Group() Group        { return GroupEnvironment }
func (c *DockerCheck) Description() string { return "Docker" }

func (c *DockerCheck) Check(_ *CheckContext) ([]Finding, error) {
	if err := exec.Command("docker", "info").Run(); err != nil {
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
