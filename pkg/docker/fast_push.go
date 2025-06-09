package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/vbauerster/mpb/v8"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/monobeam"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/web"
	"github.com/replicate/cog/pkg/weights"
)

var TarballsDir = filepath.Join(global.CogBuildArtifactsFolder, "tarballs")

const weightsObjectType = "weights"
const filesObjectType = "files"
const requirementsTarFile = "requirements.tar.zst"

func FastPush(ctx context.Context, image string, projectDir string, command command.Command, webClient *web.Client, monobeamClient *monobeam.Client) error {
	g, _ := errgroup.WithContext(ctx)
	p := mpb.New(
		mpb.WithRefreshRate(180 * time.Millisecond),
	)

	// Reading weights metadata only
	tmpWeightsDir := filepath.Join(projectDir, ".cog", "tmp", "weights")
	weights, err := weights.ReadFastWeights(tmpWeightsDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	// Upload weights
	for _, weight := range weights {
		g.Go(func() error {
			return monobeamClient.UploadFile(ctx, weightsObjectType, weight.Digest, weight.Path, p, "weights - "+filepath.Base(weight.Path))
		})
	}

	// Create a list of files to upload.
	files := []web.File{}

	aptBuildDir, err := dockercontext.CogTempDir(projectDir, dockercontext.AptBuildDir)
	if err != nil {
		return err
	}
	aptTarFile, err := CurrentAptTarball(aptBuildDir)
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
			return monobeamClient.UploadFile(ctx, filesObjectType, hash, aptTarFile, p, "apt")
		})
	}

	tmpRequirementsDir, err := dockercontext.CogTempDir(projectDir, dockercontext.RequirementsBuildDir)
	if err != nil {
		return err
	}
	requirementsFile, err := requirements.CurrentRequirements(tmpRequirementsDir)
	if err != nil {
		return err
	}

	// Temp directory for tarballs extracted from the image
	// Separate from other temp directories so that they don't cause cache invalidation
	tmpTarballsDir := filepath.Join(projectDir, TarballsDir)
	err = os.MkdirAll(tmpTarballsDir, 0o755)
	if err != nil {
		return err
	}
	// Upload python packages.
	if requirementsFile != "" {
		pythonTar, err := createPythonPackagesTarFile(ctx, image, tmpTarballsDir, command)
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
			return monobeamClient.UploadFile(ctx, filesObjectType, hash, pythonTar, p, "python-packages")
		})
	} else {
		requirementsTarFile := filepath.Join(tmpTarballsDir, requirementsTarFile)
		_, err = os.Stat(requirementsTarFile)
		if !errors.Is(err, os.ErrNotExist) {
			err = os.Remove(requirementsTarFile)
			if err != nil {
				return err
			}
		}
	}

	// Upload user /src.
	srcTar, err := createSrcTarFile(ctx, image, tmpTarballsDir, command)
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
		return monobeamClient.UploadFile(ctx, filesObjectType, hash, srcTar, p, "src")
	})

	// Wait for uploads
	err = g.Wait()
	if err != nil {
		return err
	}

	weightFiles := createWeightsFilesFromWeightsManifest(weights)

	// Initiate and do file challenges for each file in the config
	challenges, err := webClient.InitiateAndDoFileChallenge(ctx, weightFiles, files)
	if err != nil {
		return util.WrapError(err, "initiate and do file challenges")
	}

	// Tell replicate about our new version
	return webClient.PostNewVersion(ctx, image, weightFiles, files, challenges)
}

func createPythonPackagesTarFile(ctx context.Context, image string, tmpDir string, command command.Command) (string, error) {
	return CreateTarFile(ctx, command, image, tmpDir, requirementsTarFile, "root/.venv")
}

func createSrcTarFile(ctx context.Context, image string, tmpDir string, command command.Command) (string, error) {
	return CreateTarFile(ctx, command, image, tmpDir, "src.tar.zst", "src")
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
