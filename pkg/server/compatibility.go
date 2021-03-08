package server

import (
	_ "embed"
	"encoding/json"

	log "github.com/sirupsen/logrus"
)

// Python version
type Python string

// Docker image
type DockerImage string

// CUDA version
type CUDA string

// CuDNN version
type CuDNN string

// Ubuntu version
type Ubuntu string

type TFCompatibility struct {
	TF           string
	TFCPUPackage string
	TFGPUPackage string
	CUDA         CUDA
	CuDNN        CuDNN
	Pythons      []Python
}

type TorchCompatibility struct {
	Torch       string
	Torchvision string
	Torchaudio  string
	IndexURL    string
	CUDA        *CUDA
	Pythons     []Python
}

//go:generate go run ../../cmd/generate_compatibility_matrices/main.go -tf-output tf_compatability_matrix.json -torch-output torch_compatability_matrix.json

//go:embed tf_compatability_matrix.json
var tfCompatibilityMatrixData []byte
var TFCompatibilityMatrix []TFCompatibility

//go:embed torch_compatability_matrix.json
var torchCompatibilityMatrixData []byte
var TorchCompatibilityMatrix []TorchCompatibility

func init() {
	if err := json.Unmarshal(tfCompatibilityMatrixData, &TFCompatibilityMatrix); err != nil {
		log.Fatalf("Failed to load embedded Tensorflow compatibility matrix: %w", err)
	}
	if err := json.Unmarshal(torchCompatibilityMatrixData, &TorchCompatibilityMatrix); err != nil {
		log.Fatalf("Failed to load embedded PyTorch compatibility matrix: %w", err)
	}
}
