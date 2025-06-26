package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/api"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/procedure"
)

func PipelinePush(ctx context.Context, image string, projectDir string, apiClient *api.Client, client *http.Client, cfg *config.Config) error {
	err := procedure.Validate(projectDir, client, cfg, false)
	if err != nil {
		return err
	}

	tarball, err := createTarball(projectDir)
	if err != nil {
		return err
	}
	return apiClient.PostNewPipeline(ctx, image, tarball)
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
