package dockerfile

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type MonobasePackage struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type MonobaseMatrix struct {
	Id               int                 `json:"id"`
	CudaVersions     []string            `json:"cuda_versions"`
	CudnnVersions    []string            `json:"cudnn_versions"`
	PythonVersions   []string            `json:"python_versions"`
	TorchVersions    []string            `json:"torch_versions"`
	Venvs            []MonobaseVenv      `json:"venvs"`
	TorchCUDAs       map[string][]string `json:"torch_cudas"`
	LatestCoglet     MonobasePackage     `json:"latest_coglet"`
	LatestHFTransfer MonobasePackage     `json:"latest_hf_transfer"`
}

func NewMonobaseMatrix(client *http.Client) (*MonobaseMatrix, error) {
	url := "https://monobase-packages.replicate.delivery/matrix.json"
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Failed to fetch Monobase support matrix")
	}
	var matrix MonobaseMatrix
	if err := json.NewDecoder(resp.Body).Decode(&matrix); err != nil {
		return nil, err
	}
	return &matrix, nil
}

func (m MonobaseMatrix) DefaultCudnnVersion() string {
	slices.SortFunc(m.CudnnVersions, func(s1, s2 string) int {
		i1, e1 := strconv.Atoi(s1)
		i2, e2 := strconv.Atoi(s2)
		if e1 != nil || e2 != nil {
			return strings.Compare(s1, s2)
		}
		return cmp.Compare(i1, i2)
	})
	return m.CudnnVersions[len(m.CudnnVersions)-1]
}

func (m MonobaseMatrix) IsSupported(python string, torch string, cuda string) bool {
	if python == "3.8" {
		// coglet does not support Python 3.8, so we cannot use it for fast-push
		// even though it's in the matrix for older models
		return false
	}
	if torch == "" {
		return slices.Contains(m.PythonVersions, python)
	}
	if cuda == "" {
		cuda = "cpu"
	}
	return slices.Contains(m.Venvs, MonobaseVenv{Python: python, Torch: torch, Cuda: cuda})
}

func (m MonobaseMatrix) DefaultCUDAVersion(torch string) string {
	cudas := m.TorchCUDAs[torch]
	return cudas[len(cudas)-1]
}
