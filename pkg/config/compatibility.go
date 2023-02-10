package config

import (
	// blank import for embeds
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sieve-data/cog/pkg/util"
	"github.com/sieve-data/cog/pkg/util/console"

	"github.com/sieve-data/cog/pkg/util/version"
)

// TODO(andreas): check tf/py versions. tf 1.5.0 didn't install on py 3.8
// TODO(andreas): support more tf versions. No matching tensorflow CPU package for version 1.15.4, etc.
// TODO(andreas): allow user to install versions that aren't compatible
// TODO(andreas): allow user to install tf cpu package on gpu

type TFCompatibility struct {
	TF           string
	TFCPUPackage string
	TFGPUPackage string
	CUDA         string
	CuDNN        string
	Pythons      []string
}

func (compat *TFCompatibility) UnmarshalJSON(data []byte) error {
	// to avoid unmarshalling stack overflow https://stackoverflow.com/questions/34859449/unmarshaljson-results-in-stack-overflow
	type tempType TFCompatibility
	c := new(tempType)
	if err := json.Unmarshal(data, c); err != nil {
		return err
	}
	cuda := version.MustVersion(c.CUDA)
	cuDNN := version.MustVersion(c.CuDNN)
	compat.TF = c.TF
	compat.TFCPUPackage = c.TFCPUPackage
	compat.TFGPUPackage = c.TFGPUPackage
	// include minor version
	compat.CUDA = fmt.Sprintf("%d.%d", cuda.Major, cuda.Minor)
	// strip cuDNN minor version to match nvidia images
	compat.CuDNN = fmt.Sprintf("%d", cuDNN.Major)
	compat.Pythons = c.Pythons
	return nil
}

type TorchCompatibility struct {
	Torch       string
	Torchvision string
	Torchaudio  string
	IndexURL    string
	CUDA        *string
	Pythons     []string
}

func (c *TorchCompatibility) TorchVersion() string {
	parts := strings.Split(c.Torch, "+")
	return parts[0]
}

func (c *TorchCompatibility) TorchvisionVersion() string {
	parts := strings.Split(c.Torchvision, "+")
	return parts[0]
}

type CUDABaseImage struct {
	Tag     string
	CUDA    string
	CuDNN   string
	IsDevel bool
	Ubuntu  string
}

func (i *CUDABaseImage) UnmarshalJSON(data []byte) error {
	var tag string
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	parts := strings.Split(tag, "-")
	if len(parts) != 4 {
		return fmt.Errorf("Tag must be in the format <cudaVersion>-cudnn<cudnnVersion>-{devel,runtime}-ubuntu<ubuntuVersion>. Invalid tag: %s", tag)
	}
	i.Tag = tag
	i.CUDA = parts[0]
	i.CuDNN = strings.Split(parts[1], "cudnn")[1]
	i.IsDevel = parts[2] == "devel"
	i.Ubuntu = strings.Split(parts[3], "ubuntu")[1]
	return nil
}

func (i *CUDABaseImage) ImageTag() string {
	return "nvidia/cuda:" + i.Tag
}

//go:generate go run ../../tools/generate_compatibility_matrices/main.go -tf-output tf_compatability_matrix.json -torch-output torch_compatability_matrix.json -cuda-images-output cuda_base_image_tags.json

//go:embed tf_compatability_matrix.json
var tfCompatibilityMatrixData []byte
var TFCompatibilityMatrix []TFCompatibility

//go:embed torch_compatability_matrix.json
var torchCompatibilityMatrixData []byte
var TorchCompatibilityMatrix []TorchCompatibility

//go:embed cuda_base_image_tags.json
var cudaBaseImageTagsData []byte
var CUDABaseImages []CUDABaseImage

func init() {
	if err := json.Unmarshal(tfCompatibilityMatrixData, &TFCompatibilityMatrix); err != nil {
		console.Fatalf("Failed to load embedded Tensorflow compatibility matrix: %s", err)
	}
	if err := json.Unmarshal(torchCompatibilityMatrixData, &TorchCompatibilityMatrix); err != nil {
		console.Fatalf("Failed to load embedded PyTorch compatibility matrix: %s", err)
	}
	if err := json.Unmarshal(cudaBaseImageTagsData, &CUDABaseImages); err != nil {
		console.Fatalf("Failed to load embedded CUDA base images: %s", err)
	}
}

func cudasFromTorch(ver string) ([]string, error) {
	cudas := []string{}
	for _, compat := range TorchCompatibilityMatrix {
		if ver == compat.TorchVersion() && compat.CUDA != nil {
			cudas = append(cudas, *compat.CUDA)
		}
	}
	return cudas, nil
}

func cudaFromTF(ver string) (cuda string, cuDNN string, err error) {
	for _, compat := range TFCompatibilityMatrix {
		if ver == compat.TF {
			return compat.CUDA, compat.CuDNN, nil
		}
	}
	return "", "", nil
}

func compatibleCuDNNsForCUDA(cuda string) []string {
	cuDNNs := []string{}
	for _, image := range CUDABaseImages {
		if image.CUDA == cuda {
			cuDNNs = append(cuDNNs, image.CuDNN)
		}
	}
	return cuDNNs
}

func defaultCUDA() string {
	return latestTF().CUDA
}

func latestCUDAFrom(cudas []string) string {
	latest := ""
	for _, cuda := range cudas {
		if latest == "" {
			latest = cuda
		} else {
			greater, err := versionGreater(cuda, latest)
			if err != nil {
				// should never happen
				panic(fmt.Sprintf("Invalid CUDA version: %s", err))
			}
			if greater {
				latest = cuda
			}
		}
	}
	return latest
}

// resolveMinorToPatch takes a minor version string (e.g. 11.1) and resolves it to its full patch version (11.1.1)
// If no patch version exists, it returns the plain old minor version (e.g. 10.3)
func resolveMinorToPatch(minor string) (string, error) {
	patch := ""
	for _, image := range CUDABaseImages {
		if version.EqualMinor(minor, image.CUDA) {
			if patch == "" || version.Greater(image.CUDA, patch) {
				patch = image.CUDA
			}
		}
	}
	if patch == "" {
		return "", fmt.Errorf("CUDA version %s could not be found", minor)
	}
	return patch, nil
}

func latestCuDNNForCUDA(cuda string) (string, error) {
	cuDNNs := []string{}
	for _, image := range CUDABaseImages {
		if version.Equal(image.CUDA, cuda) {
			cuDNNs = append(cuDNNs, image.CuDNN)
		}
	}
	sort.Slice(cuDNNs, func(i, j int) bool {
		return version.Greater(cuDNNs[i], cuDNNs[j])
	})
	if len(cuDNNs) == 0 {
		// TODO: return a list of supported cuda versions
		return "", fmt.Errorf("CUDA %s is not supported by Cog", cuda)
	}
	return cuDNNs[0], nil
}

func latestTF() TFCompatibility {
	var latest *TFCompatibility
	for _, compat := range TFCompatibilityMatrix {
		compat := compat
		if latest == nil {
			latest = &compat
		} else {
			greater, err := versionGreater(compat.TF, latest.TF)
			if err != nil {
				// should never happen
				panic(fmt.Sprintf("Invalid tensorflow version: %s", err))
			}
			if greater {
				latest = &compat
			}
		}
	}
	return *latest
}

func versionGreater(a string, b string) (bool, error) {
	// TODO(andreas): use library
	aVer, err := version.NewVersion(a)
	if err != nil {
		return false, err
	}
	bVer, err := version.NewVersion(b)
	if err != nil {
		return false, err
	}
	return aVer.Greater(bVer), nil
}

func CUDABaseImageFor(cuda string, cuDNN string) (string, error) {
	for _, image := range CUDABaseImages {
		if version.Equal(image.CUDA, cuda) && image.CuDNN == cuDNN {
			return image.ImageTag(), nil
		}
	}
	return "", fmt.Errorf("No matching base image for CUDA %s and CuDNN %s", cuda, cuDNN)
}

func tfGPUPackage(ver string, cuda string) (name string, cpuVersion string, err error) {
	for _, compat := range TFCompatibilityMatrix {
		if compat.TF == ver && version.Equal(compat.CUDA, cuda) {
			return splitPythonPackage(compat.TFGPUPackage)
		}
	}
	// We've already warned user if they're doing something stupid in ValidateAndCompleteCUDA(), so fail silently
	return "", "", nil
}

func torchCPUPackage(ver string, goos string, goarch string) (name string, cpuVersion string, indexURL string, err error) {
	for _, compat := range TorchCompatibilityMatrix {
		if compat.TorchVersion() == ver && compat.CUDA == nil {
			return "torch", torchStripCPUSuffixForM1(compat.Torch, goos, goarch), compat.IndexURL, nil
		}
	}

	// Fall back to just installing default version. For older pytorch versions, they don't have any CPU versions.
	return "torch", ver, "", nil
}

func torchGPUPackage(ver string, cuda string) (name string, cpuVersion string, indexURL string, err error) {
	// find the torch package that has the requested torch version and the latest cuda version
	// that is at most as high as the requested cuda version
	var latest *TorchCompatibility
	for _, compat := range TorchCompatibilityMatrix {
		compat := compat
		if compat.TorchVersion() != ver || compat.CUDA == nil {
			continue
		}
		greater, err := versionGreater(*compat.CUDA, cuda)
		if err != nil {
			panic(fmt.Sprintf("Invalid CUDA version: %s", err))
		}

		if greater {
			continue
		}
		if latest == nil {
			latest = &compat
		} else {
			greater, err := versionGreater(*compat.CUDA, *latest.CUDA)
			if err != nil {
				// should never happen
				panic(fmt.Sprintf("Invalid CUDA version: %s", err))
			}
			if greater {
				latest = &compat
			}
		}
	}
	if latest == nil {
		// We've already warned user if they're doing something stupid in ValidateAndCompleteCUDA()
		return "torch", ver, "", nil
	}

	return "torch", latest.Torch, latest.IndexURL, nil
}

func torchvisionCPUPackage(ver string, goos string, goarch string) (name string, cpuVersion string, indexURL string, err error) {
	for _, compat := range TorchCompatibilityMatrix {
		if compat.TorchvisionVersion() == ver && compat.CUDA == nil {
			return "torchvision", torchStripCPUSuffixForM1(compat.Torchvision, goos, goarch), compat.IndexURL, nil
		}
	}
	// Fall back to just installing default version. For older torchvision versions, they don't have any CPU versions.
	return "torchvision", ver, "", nil
}

func torchvisionGPUPackage(ver string, cuda string) (name string, cpuVersion string, indexURL string, err error) {
	// find the torchvision package that has the requested
	// torchvision version and the latest cuda version that is at
	// most as high as the requested cuda version
	var latest *TorchCompatibility
	for _, compat := range TorchCompatibilityMatrix {
		compat := compat
		if compat.TorchvisionVersion() != ver || compat.CUDA == nil {
			continue
		}
		greater, err := versionGreater(*compat.CUDA, cuda)
		if err != nil {
			panic(fmt.Sprintf("Invalid CUDA version: %s", err))
		}
		if greater {
			continue
		}
		if latest == nil {
			latest = &compat
		} else {
			greater, err := versionGreater(*compat.CUDA, *latest.CUDA)
			if err != nil {
				// should never happen
				panic(fmt.Sprintf("Invalid CUDA version: %s", err))
			}
			if greater {
				latest = &compat
			}
		}
	}
	if latest == nil {
		// TODO: can we suggest a CUDA version known to be compatible?
		console.Warnf("Cog doesn't know if CUDA %s is compatible with torchvision %s. This might cause CUDA problems.", cuda, ver)
		return "torchvision", ver, "", nil
	}

	return "torchvision", latest.Torchvision, latest.IndexURL, nil
}

// aarch64 packages don't have +cpu suffix: https://download.pytorch.org/whl/torch_stable.html
// TODO(andreas): clean up this hack by actually parsing the torch_stable.html list in the generator
func torchStripCPUSuffixForM1(version string, goos string, goarch string) string {
	// TODO(andreas): clean up this hack
	if util.IsM1Mac(goos, goarch) {
		return strings.ReplaceAll(version, "+cpu", "")
	}
	return version
}
