package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/web"
)

func PipelinePush(ctx context.Context, image string, projectDir string, webClient *web.Client) error {
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
