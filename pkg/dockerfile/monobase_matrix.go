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

type MonobaseMatrix struct {
	Id             int            `json:"id"`
	CudaVersions   []string       `json:"cuda_versions"`
	CudnnVersions  []string       `json:"cudnn_versions"`
	PythonVersions []string       `json:"python_versions"`
	TorchVersions  []string       `json:"torch_versions"`
	Venvs          []MonobaseVenv `json:"venvs"`
}

func NewMonobaseMatrix(client *http.Client) (*MonobaseMatrix, error) {
	resp, err := client.Get("https://raw.githubusercontent.com/replicate/monobase/refs/heads/main/matrix.json")
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
	if torch == "" {
		return slices.Contains(m.PythonVersions, python)
	}
	if cuda == "" {
		cuda = "cpu"
	}
	return slices.Contains(m.Venvs, MonobaseVenv{Python: python, Torch: torch, Cuda: cuda})
}
