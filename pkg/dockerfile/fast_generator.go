package dockerfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/weights"
)

const MONOBASE_IMAGE = "r8.im/monobase:latest"
const FUSE_RPC_WEIGHTS_PATH = "/srv/r8/fuse-rpc/weights/sha256"
const MONOBASE_CACHE_DIR = "/var/cache/monobase"
const MONOBASE_CACHE_MOUNT = "--mount=type=cache,target=" + MONOBASE_CACHE_DIR + ",id=monobase-cache"
const UV_CACHE_DIR = "/srv/r8/monobase/uv/cache"
const UV_CACHE_MOUNT = "--mount=type=cache,target=" + UV_CACHE_DIR + ",id=uv-cache"
const FAST_GENERATOR_NAME = "FAST_GENERATOR"

const contextBuildDir = "context"
const aptBuildContextName = "apt"
const monobaseBuildContextName = "monobase"
const requirementsBuildContextName = "requirements"
const srcBuildContextName = "src"
const buildTmpDir = "/buildtmp"

var srcBuildDir = filepath.Join(contextBuildDir, "src")
var aptBuildDir = filepath.Join(contextBuildDir, "apt")
var monobaseBuildDir = filepath.Join(contextBuildDir, "monobase")
var requirementsBuildDir = filepath.Join(contextBuildDir, "requirements")

type FastGenerator struct {
	Config        *config.Config
	Dir           string
	dockerCommand command.Command
	matrix        MonobaseMatrix
	localImage    bool
}

type MonobaseVenv struct {
	Python string `json:"python"`
	Torch  string `json:"torch"`
	Cuda   string `json:"cuda"`
}

func NewFastGenerator(config *config.Config, dir string, dockerCommand command.Command, matrix *MonobaseMatrix, localImage bool) (*FastGenerator, error) {
	return &FastGenerator{
		Config:        config,
		Dir:           dir,
		dockerCommand: dockerCommand,
		matrix:        *matrix,
		localImage:    localImage,
	}, nil
}

func (g *FastGenerator) GenerateInitialSteps() (string, error) {
	return "", errors.New("GenerateInitialSteps not supported in FastGenerator")
}

func (g *FastGenerator) BaseImage() (string, error) {
	return "", errors.New("BaseImage not supported in FastGenerator")
}

func (g *FastGenerator) Cleanup() error {
	return nil
}

func (g *FastGenerator) GenerateDockerfileWithoutSeparateWeights() (string, error) {
	return g.generate()
}

func (g *FastGenerator) GenerateModelBase() (string, error) {
	return "", errors.New("GenerateModelBase not supported in FastGenerator")
}

func (g *FastGenerator) GenerateModelBaseWithSeparateWeights(imageName string) (weightsBase string, dockerfile string, dockerignoreContents string, err error) {
	return "", "", "", errors.New("GenerateModelBaseWithSeparateWeights not supported in FastGenerator")
}

func (g *FastGenerator) GenerateWeightsManifest() (*weights.Manifest, error) {
	return nil, errors.New("GenerateWeightsManifest not supported in FastGenerator")
}

func (g *FastGenerator) IsUsingCogBaseImage() bool {
	return false
}

func (g *FastGenerator) SetPrecompile(precompile bool) {
}

func (g *FastGenerator) SetStrip(strip bool) {
}

func (g *FastGenerator) SetUseCogBaseImage(useCogBaseImage bool) {
}

func (g *FastGenerator) SetUseCogBaseImagePtr(useCogBaseImage *bool) {
}

func (g *FastGenerator) SetUseCudaBaseImage(argumentValue string) {
}

func (g *FastGenerator) Name() string {
	return FAST_GENERATOR_NAME
}

func (g *FastGenerator) BuildDir() (string, error) {
	if !g.localImage {
		return StandardBuildDirectory, nil
	}
	contextDir, err := BuildCogTempDir(g.Dir, contextBuildDir)
	if err != nil {
		return "", err
	}
	return contextDir, nil
}

func (g *FastGenerator) BuildContexts() (map[string]string, error) {
	aptDir, err := BuildCogTempDir(g.Dir, aptBuildDir)
	if err != nil {
		return nil, err
	}
	monobaseDir, err := BuildCogTempDir(g.Dir, monobaseBuildDir)
	if err != nil {
		return nil, err
	}
	requirementsDir, err := BuildCogTempDir(g.Dir, requirementsBuildDir)
	if err != nil {
		return nil, err
	}
	srcDir, err := BuildCogTempDir(g.Dir, srcBuildDir)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		aptBuildContextName:          aptDir,
		monobaseBuildContextName:     monobaseDir,
		requirementsBuildContextName: requirementsDir,
		srcBuildContextName:          srcDir,
	}, nil
}

func (g *FastGenerator) generate() (string, error) {
	err := g.validateConfig()
	if err != nil {
		return "", err
	}

	// Always pull latest monobase as we rely on it for build logic
	if err := g.dockerCommand.Pull(MONOBASE_IMAGE); err != nil {
		return "", err
	}

	// Temp directories are used as bind mounts in docker build
	// Separate them so that changes in one layer doesn't invalidate everything else

	// Weights layer
	// Includes file metadata, triggered by weights file changes
	tmpWeightsDir, err := BuildCogTempDir(g.Dir, "weights")
	if err != nil {
		return "", err
	}
	weights, err := weights.FindFastWeights(g.Dir, tmpWeightsDir)
	if err != nil {
		return "", err
	}

	// APT layer
	// Includes a tarball extracted from APT packages, triggered by system_packages changes
	tmpAptDir, err := BuildCogTempDir(g.Dir, aptBuildDir)
	if err != nil {
		return "", err
	}
	aptTarFile, err := g.generateAptTarball(tmpAptDir)
	if err != nil {
		return "", fmt.Errorf("generate apt tarball: %w", err)
	}

	// Monobase layer
	// Includes an ENV file, triggered by Python, Torch, or CUDA version changes
	tmpMonobaseDir, err := BuildCogTempDir(g.Dir, monobaseBuildDir)
	if err != nil {
		return "", err
	}
	lines := []string{}
	lines, err = g.generateMonobase(lines, tmpMonobaseDir)
	if err != nil {
		return "", err
	}

	// User layer
	// Includes requirements.txt, triggered by python_requirements changes
	tmpRequirementsDir, err := BuildCogTempDir(g.Dir, requirementsBuildDir)
	if err != nil {
		return "", err
	}

	lines, err = g.copyWeights(lines, weights)
	if err != nil {
		return "", err
	}

	lines, err = g.installApt(lines, aptTarFile)
	if err != nil {
		return "", err
	}

	lines, err = g.installPython(lines, tmpRequirementsDir)
	if err != nil {
		return "", err
	}

	lines, err = g.installSrc(lines, weights)
	if err != nil {
		return "", err
	}

	lines, err = g.entrypoint(lines)
	if err != nil {
		return "", err
	}

	return strings.Join(lines, "\n"), nil
}

func (g *FastGenerator) generateMonobase(lines []string, tmpDir string) ([]string, error) {
	var envs []string
	envs = append(envs, []string{
		// This installs latest version of coglet
		"ENV R8_COG_VERSION=coglet",
	}...)

	if g.Config.Build.GPU {
		cudaVersion := g.Config.Build.CUDA
		cudnnVersion := g.Config.Build.CuDNN
		if cudnnVersion == "" {
			cudnnVersion = g.matrix.DefaultCudnnVersion()
		}
		if !CheckMajorMinorOnly(cudaVersion) {
			return nil, fmt.Errorf("CUDA version must be <major>.<minor>, supported versions: %s", strings.Join(g.matrix.CudaVersions, ", "))
		}
		if !CheckMajorOnly(cudnnVersion) {
			return nil, fmt.Errorf("CUDNN version must be <major> only, supported versions: %s", strings.Join(g.matrix.CudnnVersions, ", "))
		}

		envs = append(envs, []string{
			"ENV R8_CUDA_VERSION=" + cudaVersion,
			"ENV R8_CUDNN_VERSION=" + cudnnVersion,
			"ENV R8_CUDA_PREFIX=https://monobase-packages.replicate.delivery/cuda",
			"ENV R8_CUDNN_PREFIX=https://monobase-packages.replicate.delivery/cudnn",
		}...)
	}

	if !CheckMajorMinorOnly(g.Config.Build.PythonVersion) {
		return nil, fmt.Errorf(
			"Python version must be <major>.<minor>, supported versions: %s", strings.Join(g.matrix.PythonVersions, ", "))
	}
	envs = append(envs, []string{
		"ENV R8_PYTHON_VERSION=" + g.Config.Build.PythonVersion,
	}...)

	torchVersion, ok := g.Config.TorchVersion()
	if ok {
		if !CheckMajorMinorPatch(torchVersion) {
			return nil, fmt.Errorf("Torch version must be <major>.<minor>.<patch>: %s", strings.Join(g.matrix.TorchVersions, ", "))
		}
		envs = append(envs, []string{
			"ENV R8_TORCH_VERSION=" + torchVersion,
		}...)
	}

	if !g.matrix.IsSupported(g.Config.Build.PythonVersion, torchVersion, g.Config.Build.CUDA) {
		return nil, fmt.Errorf(
			"Unsupported version combination: Python=%s, Torch=%s, CUDA=%s",
			g.Config.Build.PythonVersion, torchVersion, g.Config.Build.CUDA)
	}

	// The only input to monobase.build are these ENV vars
	// Write them in tmp mount for layer caching
	err := files.WriteIfDifferent(path.Join(tmpDir, "env.txt"), strings.Join(envs, "\n"))
	if err != nil {
		return nil, err
	}

	lines = append(lines, []string{
		"# syntax=docker/dockerfile:1-labs",
		"FROM " + MONOBASE_IMAGE,
	}...)
	lines = append(lines, envs...)
	lines = append(lines, []string{
		"RUN " + strings.Join([]string{
			"--mount=from=" + monobaseBuildContextName + ",target=" + buildTmpDir,
			MONOBASE_CACHE_MOUNT,
			UV_CACHE_MOUNT,
		}, " ") + " UV_CACHE_DIR=\"" + UV_CACHE_DIR + "\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --mini --cache=" + MONOBASE_CACHE_DIR,
	}...)
	return lines, nil
}

func (g *FastGenerator) copyWeights(lines []string, weightsInfo []weights.Weight) ([]string, error) {
	if len(weightsInfo) == 0 {
		return lines, nil
	}

	if g.localImage {
		weightPaths := []weights.WeightManifest{}
		for _, weight := range weightsInfo {
			weightPathAbs, err := filepath.Abs(weight.Path)
			if err != nil {
				return lines, err
			}
			weightPaths = append(weightPaths, weights.WeightManifest{
				Source:      weightPathAbs,
				Destination: weight.Path,
			})
		}
		jsonBytes, err := json.Marshal(weightPaths)
		if err != nil {
			return lines, err
		}
		escapedJSON := strings.ReplaceAll(string(jsonBytes), `"`, `\"`)
		lines = append(lines, "LABEL "+command.CogWeightsManifestLabelKey+"=\""+escapedJSON+"\"")
	} else {
		for _, weight := range weightsInfo {
			lines = append(lines, "COPY --link \""+weight.Path+"\" \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest)+"\"")
		}
	}

	return lines, nil
}

func (g *FastGenerator) installApt(lines []string, aptTarFile string) ([]string, error) {
	// Install apt packages

	if aptTarFile != "" {
		lines = append(lines, "RUN --mount=from="+aptBuildContextName+",target="+buildTmpDir+" tar -xf \""+filepath.Join(buildTmpDir, aptTarFile)+"\" -C /")
	}
	return lines, nil
}

func (g *FastGenerator) installPython(lines []string, tmpDir string) ([]string, error) {
	// Install python packages
	if len(g.Config.Build.PythonPackages) > 0 {
		return nil, fmt.Errorf("python_packages is no longer supported, use python_requirements instead")
	}
	// No Python requirements
	if g.Config.Build.PythonRequirements == "" {
		return lines, nil
	}

	requirementsFile, err := requirements.GenerateRequirements(tmpDir, g.Config.Build.PythonRequirements)
	if err != nil {
		return nil, err
	}
	if requirementsFile != "" {
		lines = append(lines, "RUN "+strings.Join([]string{
			"--mount=from=" + requirementsBuildContextName + ",target=/buildtmp",
			"--mount=from=" + srcBuildContextName + ",target=/src",
			UV_CACHE_MOUNT,
		}, " ")+" cd /src && UV_CACHE_DIR=\""+UV_CACHE_DIR+"\" UV_LINK_MODE=copy UV_COMPILE_BYTECODE=0 /opt/r8/monobase/run.sh monobase.user --requirements=/buildtmp/requirements.txt")
	}
	return lines, nil
}

func (g *FastGenerator) installSrc(lines []string, weights []weights.Weight) ([]string, error) {
	// Install /src

	srcDir, err := BuildCogTempDir(g.Dir, srcBuildDir)
	if err != nil {
		return nil, err
	}

	// Rsync local src with our srcdir
	if g.localImage {
		err := g.rsyncSrc(srcDir, weights)
		if err != nil {
			return nil, err
		}
	}

	// Copy over source / without weights
	if !g.localImage {
		copyCommand := "COPY --link --exclude='.cog' "
		for _, weight := range weights {
			copyCommand += "--exclude='" + weight.Path + "' "
		}
		copyCommand += ". /src"
		lines = append(lines, copyCommand)
	} else {
		buildDir, err := g.BuildDir()
		if err != nil {
			return nil, err
		}
		relSrcDir, err := filepath.Rel(buildDir, srcDir)
		if err != nil {
			return nil, err
		}
		copyCommand := "COPY --link " + relSrcDir + "/. /src"
		lines = append(lines, copyCommand)
	}

	// Link to weights
	// If it is a local image we do this with a runtime mount instead to make builds faster.
	if len(weights) > 0 && !g.localImage {
		linkCommands := []string{}
		for _, weight := range weights {
			linkCommands = append(linkCommands, "ln -s \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest)+"\" \"/src/"+weight.Path+"\"")
		}
		lines = append(lines, "RUN "+strings.Join(linkCommands, " && "))
	}

	return lines, nil
}

func (g *FastGenerator) entrypoint(lines []string) ([]string, error) {
	return append(lines, []string{
		"WORKDIR /src",
		"ENV VERBOSE=0",
		"ENTRYPOINT [\"/usr/bin/tini\", \"--\", \"/opt/r8/monobase/exec.sh\"]",
		"CMD [\"python\", \"-m\", \"cog.server.http\"]",
	}...), nil
}

func (g *FastGenerator) generateAptTarball(tmpDir string) (string, error) {
	return docker.CreateAptTarball(tmpDir, g.dockerCommand, g.Config.Build.SystemPackages...)
}

func (g *FastGenerator) validateConfig() error {
	if len(g.Config.Build.Run) > 0 {
		return errors.New("cog builds with --x-fast do not support build run commands.")
	}
	return nil
}

func (g *FastGenerator) rsyncSrc(srcDir string, weights []weights.Weight) error {
	matcher, err := dockerignore.CreateMatcher(g.Dir)
	if err != nil {
		return err
	}

	relPath, err := filepath.Rel(g.Dir, srcDir)
	if err != nil {
		return err
	}

	// Find files we haven't copied over yet.
	usedFiles := make(map[string]bool)
	usedFiles[relPath] = true
	err = filepath.Walk(g.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if matcher != nil && matcher.MatchesPath(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() && info.Name() == CogBuildArtifactsFolder {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(g.Dir, path)
		if err != nil {
			return err
		}

		// Skip weights, we handle them separately
		for _, weight := range weights {
			if weight.Path == relPath {
				return nil
			}
		}

		copyPath := filepath.Join(srcDir, relPath)
		err = ensureFSObjectExists(copyPath, path)
		if err != nil {
			return err
		}
		usedFiles[relPath] = true
		return nil
	})
	if err != nil {
		return err
	}

	// Remove files that we no longer need in our tmp dir.
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		_, ok := usedFiles[relPath]
		if !ok {
			console.Debug("Deleting " + relPath)
			err = os.RemoveAll(path)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func linkFile(destination string, src string) error {
	console.Debug("Linking " + destination + " to " + src)

	fileInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	// If we are a symlink, link to the original target
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		destination, err = os.Readlink(src)
		if err != nil {
			return err
		}
	}

	err = os.Link(src, destination)
	if err != nil {
		return err
	}
	return nil
}

func ensureFSObjectExists(destination string, src string) error {
	exists, err := files.Exists(destination)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := info.Mode()

	if !exists {
		switch {
		case mode.IsDir():
			err := os.Mkdir(destination, mode.Perm())
			if err != nil {
				return err
			}
		case mode.IsRegular():
			err := linkFile(destination, src)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
