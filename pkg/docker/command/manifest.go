package command

import "github.com/replicate/cog/pkg/global"

type Config struct {
	Labels map[string]string `json:"Labels"`
	Env    []string          `json:"Env"`
}

type Manifest struct {
	Config Config `json:"Config"`
	ID     string `json:"Id"`
}

const UvPythonInstallDirEnvVarName = "UV_PYTHON_INSTALL_DIR"
const R8TorchVersionEnvVarName = "R8_TORCH_VERSION"
const R8CudaVersionEnvVarName = "R8_CUDA_VERSION"
const R8CudnnVersionEnvVarName = "R8_CUDNN_VERSION"
const R8PythonVersionEnvVarName = "R8_PYTHON_VERSION"

var CogConfigLabelKey = global.LabelNamespace + "config"
var CogVersionLabelKey = global.LabelNamespace + "version"
var CogOpenAPISchemaLabelKey = global.LabelNamespace + "openapi_schema"
