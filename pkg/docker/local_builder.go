package docker

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/replicate/modelserver/pkg/shell"
)

type LocalImageBuilder struct {
	registry string
}

func NewLocalImageBuilder(registry string) *LocalImageBuilder {
	return &LocalImageBuilder{registry: registry}
}

func (b *LocalImageBuilder) BuildAndPush(dir string, dockerfilePath string, name string) (fullImageTag string, err error) {
	tag, err := b.build(dir, dockerfilePath)
	if err != nil {
		return "", err
	}
	fullImageTag = fmt.Sprintf("%s/%s:%s", b.registry, name, tag)
	if err := b.tag(tag, fullImageTag); err != nil {
		return "", err
	}
	if err := b.push(fullImageTag); err != nil {
		return "", err
	}
	return fullImageTag, nil
}

func (b *LocalImageBuilder) build(dir string, dockerfilePath string) (tag string, err error) {
	log.Debugf("Building in %s", dir)

	cmd := exec.Command(
		"docker", "build", ".",
		"-f", dockerfilePath,
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")

	tagChan, err := pipeToWithDockerTag(cmd.StderrPipe, log.Debug)
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}
	dockerTag := <-tagChan

	if err = cmd.Wait(); err != nil {
		return "", err
	}

	log.Debugf("Successfully built %s", dockerTag)

	return dockerTag, err
}

func (b *LocalImageBuilder) tag(tag string, fullImageTag string) error {
	log.Debugf("Tagging %s as %s", tag, fullImageTag)

	cmd := exec.Command("docker", "tag", tag, fullImageTag)
	cmd.Env = os.Environ()
	if _, err := cmd.Output(); err != nil {
		ee := err.(*exec.ExitError)
		stderr := string(ee.Stderr)
		return fmt.Errorf("Failed to tag %s as %s, got error: %s", tag, fullImageTag, stderr)
	}
	return nil
}

func (b *LocalImageBuilder) push(tag string) error {
	log.Debugf("Pushing %s", tag)

	args := []string{"push", tag}
	cmd := exec.Command("docker", args...)
	cmd.Env = os.Environ()

	log.Debug("Pushing model to Registry...")
	stderrDone, err := pipeToWithDockerChecks(cmd.StderrPipe, log.Debug)
	if err != nil {
		return err
	}

	err = cmd.Run()
	<-stderrDone
	if err != nil {
		return err
	}
	return nil
}

func pipeToWithDockerTag(pf shell.PipeFunc, lf shell.LogFunc) (tagChan chan string, err error) {
	// TODO: this is a hack, use Docker Go API instead

	// awkward logic: scan docker build output for the string
	// "Successfully built" to find the newly built tag.
	// BUT! that same string is used by pip, so we can only
	// scan for it after we're done pip installing, hence
	// we look for "LABEL" first. obviously this requires
	// all LABELs to be at the end of the build script.

	hasSeenLabel := false
	label := " : LABEL"
	prefix := "Successfully built "
	buildkitRegex := regexp.MustCompile("^#[0-9]+ writing image sha256:([0-9a-f]{12}).+$")
	tagChan = make(chan string)
	doneChan, err := shell.PipeTo(pf, func(args ...interface{}) {
		line := args[0].(string)
		lf(line)
		if hasSeenLabel && strings.HasPrefix(line, prefix) {
			tagChan <- strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
		if !hasSeenLabel && strings.Contains(line, label) {
			hasSeenLabel = true
		}
		match := buildkitRegex.FindStringSubmatch(line)
		if len(match) == 2 {
			tagChan <- match[1]
		}
	})
	if err != nil {
		return nil, err
	}

	go func() {
		<-doneChan
		close(tagChan)
	}()

	return tagChan, nil
}

func pipeToWithDockerChecks(pf shell.PipeFunc, lf shell.LogFunc) (done chan struct{}, err error) {
	return shell.PipeTo(pf, func(args ...interface{}) {
		line := args[0].(string)
		if strings.Contains(line, "Cannot connect to the Docker daemon") {
			log.Fatal("Docker does not appear to be running; please start Docker and try again")
		}
		if strings.Contains(line, "failed to dial gRPC: unable to upgrade to h2c, received 502") {
			log.Fatal("Your Docker version appears to be out out date; please upgrade Docker to the latest version and try again")
		}
		if lf != nil {
			lf(line)
		}
	})
}
