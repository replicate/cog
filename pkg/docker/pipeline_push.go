package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/api"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/procedure"
)

func PipelinePush(ctx context.Context, image string, projectDir string, apiClient *api.Client, client *http.Client, cfg *config.Config) error {
	err := procedure.Validate(projectDir, client, cfg, true)
	if err != nil {
		return err
	}

	tarball, err := createTarball(projectDir, cfg)
	if err != nil {
		return err
	}
	return apiClient.PostNewPipeline(ctx, image, tarball)
}

func createTarball(folder string, cfg *config.Config) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	matcher, err := dockerignore.CreateMatcher(folder)
	if err != nil {
		return nil, err
	}

	// Track if we need to add downloaded requirements to the tarball
	var downloadedRequirementsPath string
	var downloadedRequirementsContent []byte

	// If config points to downloaded requirements (outside project directory),
	// we need to include them in the tarball as requirements.txt
	if cfg.Build.PythonRequirements != "" {
		reqPath := cfg.RequirementsFile(folder)
		if !strings.HasPrefix(reqPath, folder) || strings.Contains(reqPath, global.CogBuildArtifactsFolder) {
			// This is a downloaded requirements file, read its content
			content, err := os.ReadFile(reqPath)
			if err != nil {
				return nil, err
			}
			downloadedRequirementsPath = "requirements.txt"
			downloadedRequirementsContent = content
		}
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

		// If this is the local requirements.txt and we have downloaded requirements,
		// skip the local one (we'll add the downloaded version instead)
		if downloadedRequirementsPath != "" && relPath == "requirements.txt" {
			return nil
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

	// Add downloaded requirements as requirements.txt if we have them
	if downloadedRequirementsPath != "" {
		header := &tar.Header{
			Name: downloadedRequirementsPath,
			Mode: 0o644,
			Size: int64(len(downloadedRequirementsContent)),
		}

		err = tw.WriteHeader(header)
		if err != nil {
			return nil, err
		}

		_, err = tw.Write(downloadedRequirementsContent)
		if err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}
