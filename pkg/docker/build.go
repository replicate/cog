package docker

import (
	"bufio"
	"fmt"
	"github.com/Masterminds/semver"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

const minimumBuildKitVersionEpochRewrite = "0.13.0"

func Build(dir, dockerfile, imageName string, secrets []string, noCache bool, progressOutput string, epoch int64) error {
	var args []string

	args = append(args,
		"buildx", "build",
	)

	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		args = append(args, "--platform", "linux/amd64", "--load")
	}

	for _, secret := range secrets {
		args = append(args, "--secret", secret)
	}

	if noCache {
		args = append(args, "--no-cache")
	}

	// Base Images are special, we force timestamp rewriting to epoch. This requires some consideration on the output
	// format. It's generally safe to override to --output type=docker,rewrite-timestamp=true as the use of `--load` is
	// equivalent to `--output type=docker`
	if epoch >= 0 {
		if !checkBuildKitVersion() {
			os.Exit(1)
		}
		args = append(args, "--build-arg", fmt.Sprintf("SOURCE_DATE_EPOCH=%d", epoch))
		args = append(args, "--output", "type=docker,rewrite-timestamp=true")
		console.Infof("Forcing timestamp rewriting to epoch %d", epoch)

	}

	args = append(args,
		"--file", "-",
		"--cache-to", "type=inline",
		"--tag", imageName,
		"--progress", progressOutput,
		".",
	)

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // redirect stdout to stderr - build output is all messaging
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))
	return cmd.Run()
}

func checkBuildKitVersion() bool {
	cmd := exec.Command("docker", "buildx", "inspect", "--bootstrap")
	output, err := cmd.CombinedOutput()
	if err != nil {
		console.Warnf("Error checking buildx version: %v", err)
		return false
	}

	reader := strings.NewReader(string(output))
	compatible, version, err := findAndCompareBuildKitVersion(reader)
	if err != nil {
		console.Warnf("Error checking buildx buildKit version: %v", err)
		return compatible
	}
	if !compatible {
		console.Warnf("BuildKit version v%s is not compatible with timestamp rewriting. Please upgrade to v%s or later to enable timestamp rewriting.", version, minimumBuildKitVersionEpochRewrite)
	}
	return compatible
}

func findAndCompareBuildKitVersion(r io.Reader) (bool, string, error) {
	compareSemver, err := semver.NewVersion(minimumBuildKitVersionEpochRewrite)
	if err != nil {
		return false, "", fmt.Errorf("invalid compare version: %w", err)
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "BuildKit version:") {
			versionStr := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if strings.HasPrefix(versionStr, "v") {
				versionStr = versionStr[1:]
			}

			version, err := semver.NewVersion(versionStr)
			if err != nil {
				return false, version.String(), fmt.Errorf("invalid version in output: %w", err)
			}

			return version.Equal(compareSemver) || version.GreaterThan(compareSemver), version.String(), nil
		}
	}

	err = scanner.Err()
	if err != nil {
		return false, "", err
	}
	return false, "", fmt.Errorf("version line not found")
}

func BuildAddLabelsAndSchemaToImage(image string, labels map[string]string, bundledSchemaFile string, bundledSchemaPy string) error {
	var args []string

	args = append(args,
		"buildx", "build",
	)

	if util.IsAppleSiliconMac(runtime.GOOS, runtime.GOARCH) {
		// Fixes "WARNING: The requested image's platform (linux/amd64) does not match the detected host platform (linux/arm64/v8) and no specific platform was requested"
		args = append(args, "--platform", "linux/amd64", "--load")
	}

	args = append(args,
		"--file", "-",
		"--tag", image,
	)
	for k, v := range labels {
		// Unlike in Dockerfiles, the value here does not need quoting -- Docker merely
		// splits on the first '=' in the argument and the rest is the label value.
		args = append(args, "--label", fmt.Sprintf(`%s=%s`, k, v))
	}
	// We're not using context, but Docker requires we pass a context
	args = append(args, ".")
	cmd := exec.Command("docker", args...)

	dockerfile := "FROM " + image + "\n"
	dockerfile += "COPY " + bundledSchemaFile + " .cog\n"
	env_path := "/tmp/venv/tools/"
	dockerfile += "RUN python -m venv --symlinks " + env_path + " && " +
		env_path + "/bin/python -m pip install 'datamodel-code-generator>=0.25' && " +
		env_path + "/bin/datamodel-codegen --version && " +
		env_path + "/bin/datamodel-codegen --input-file-type openapi --input " + bundledSchemaFile +
		" --output " + bundledSchemaPy + " && rm -rf " + env_path
	cmd.Stdin = strings.NewReader(dockerfile)

	console.Debug("$ " + strings.Join(cmd.Args, " "))

	if combinedOutput, err := cmd.CombinedOutput(); err != nil {
		console.Info(string(combinedOutput))
		return err
	}
	return nil
}
