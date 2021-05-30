package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/shell"
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

func (b *LocalImageBuilder) Build(ctx context.Context, dir string, dockerfileContents string, name string, useGPU bool, logWriter logger.Logger) (tag string, err error) {
	console.Debugf("Building in %s", dir)

	// TODO(andreas): pipe dockerfile contents to builder
	relDockerfilePath := "Dockerfile"
	dockerfileDir, err := os.MkdirTemp("/tmp", "dockerfile")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dockerfileDir)

	dockerfilePath := filepath.Join(dockerfileDir, relDockerfilePath)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContents), 0644); err != nil {
		return "", fmt.Errorf("Failed to write Dockerfile")
	}

	var cmd *exec.Cmd
	var outputPipeFn shell.PipeFunc
	if util.IsM1Mac(runtime.GOOS, runtime.GOARCH) {
		cmd, outputPipeFn = b.buildxCommand(ctx, dir, dockerfilePath, logWriter)
		if err != nil {
			return "", err
		}
	} else if useGPU {
		// TODO(andreas): follow https://github.com/moby/buildkit/issues/1436, hopefully buildkit will be able to use GPUs soon
		cmd, outputPipeFn = b.legacyCommand(ctx, dir, dockerfilePath, logWriter)
		if err != nil {
			return "", err
		}
	} else {
		cmd, outputPipeFn = b.buildKitCommand(ctx, dir, dockerfilePath, logWriter)
		if err != nil {
			return "", err
		}
	}

	outputPipe, err := outputPipeFn()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	lastLogs, imageId := buildPipe(outputPipe, logWriter)

	if err = cmd.Wait(); err != nil {
		for _, logLine := range lastLogs {
			logWriter.Info(logLine)
		}
		return "", err
	}

	logWriter.Infof("Successfully built %s", imageId)

	if err != nil {
		return "", err
	}

	tag = imageId
	if name != "" {
		tag = fmt.Sprintf("%s/%s:%s", b.registry, strings.ToLower(name), imageId)
		if err := b.tag(imageId, tag, logWriter); err != nil {
			return "", err
		}

		// tag with :latest so we can rmi the tag created above without removing
		// all the cached layers. this means we only ever keep one copy of the image,
		// and avoid using too much disk space.
		latestTag := fmt.Sprintf("%s/%s:latest", b.registry, strings.ToLower(name))
		if err := b.tag(imageId, latestTag, logWriter); err != nil {
			return "", err
		}
	}

	return tag, nil
}

func (b *LocalImageBuilder) legacyCommand(ctx context.Context, dir string, dockerfilePath string, logWriter logger.Logger) (*exec.Cmd, shell.PipeFunc) {
	// shelling out to docker build because it's easier to get logs this way
	// than when using the sdk
	cmd := exec.CommandContext(
		ctx,
		"docker", "build", ".",
		"--progress", "plain",
		"-f", dockerfilePath,
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=0")

	console.Debug("Using legacy builder")

	return cmd, cmd.StdoutPipe
}

func (b *LocalImageBuilder) buildKitCommand(ctx context.Context, dir string, dockerfilePath string, logWriter logger.Logger) (*exec.Cmd, shell.PipeFunc) {
	// shelling out to docker build because it's easier to get logs this way
	// than when using the sdk
	cmd := exec.CommandContext(
		ctx,
		"docker", "build", ".",
		"--progress", "plain",
		"-f", dockerfilePath,
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")

	console.Debug("Using BuildKit")

	return cmd, cmd.StderrPipe
}

func (b *LocalImageBuilder) buildxCommand(ctx context.Context, dir string, dockerfilePath string, logWriter logger.Logger) (*exec.Cmd, shell.PipeFunc) {
	// shelling out to docker build because it's easier to get logs this way
	// than when using the sdk
	cmd := exec.CommandContext(
		ctx,
		"docker", "buildx", "build", ".",
		"--progress", "plain",
		"-f", dockerfilePath,
		"--build-arg", "BUILDKIT_INLINE_CACHE=1",
		"--platform", "linux/amd64",
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")

	console.Debug("Using Buildx")

	return cmd, cmd.StderrPipe
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

func (b *LocalImageBuilder) Push(ctx context.Context, tag string, logWriter logger.Logger) error {
	if b.registry == noRegistry {
		return nil
	}

	logWriter.Debugf("Pushing %s to registry", tag)

	args := []string{"push", tag}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()

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

func (b *LocalImageBuilder) Cleanup(dockerTag string) error {
	return b.rmi(dockerTag)
}

func (b *LocalImageBuilder) rmi(dockerTag string) error {
	console.Debugf("Untagging %s", dockerTag)

	cmd := exec.Command("docker", "rmi", dockerTag)
	cmd.Env = os.Environ()
	if _, err := cmd.Output(); err != nil {
		ee := err.(*exec.ExitError)
		stderr := string(ee.Stderr)
		return fmt.Errorf("Failed to untag %s: %s", dockerTag, stderr)
	}
	return nil
}

func buildPipe(pipe io.ReadCloser, logWriter logger.Logger) (lastLogs []string, tag string) {
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

	scanner := bufio.NewScanner(pipe)
	currentSection := SectionStartingBuild
	currentLogLines := []string{}
	logWriter.Infof("  * %s", currentSection)

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
			tag = strings.TrimSpace(strings.TrimPrefix(line, successPrefix))
		}
		match := buildkitRegex.FindStringSubmatch(line)
		if len(match) == 2 {
			tag = match[1]
		}
	}
	lastLogs = currentLogLines

	return lastLogs, tag
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
