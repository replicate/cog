package config

import (
	// blank import for embeds
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"

	"github.com/replicate/cog/pkg/util/version"
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
	Torch         string
	Torchvision   string
	Torchaudio    string
	FindLinks     string
	ExtraIndexURL string
	CUDA          *string
	Pythons       []string
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

func (i *CUDABaseImage) ImageTag() string {
	return "nvidia/cuda:" + i.Tag
}

//go:generate go run ../../tools/compatgen/main.go cuda -o cuda_base_images.json
//go:embed cuda_base_images.json
var cudaBaseImagesData []byte
var CUDABaseImages []CUDABaseImage

//go:generate go run ../../tools/compatgen/main.go tensorflow -o tf_compatibility_matrix.json
//go:embed tf_compatibility_matrix.json
var tfCompatibilityMatrixData []byte
var TFCompatibilityMatrix []TFCompatibility

//go:generate go run ../../tools/compatgen/main.go torch -o torch_compatibility_matrix.json
//go:embed torch_compatibility_matrix.json
var torchCompatibilityMatrixData []byte
var TorchCompatibilityMatrix []TorchCompatibility

// For minor Torch versions we use the latest patch version.
// This is semantically different to pip, which uses the .0
// patch version.
var TorchMinorCompatibilityMatrix []TorchCompatibility

func init() {
	if err := json.Unmarshal(cudaBaseImagesData, &CUDABaseImages); err != nil {
		console.Fatalf("Failed to load embedded CUDA base images: %s", err)
	}

	if err := json.Unmarshal(tfCompatibilityMatrixData, &TFCompatibilityMatrix); err != nil {
		console.Fatalf("Failed to load embedded Tensorflow compatibility matrix: %s", err)
	}

	var torchCompatibilityMatrix []TorchCompatibility
	if err := json.Unmarshal(torchCompatibilityMatrixData, &torchCompatibilityMatrix); err != nil {
		console.Fatalf("Failed to load embedded PyTorch compatibility matrix: %s", err)
	}
	filteredTorchCompatibilityMatrix := []TorchCompatibility{}
	for _, compat := range torchCompatibilityMatrix {
		for _, cudaBaseImage := range CUDABaseImages {
			if compat.CUDA == nil || version.Matches(*compat.CUDA, cudaBaseImage.CUDA) {
				filteredTorchCompatibilityMatrix = append(filteredTorchCompatibilityMatrix, compat)
				break
			}
		}
	}
	TorchCompatibilityMatrix = filteredTorchCompatibilityMatrix
	TorchMinorCompatibilityMatrix = generateTorchMinorVersionCompatibilityMatrix(TorchCompatibilityMatrix)
}

func generateTorchMinorVersionCompatibilityMatrix(matrix []TorchCompatibility) []TorchCompatibility {
	minorMatrix := []TorchCompatibility{}

	// First sort compatibilities by Torch version descending
	matrixByTorchDesc := make([]TorchCompatibility, len(matrix))
	copy(matrixByTorchDesc, matrix)
	sort.Slice(matrixByTorchDesc, func(i, j int) bool {
		return version.Greater(matrixByTorchDesc[i].Torch, matrixByTorchDesc[j].Torch)
	})

	// Then pick CUDA for the most recent patch versions
	seenCUDATorchMinor := make(map[[2]string]bool)

	for _, compat := range matrixByTorchDesc {
		cudaString := ""
		if compat.CUDA != nil {
			cudaString = *compat.CUDA
		}
		torchMinor := version.StripPatch(compat.Torch)
		key := [2]string{cudaString, torchMinor}

		if seen := seenCUDATorchMinor[key]; !seen {
			minorMatrix = append(minorMatrix, TorchCompatibility{
				Torch:         torchMinor,
				CUDA:          compat.CUDA,
				Pythons:       compat.Pythons,
				Torchvision:   compat.Torchvision,
				Torchaudio:    compat.Torchaudio,
				FindLinks:     compat.FindLinks,
				ExtraIndexURL: compat.ExtraIndexURL,
			})
			seenCUDATorchMinor[key] = true
		}
	}
	return minorMatrix

}

func cudasFromTorch(ver string) ([]string, error) {
	cudas := []string{}
	for _, compat := range TorchCompatibilityMatrix {
		if ver == compat.TorchVersion() && compat.CUDA != nil {
			cudas = append(cudas, *compat.CUDA)
		}
	}
	slices.Sort(cudas)
	for _, compat := range TorchMinorCompatibilityMatrix {
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
	// TODO: change this to latestTF().CUDA once replicate supports >= 12 everywhere
	return "11.8"
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

func latestCuDNNForCUDA(cuda string) (string, error) {
	cuDNNs := []string{}
	for _, image := range CUDABaseImages {
		if version.Matches(cuda, image.CUDA) {
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
	var images []CUDABaseImage
	for _, image := range CUDABaseImages {
		if version.Matches(cuda, image.CUDA) && image.CuDNN == cuDNN {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		return "", fmt.Errorf("No matching base image for CUDA %s and CuDNN %s", cuda, cuDNN)
	}

	sort.Slice(images, func(i, j int) bool {
		if images[i].CUDA != images[j].CUDA {
			return version.MustVersion(images[i].CUDA).Greater(version.MustVersion(images[j].CUDA))
		}
		return images[i].Ubuntu > images[j].Ubuntu
	})

	return images[0].ImageTag(), nil
}

func tfGPUPackage(ver string, cuda string) (name string, cpuVersion string, err error) {
	for _, compat := range TFCompatibilityMatrix {
		if compat.TF == ver && version.Equal(compat.CUDA, cuda) {
			name, cpuVersion, _, _, err = splitPinnedPythonRequirement(compat.TFGPUPackage)
			return name, cpuVersion, err
		}
	}
	// We've already warned user if they're doing something stupid in validateAndCompleteCUDA(), so fail silently
	return "", "", nil
}

func torchCPUPackage(ver, goos, goarch string) (name, cpuVersion, findLinks, extraIndexURL string, err error) {
	for _, compat := range TorchCompatibilityMatrix {
		if compat.TorchVersion() == ver && compat.CUDA == nil {
			return "torch", torchStripCPUSuffixForM1(compat.Torch, goos, goarch), compat.FindLinks, compat.ExtraIndexURL, nil
		}
	}

	// Fall back to just installing default version. For older pytorch versions, they don't have any CPU versions.
	return "torch", ver, "", "", nil
}

func torchGPUPackage(ver string, cuda string) (name, cpuVersion, findLinks, extraIndexURL string, err error) {
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
		// We've already warned user if they're doing something stupid in validateAndCompleteCUDA()
		return "torch", ver, "", "", nil
	}

	return "torch", latest.Torch, latest.FindLinks, latest.ExtraIndexURL, nil
}

func torchvisionCPUPackage(ver, goos, goarch string) (name, cpuVersion, findLinks, extraIndexURL string, err error) {
	for _, compat := range TorchCompatibilityMatrix {
		if compat.TorchvisionVersion() == ver && compat.CUDA == nil {
			return "torchvision", torchStripCPUSuffixForM1(compat.Torchvision, goos, goarch), compat.FindLinks, compat.ExtraIndexURL, nil
		}
	}
	// Fall back to just installing default version. For older torchvision versions, they don't have any CPU versions.
	return "torchvision", ver, "", "", nil
}

func torchvisionGPUPackage(ver, cuda string) (name, cpuVersion, findLinks, extraIndexURL string, err error) {
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
		return "torchvision", ver, "", "", nil
	}

	return "torchvision", latest.Torchvision, latest.FindLinks, latest.ExtraIndexURL, nil
}

// aarch64 packages don't have +cpu suffix: https://download.pytorch.org/whl/torch_stable.html
// TODO(andreas): clean up this hack by actually parsing the torch_stable.html list in the generator
func torchStripCPUSuffixForM1(version string, goos string, goarch string) string {
	// TODO(andreas): clean up this hack
	if util.IsAppleSiliconMac(goos, goarch) {
		return strings.ReplaceAll(version, "+cpu", "")
	}
	return version
}
