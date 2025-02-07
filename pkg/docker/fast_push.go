package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/web"
	"github.com/replicate/cog/pkg/weights"
)

const weightsObjectType = "weights"
const filesObjectType = "files"
const requirementsTarFile = "requirements.tar.zst"

func FastPush(ctx context.Context, image string, projectDir string, command command.Command, webClient *web.Client, monobeamClient *monobeam.Client) error {
	g, _ := errgroup.WithContext(ctx)

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	weights, err := weights.ReadFastWeights(tmpDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	// Upload weights
	for _, weight := range weights {
		g.Go(func() error {
			return monobeamClient.UploadFile(ctx, weightsObjectType, weight.Digest, weight.Path)
		})
	}

	// Create a list of files to upload.
	files := []web.File{}

	aptTarFile, err := CurrentAptTarball(tmpDir)
	if err != nil {
		return fmt.Errorf("current apt tarball error: %w", err)
	}
	// Upload apt tar file
	if aptTarFile != "" {
		hash, err := util.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}
		file, err := createFile(aptTarFile, hash)
		if err != nil {
			return err
		}
		files = append(files, *file)
		g.Go(func() error {
			return monobeamClient.UploadFile(ctx, filesObjectType, hash, aptTarFile)
		})
	}

	requirementsFile, err := requirements.CurrentRequirements(tmpDir)
	if err != nil {
		return err
	}
	// Upload python packages.
	if requirementsFile != "" {
		pythonTar, err := createPythonPackagesTarFile(image, tmpDir, command)
		if err != nil {
			return err
		}

		hash, err := util.SHA256HashFile(pythonTar)
		if err != nil {
			return err
		}

		file, err := createFile(pythonTar, hash)
		if err != nil {
			return err
		}
		files = append(files, *file)

		g.Go(func() error {
			return monobeamClient.UploadFile(ctx, filesObjectType, hash, pythonTar)
		})
	} else {
		requirementsTarFile := filepath.Join(tmpDir, requirementsTarFile)
		_, err = os.Stat(requirementsTarFile)
		if !errors.Is(err, os.ErrNotExist) {
			err = os.Remove(requirementsTarFile)
			if err != nil {
				return err
			}
		}
	}

	// Upload user /src.
	srcTar, err := createSrcTarFile(image, tmpDir, command)
	if err != nil {
		return fmt.Errorf("create src tarfile: %w", err)
	}
	hash, err := util.SHA256HashFile(srcTar)
	if err != nil {
		return err
	}
	file, err := createFile(srcTar, hash)
	if err != nil {
		return err
	}
	files = append(files, *file)
	g.Go(func() error {
		return monobeamClient.UploadFile(ctx, filesObjectType, hash, srcTar)
	})

	// Wait for uploads
	err = g.Wait()
	if err != nil {
		return err
	}

	// Tell replicate about our new version
	return webClient.PostNewVersion(ctx, image, createWeightsFilesFromWeightsManifest(weights), files)
}

func createPythonPackagesTarFile(image string, tmpDir string, command command.Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, requirementsTarFile, "root/.venv")
}

func createSrcTarFile(image string, tmpDir string, command command.Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, "src.tar.zst", "src")
}

func createWeightsFilesFromWeightsManifest(weights []weights.Weight) []web.File {
	weightsFiles := []web.File{}
	for _, weight := range weights {
		file := web.File{
			Path:   weight.Path,
			Digest: weight.Digest,
			Size:   weight.Size,
		}
		weightsFiles = append(weightsFiles, file)
	}
	return weightsFiles
}

func createFile(path string, digest string) (*web.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	webFile := web.File{
		Path:   path,
		Digest: digest,
		Size:   fileInfo.Size(),
	}
	return &webFile, nil
}
