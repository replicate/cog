package docker

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/shell"
)

const noRegistry = "no_registry"

type LocalImageBuilder struct {
	registry string
}

func NewLocalImageBuilder(registry string) *LocalImageBuilder {
	if registry == "" {
		registry = noRegistry
	}
	return &LocalImageBuilder{registry: registry}
}

func (b *LocalImageBuilder) Build(dir string, dockerfileContents string, name string, logWriter logger.Logger) (tag string, err error) {
	console.Debugf("Building in %s", dir)

	// TODO(andreas): pipe dockerfile contents to builder
	relDockerfilePath := "Dockerfile"
	dockerfilePath := filepath.Join(dir, relDockerfilePath)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContents), 0644); err != nil {
		return "", fmt.Errorf("Failed to write Dockerfile")
	}

	// shelling out to docker build because it's easier to get logs this way
	// than when using the sdk
	cmd := exec.Command(
		"docker", "build", ".",
		"--progress", "plain",
		"-f", dockerfilePath,
		// "--build-arg", "BUILDKIT_INLINE_CACHE=1",
	)
	cmd.Dir = dir
	// TODO(andreas): follow https://github.com/moby/buildkit/issues/1436, hopefully buildkit will be able to use GPUs soon
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=0")

	lastLogsChan, tagChan, err := buildPipe(cmd.StdoutPipe, logWriter)
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	if err = cmd.Wait(); err != nil {
		lastLogs := <-lastLogsChan
		for _, logLine := range lastLogs {
			logWriter.Info(logLine)
		}
		return "", err
	}

	dockerTag := <-tagChan

	if err != nil {
		return "", err
	}

	logWriter.Infof("Successfully built %s", dockerTag)

	tag = dockerTag
	if name != "" {
		tag = fmt.Sprintf("%s/%s:%s", b.registry, name, dockerTag)
		if err := b.tag(dockerTag, tag, logWriter); err != nil {
			return "", err
		}
	}

	return tag, nil
}

func (b *LocalImageBuilder) tag(dockerTag string, tag string, logWriter logger.Logger) error {
	console.Debugf("Tagging %s as %s", dockerTag, tag)

	cmd := exec.Command("docker", "tag", dockerTag, tag)
	cmd.Env = os.Environ()
	if _, err := cmd.Output(); err != nil {
		ee := err.(*exec.ExitError)
		stderr := string(ee.Stderr)
		return fmt.Errorf("Failed to tag %s as %s, got error: %s", dockerTag, tag, stderr)
	}
	return nil
}

func (b *LocalImageBuilder) Push(tag string, logWriter logger.Logger) error {
	if b.registry == noRegistry {
		return nil
	}

	logWriter.Infof("Pushing %s to registry", tag)

	args := []string{"push", tag}
	cmd := exec.Command("docker", args...)
	cmd.Env = os.Environ()

	console.Debug("Pushing model to Registry...")
	stderrDone, err := pipeToWithDockerChecks(cmd.StderrPipe, logWriter)
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

func buildPipe(pf shell.PipeFunc, logWriter logger.Logger) (lastLogsChan chan []string, tagChan chan string, err error) {
	// TODO: this is a hack, use Docker Go API instead

	// awkward logic: scan docker build output for the string
	// "Successfully built" to find the newly built tag.
	// BUT! that same string is used by pip, so we can only
	// scan for it after we're done pip installing, hence
	// we look for "LABEL" first. obviously this requires
	// all LABELs to be at the end of the build script.

	successPrefix := "Successfully built "
	sectionPrefix := "RUN " + SectionPrefix
	buildkitRegex := regexp.MustCompile("^#[0-9]+ writing image sha256:([0-9a-f]{12}).+$")
	tagChan = make(chan string)

	lastLogsChan = make(chan []string)

	pipe, err := pf()
	if err != nil {
		return nil, nil, err
	}
	scanner := bufio.NewScanner(pipe)
	go func() {
		currentSection := SectionStartingBuild
		currentLogLines := []string{}

		for scanner.Scan() {
			line := scanner.Text()
			logWriter.Debug(line)

			if strings.Contains(line, sectionPrefix) {
				currentSection = strings.SplitN(line, sectionPrefix, 2)[1]
				currentLogLines = []string{}
				logWriter.Infof("  * %s", currentSection)
			} else {
				currentLogLines = append(currentLogLines, line)
			}
			if strings.HasPrefix(line, successPrefix) {
				tagChan <- strings.TrimSpace(strings.TrimPrefix(line, successPrefix))
			}
			match := buildkitRegex.FindStringSubmatch(line)
			if len(match) == 2 {
				tagChan <- match[1]
			}
		}
		lastLogsChan <- currentLogLines
	}()

	return lastLogsChan, tagChan, nil
}

func pipeToWithDockerChecks(pf shell.PipeFunc, logWriter logger.Logger) (done chan struct{}, err error) {
	return shell.PipeTo(pf, func(args ...interface{}) {
		line := args[0].(string)
		if strings.Contains(line, "Cannot connect to the Docker daemon") {
			console.Fatal("Docker does not appear to be running; please start Docker and try again")
		}
		if strings.Contains(line, "failed to dial gRPC: unable to upgrade to h2c, received 502") {
			console.Fatal("Your Docker version appears to be out out date; please upgrade Docker to the latest version and try again")
		}
		if logWriter != nil {
			logWriter.Info(line)
		}
	})
}
