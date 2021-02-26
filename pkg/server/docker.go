package server

import (
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func DockerBuild(dir string, tag string, dockerfile string) error {
	args := []string{"build", "--tag", tag, "--file", dockerfile}
	args = append(args, ".")
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir

	log.Info("Building image...")

	stdoutDone, err := PipeTo(cmd.StdoutPipe, log.Info)
	if err != nil {
		return err
	}

	stderrDone, err := pipeToWithDockerChecks(cmd.StderrPipe, log.Warn)
	if err != nil {
		return err
	}

	err = cmd.Run()
	<-stdoutDone
	<-stderrDone
	return err
}
