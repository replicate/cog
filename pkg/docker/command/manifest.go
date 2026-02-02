package command

import "github.com/replicate/cog/pkg/global"

type Config struct {
	Labels map[string]string `json:"Labels"`
	Env    []string          `json:"Env"`
}

type Manifest struct {
	Config Config `json:"Config"`
}

const (
	R8CogVersionEnvVarName    = "R8_COG_VERSION"
	R8TorchVersionEnvVarName  = "R8_TORCH_VERSION"
	R8CudaVersionEnvVarName   = "R8_CUDA_VERSION"
	R8CudnnVersionEnvVarName  = "R8_CUDNN_VERSION"
	R8PythonVersionEnvVarName = "R8_PYTHON_VERSION"
)

var (
	CogConfigLabelKey          = global.LabelNamespace + "config"
	CogVersionLabelKey         = global.LabelNamespace + "version"
	CogOpenAPISchemaLabelKey   = global.LabelNamespace + "openapi_schema"
	CogWeightsManifestLabelKey = global.LabelNamespace + "r8_weights_manifest"
)
