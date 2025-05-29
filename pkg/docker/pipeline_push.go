package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
	"github.com/replicate/cog/pkg/util/version"
	"github.com/replicate/cog/pkg/web"
)

const EtagHeader = "etag"

var (
	ErrorBadStatus          = errors.New("Bad status from pipelines-runtime requirements.txt endpoint")
	ErrorPythonPackage      = errors.New("Python package not available in pipelines runtime")
	ErrorPythonPackages     = errors.New("Python packages is not supported in pipelines runtime")
	ErrorETagHeaderNotFound = errors.New("ETag header was not found on pipelines runtime requirements.txt")
)

func PipelinePush(ctx context.Context, image string, projectDir string, webClient *web.Client, client *http.Client, cfg *config.Config) error {
	err := validateRequirements(projectDir, client, cfg)
	if err != nil {
		return err
	}

	tarball, err := createTarball(projectDir)
	if err != nil {
		return err
	}
	return webClient.PostNewPipeline(ctx, image, tarball)
}

func createTarball(folder string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	matcher, err := dockerignore.CreateMatcher(folder)
	if err != nil {
		return nil, err
	}

	err = dockerignore.Walk(folder, matcher, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = relPath

		err = tw.WriteHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(tw, file)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

func downloadRequirements(projectDir string, client *http.Client) (string, error) {
	tmpDir, err := dockercontext.CogBuildArtifactsDirPath(projectDir)
	if err != nil {
		return "", err
	}
	url := requirementsURL()

	resp, err := client.Head(url.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	exists := false
	var requirementsFilePath string
	if resp.StatusCode >= 400 {
		console.Warn("Failed to fetch HEAD for pipelines-runtime requirements.txt")
	} else {
		etag := strings.ReplaceAll(filepath.Base(resp.Header.Get(EtagHeader)), "\"", "")
		requirementsFilePath = filepath.Join(tmpDir, "pipelines_runtime_requirements_"+etag+".txt")
		exists, err = files.Exists(requirementsFilePath)
		if err != nil {
			return "", err
		}
	}

	if !exists {
		resp, err = client.Get(url.String())
		if err != nil {
			return "", err
		}

		if resp.StatusCode >= 400 {
			return "", util.WrapError(ErrorBadStatus, strconv.Itoa(resp.StatusCode))
		}

		etag := strings.ReplaceAll(filepath.Base(resp.Header.Get(EtagHeader)), "\"", "")
		if etag == "." {
			return "", ErrorETagHeaderNotFound
		}
		requirementsFilePath = filepath.Join(tmpDir, "pipelines_runtime_requirements_"+etag+".txt")

		file, err := os.Create(requirementsFilePath)
		if err != nil {
			console.Info("CREATION FAILED!")
			return "", err
		}
		defer file.Close()

		_, err = io.Copy(file, resp.Body)
		if err != nil {
			return "", err
		}
	}

	return requirementsFilePath, nil
}

func requirementsURL() url.URL {
	requirementsURL := pipelinesRuntimeBaseURL()
	requirementsURL.Path = "requirements.txt"
	return requirementsURL
}

func pipelinesRuntimeBaseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   env.PipelinesRuntimeHostFromEnvironment(),
	}
}

func validateRequirements(projectDir string, client *http.Client, cfg *config.Config) error {
	if len(cfg.Build.PythonPackages) > 0 {
		return ErrorPythonPackages
	}

	if cfg.Build.PythonRequirements == "" {
		return nil
	}

	requirementsFilePath, err := downloadRequirements(projectDir, client)
	if err != nil {
		return err
	}

	pipelineRequirements, err := requirements.ReadRequirements(requirementsFilePath)
	if err != nil {
		return err
	}

	projectRequirements, err := requirements.ReadRequirements(cfg.RequirementsFile(projectDir))
	if err != nil {
		return err
	}

	for _, projectRequirement := range projectRequirements {
		projectPackage, projectVersion, _, _, err := requirements.SplitPinnedPythonRequirement(projectRequirement)
		if err != nil {
			return err
		}
		found := false
		for _, pipelineRequirement := range pipelineRequirements {
			pipelinePackage, pipelineVersion, _, _, err := requirements.SplitPinnedPythonRequirement(pipelineRequirement)
			if err != nil {
				return err
			}
			if pipelinePackage == projectPackage {
				found = pipelineVersion == "" || version.GreaterOrEqual(projectVersion, pipelineVersion)
				break
			}
		}
		if !found {
			return util.WrapError(ErrorPythonPackage, projectRequirement)
		}
	}

	return nil
}
