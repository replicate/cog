package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util"
)

type Client struct {
	dockerCommand command.Command
	client        *http.Client
}

type File struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type Env struct {
	CogGpu              string `json:"COG_GPU"`
	CogPredictTypeStub  string `json:"COG_PREDICT_TYPE_STUB"`
	CogTrainTypeStub    string `json:"COG_TRAIN_TYPE_STUB"`
	CogPredictCodeStrip string `json:"COG_PREDICT_CODE_STRIP"`
	CogTrainCodeStrip   string `json:"COG_TRAIN_CODE_STRIP"`
	CogPyEnvPath        string `json:"COG_PYENV_PATH"`
	CogEagerImports     string `json:"COG_EAGER_IMPORTS"`
	R8CudaVersion       string `json:"R8_CUDA_VERSION"`
	R8CudnnVersion      string `json:"R8_CUDNN_VERSION"`
	R8PythonVersion     string `json:"R8_PYTHON_VERSION"`
	R8TorchVersion      string `json:"R8_TORCH_VERSION"`
}

type RuntimeConfig struct {
	Weights []File `json:"weights"`
	Files   []File `json:"files"`
	Env     Env    `json:"env"`
}

type Version struct {
	Annotations   map[string]string `json:"annotations"`
	CogConfig     config.Config     `json:"cog_config"`
	CogVersion    string            `json:"cog_version"`
	OpenAPISchema map[string]any    `json:"openapi_schema"`
	RuntimeConfig RuntimeConfig     `json:"runtime_config"`
	Virtual       bool              `json:"virtual"`
}

func NewClient(dockerCommand command.Command, client *http.Client) *Client {
	return &Client{
		dockerCommand: dockerCommand,
		client:        client,
	}
}

func (c *Client) PostNewVersion(ctx context.Context, image string, weights []File, files []File) error {
	version, err := c.versionFromManifest(image, weights, files)
	if err != nil {
		return util.WrapError(err, "failed to build new version from manifest")
	}

	jsonData, err := json.Marshal(version)
	if err != nil {
		return util.WrapError(err, "failed to marshal JSON for new version")
	}

	versionUrl, err := newVersionURL(image)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, versionUrl.String(), bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return errors.New("Bad response from new version endpoint: " + strconv.Itoa(resp.StatusCode))
	}

	return nil
}

func (c *Client) versionFromManifest(image string, weights []File, files []File) (*Version, error) {
	manifest, err := c.dockerCommand.Inspect(image)
	if err != nil {
		return nil, util.WrapError(err, "failed to inspect docker image")
	}

	var cogConfig config.Config
	err = json.Unmarshal([]byte(manifest.Config.Labels[command.CogConfigLabelKey]), &cogConfig)
	if err != nil {
		return nil, util.WrapError(err, "failed to get cog config from docker image")
	}

	var openAPISchema map[string]any
	err = json.Unmarshal([]byte(manifest.Config.Labels[command.CogOpenAPISchemaLabelKey]), &openAPISchema)
	if err != nil {
		return nil, util.WrapError(err, "failed to get OpenAPI schema from docker image")
	}

	predictCode, err := stripCodeFromStub(cogConfig, true)
	if err != nil {
		return nil, err
	}
	trainCode, err := stripCodeFromStub(cogConfig, false)
	if err != nil {
		return nil, err
	}

	var cogGPU int
	if cogConfig.Build.GPU {
		cogGPU = 1
	}

	torchVersion := ""
	cudaVersion := ""
	cudnnVersion := ""
	pythonVersion := ""
	pythonPath := ""
	for _, env := range manifest.Config.Env {
		envName, envValue, found := strings.Cut(env, "=")
		if !found {
			continue
		}
		switch envName {
		case command.R8TorchVersionEnvVarName:
			torchVersion = envValue
		case command.R8CudaVersionEnvVarName:
			cudaVersion = envValue
		case command.R8CudnnVersionEnvVarName:
			cudnnVersion = envValue
		case command.R8PythonVersionEnvVarName:
			pythonVersion = envValue
		case command.UvPythonInstallDirEnvVarName:
			pythonPath = envValue
		}
	}

	eagerImports := ""
	if torchVersion != "" {
		eagerImports = "torch"
	}

	env := Env{
		CogGpu:              strconv.Itoa(cogGPU),
		CogPredictTypeStub:  cogConfig.Predict,
		CogTrainTypeStub:    cogConfig.Train,
		CogPredictCodeStrip: predictCode,
		CogTrainCodeStrip:   trainCode,
		CogPyEnvPath:        pythonPath,
		CogEagerImports:     eagerImports,
		R8CudaVersion:       cudaVersion,
		R8CudnnVersion:      cudnnVersion,
		R8PythonVersion:     pythonVersion,
		R8TorchVersion:      torchVersion,
	}

	runtimeConfig := RuntimeConfig{
		Weights: weights,
		Files:   files,
		Env:     env,
	}

	version := Version{
		Annotations:   manifest.Config.Labels,
		CogConfig:     cogConfig,
		CogVersion:    manifest.Config.Labels[command.CogVersionLabelKey],
		OpenAPISchema: openAPISchema,
		RuntimeConfig: runtimeConfig,
		Virtual:       true,
	}

	return &version, nil
}

func newVersionURL(image string) (url.URL, error) {
	imageComponents := strings.Split(image, "/")
	newVersionUrl := webBaseURL()
	if len(imageComponents) != 3 {
		return newVersionUrl, errors.New("The image URL must have 3 components in the format of " + global.ReplicateRegistryHost + "/your-username/your-model")
	}
	if imageComponents[0] != global.ReplicateRegistryHost {
		return newVersionUrl, errors.New("The image name must have the " + global.ReplicateRegistryHost + " prefix when using --x-fast.")
	}
	newVersionUrl.Path = strings.Join([]string{"", "api", "models", imageComponents[1], imageComponents[2], "versions"}, "/")
	return newVersionUrl, nil
}

func webBaseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   HostFromEnvironment(),
	}
}

func stripCodeFromStub(cogConfig config.Config, isPredict bool) (string, error) {
	var stubComponents []string
	if isPredict {
		stubComponents = strings.Split(cogConfig.Predict, ":")
	} else {
		stubComponents = strings.Split(cogConfig.Train, ":")
	}

	if len(stubComponents) < 2 {
		return "", nil
	}

	codeFile := stubComponents[0]

	b, err := os.ReadFile(codeFile)
	if err != nil {
		return "", err
	}

	// TODO: We should attempt to strip the code here, in python this is done like so:
	// from cog.code_xforms import strip_model_source_code
	// code = strip_model_source_code(
	//   util.read_file(os.path.join(fs, 'src', base_file)),
	//   [base_class],
	//   ['predict', 'train'],
	// )
	// Currently the behavior of the code strip attempts to strip, and if it can't it
	// loads the whole file in. Here we just load the whole file in.
	// We should figure out a way to call cog python from here to fulfill this.
	// It could be a good idea to do this in the layer functions where we do pip freeze
	// et al.

	return string(b), nil
}
