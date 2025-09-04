package procedure

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	version "github.com/aquasecurity/go-pep440-version"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

const EtagHeader = "etag"
const PythonVersion = "3.13"

var (
	ErrorBadStatus          = errors.New("Bad status from pipelines-runtime requirements.txt endpoint")
	ErrorPythonPackage      = errors.New("Python package not available in pipelines runtime")
	ErrorPythonPackages     = errors.New("Python packages is not supported in pipelines runtime")
	ErrorETagHeaderNotFound = errors.New("ETag header was not found on pipelines runtime requirements.txt")
	ErrorPythonVersion      = errors.New("Python version does not match the required python version: " + PythonVersion)
	ErrorSystemPackage      = errors.New("System package not available in pipelines runtime")
	ErrorRunCommand         = errors.New("Run commands are not supported in pipelines runtime")
	ErrorGPU                = errors.New("GPU is not supported in pipelines runtime")
	ErrorCUDA               = errors.New("CUDA is not supported in pipelines runtime")
	ErrorConcurrency        = errors.New("Concurrencies above 1 are not supported in pipelines runtime")
)

var SystemPackages = map[string]bool{
	"curl":        true,
	"ffmpeg":      true,
	"imagemagick": true,
}

func Validate(projectDir string, client *http.Client, cfg *config.Config, fill bool) error {
	// Validate requirements
	err := validateRequirements(projectDir, client, cfg, fill)
	if err != nil {
		return err
	}

	// Handle python versions
	if fill && cfg.Build.PythonVersion == "" {
		cfg.Build.PythonVersion = PythonVersion
	}
	if cfg.Build.PythonVersion != PythonVersion {
		return util.WrapError(ErrorPythonVersion, cfg.Build.PythonVersion)
	}

	// Handle system packages
	seenSystemPackages := map[string]bool{}
	for _, systemPackage := range cfg.Build.SystemPackages {
		_, ok := SystemPackages[systemPackage]
		if !ok {
			return util.WrapError(ErrorSystemPackage, systemPackage)
		}
		seenSystemPackages[systemPackage] = true
	}

	if fill {
		for systemPackage := range SystemPackages {
			_, ok := seenSystemPackages[systemPackage]
			if !ok {
				cfg.Build.SystemPackages = append(cfg.Build.SystemPackages, systemPackage)
			}
		}
	}

	// Validate run comamnds
	if len(cfg.Build.Run) > 0 {
		return ErrorRunCommand
	}

	// Validate GPU
	if cfg.Build.GPU {
		return ErrorGPU
	}

	// Validate CUDA
	if cfg.Build.CUDA != "" {
		return ErrorCUDA
	}

	// Validate concurrency
	concurrency := cfg.Concurrency
	if concurrency != nil {
		if concurrency.Max > 1 {
			return ErrorConcurrency
		}
	}

	err = cfg.ValidateAndComplete(projectDir)
	if err != nil {
		return err
	}

	return nil
}

func validateRequirements(projectDir string, client *http.Client, cfg *config.Config, fill bool) error {
	if len(cfg.Build.PythonPackages) > 0 {
		return ErrorPythonPackages
	}

	requirementsFilePath, err := downloadRequirements(projectDir, client)
	if err != nil {
		return err
	}

	// Update local requirements.txt to match production before validation if filling
	if fill {
		err := updateLocalRequirementsFile(projectDir, requirementsFilePath)
		if err != nil {
			// Log warning but don't fail the build - the downloaded requirements will still be used
			console.Warn(fmt.Sprintf("Failed to update local requirements.txt: %v", err))
		}
	}

	if cfg.Build.PythonRequirements != "" {
		pipelineRequirements, err := requirements.ReadRequirements(filepath.Join(projectDir, requirementsFilePath))
		if err != nil {
			return err
		}

		projectRequirements, err := requirements.ReadRequirements(cfg.RequirementsFile(projectDir))
		if err != nil {
			return err
		}

		for _, projectRequirement := range projectRequirements {
			projectPackage := requirements.PackageName(projectRequirement)
			projectVersionSpecifier := requirements.VersionSpecifier(projectRequirement)
			// Continue in case the project does not specify a specific version
			if projectVersionSpecifier == "" {
				continue
			}
			found := false
			for _, pipelineRequirement := range pipelineRequirements {
				if pipelineRequirement == projectRequirement {
					found = true
					break
				}
				if strings.Contains(pipelineRequirement, "@") {
					continue
				}
				pipelinePackage, pipelineVersion, _, _, err := requirements.SplitPinnedPythonRequirement(pipelineRequirement)
				if err != nil {
					return err
				}
				if pipelinePackage == projectPackage {
					if pipelineVersion == "" {
						found = true
					} else {
						pipelineVersion, err := version.Parse(pipelineVersion)
						if err != nil {
							return err
						}
						specifier, err := version.NewSpecifiers(projectVersionSpecifier)
						if err != nil {
							return err
						}
						if specifier.Check(pipelineVersion) {
							found = true
							break
						}
					}
					break
				}
			}
			if !found {
				return util.WrapError(ErrorPythonPackage, projectRequirement)
			}
		}
	}

	if fill {
		cfg.Build.PythonRequirements = requirementsFilePath
	}

	return nil
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
			return "", err
		}
		defer file.Close()

		_, err = io.Copy(file, resp.Body)
		if err != nil {
			return "", err
		}
	}

	requirementsFilePath, err = filepath.Rel(projectDir, requirementsFilePath)
	if err != nil {
		return "", err
	}

	return requirementsFilePath, nil
}

// updateLocalRequirementsFile copies the downloaded requirements to the local requirements.txt file
// This keeps the local file in sync with what's actually available in the runtime
func updateLocalRequirementsFile(projectDir, downloadedRequirementsPath string) error {
	// Read the downloaded requirements
	downloadedPath := filepath.Join(projectDir, downloadedRequirementsPath)
	downloadedContent, err := os.ReadFile(downloadedPath)
	if err != nil {
		return fmt.Errorf("failed to read downloaded requirements: %w", err)
	}

	// Write to local requirements.txt
	localRequirementsPath := filepath.Join(projectDir, "requirements.txt")
	err = os.WriteFile(localRequirementsPath, downloadedContent, 0o644)
	if err != nil {
		return fmt.Errorf("failed to write local requirements.txt: %w", err)
	}

	console.Infof("Updated local requirements.txt with runtime requirements")
	return nil
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
