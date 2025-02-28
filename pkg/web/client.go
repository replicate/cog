package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/replicate/go/types"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util"
)

const (
	pushStartURLPath         = "/api/models/push-start"
	initiateChallengeURLPath = "/api/initiate-file-challenge"
)

var (
	ErrorBadResponseNewVersionEndpoint        = errors.New("Bad response from new version endpoint")
	ErrorBadResponsePushStartEndpoint         = errors.New("Bad response from push start endpoint")
	ErrorBadResponseInitiateChallengeEndpoint = errors.New("Bad response from initate file challenge endpoint")
	ErrorBadRegistryURL                       = errors.New("The image URL must have 3 components in the format of " + global.ReplicateRegistryHost + "/your-username/your-model")
	ErrorBadRegistryHost                      = errors.New("The image name must have the " + global.ReplicateRegistryHost + " prefix when using --x-fast.")
	ErrorNoSuchDigest                         = errors.New("No digest submitted matches the digest requested")
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
	R8CogVersion        string `json:"R8_COG_VERSION"`
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
	PushID        string            `json:"push_id"`
}

type FileChallenge struct {
	Salt   string `json:"salt"`
	Start  int    `json:"byte_start"`
	End    int    `json:"byte_end"`
	Digest string `json:"digest"`
}

type FileChallengeAnswer struct {
	Digest string `json:"digest"`
	Hash   string `json:"hash"`
}

type FileChallengeQuery struct {
	Challenges []FileChallenge `json:"files"`
	ID         string          `json:"challenge_id"`
}

type FileChallengeResponse struct {
	Challenges []FileChallengeAnswer `json:"answers"`
	ID         string                `json:"challenge_id"`
}

func NewClient(dockerCommand command.Command, client *http.Client) *Client {
	return &Client{
		dockerCommand: dockerCommand,
		client:        client,
	}
}

func (c *Client) PostPushStart(ctx context.Context, pushID string, buildTime time.Duration) error {
	jsonBody := map[string]any{
		"push_id":         pushID,
		"build_duration":  types.Duration(buildTime).String(),
		"push_start_time": time.Now().UTC(),
	}

	jsonData, err := json.Marshal(jsonBody)
	if err != nil {
		return util.WrapError(err, "failed to marshal JSON for build start")
	}

	url := webBaseURL()
	url.Path = pushStartURLPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return util.WrapError(ErrorBadResponsePushStartEndpoint, strconv.Itoa(resp.StatusCode))
	}

	return nil
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
		return util.WrapError(ErrorBadResponseNewVersionEndpoint, strconv.Itoa(resp.StatusCode))
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

	cogVersion := ""
	torchVersion := ""
	cudaVersion := ""
	cudnnVersion := ""
	pythonVersion := ""
	for _, env := range manifest.Config.Env {
		envName, envValue, found := strings.Cut(env, "=")
		if !found {
			continue
		}
		switch envName {
		case command.R8CogVersionEnvVarName:
			cogVersion = envValue
		case command.R8TorchVersionEnvVarName:
			torchVersion = envValue
		case command.R8CudaVersionEnvVarName:
			cudaVersion = envValue
		case command.R8CudnnVersionEnvVarName:
			cudnnVersion = envValue
		case command.R8PythonVersionEnvVarName:
			pythonVersion = envValue
		}
	}

	env := Env{
		CogGpu:              strconv.Itoa(cogGPU),
		CogPredictTypeStub:  cogConfig.Predict,
		CogTrainTypeStub:    cogConfig.Train,
		CogPredictCodeStrip: predictCode,
		CogTrainCodeStrip:   trainCode,
		R8CogVersion:        cogVersion,
		R8CudaVersion:       cudaVersion,
		R8CudnnVersion:      cudnnVersion,
		R8PythonVersion:     pythonVersion,
		R8TorchVersion:      torchVersion,
	}

	prefixedFiles := make([]File, len(files))

	for i, file := range files {
		prefixedFiles[i] = File{
			Path:   file.Path,
			Digest: "sha256:" + file.Digest,
			Size:   file.Size,
		}
	}

	prefixedWeights := make([]File, len(weights))

	for i, file := range weights {
		prefixedWeights[i] = File{
			Path:   file.Path,
			Digest: "sha256:" + file.Digest,
			Size:   file.Size,
		}
	}

	runtimeConfig := RuntimeConfig{
		Weights: prefixedWeights,
		Files:   prefixedFiles,
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

	if pushID, ok := manifest.Config.Labels["run.cog.push_id"]; ok {
		version.PushID = pushID
	}

	return &version, nil
}

func newVersionURL(image string) (url.URL, error) {
	imageComponents := strings.Split(image, "/")
	newVersionUrl := webBaseURL()
	if len(imageComponents) != 3 {
		return newVersionUrl, ErrorBadRegistryURL
	}
	if imageComponents[0] != global.ReplicateRegistryHost {
		return newVersionUrl, ErrorBadRegistryHost
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

func (c *Client) InitiateAndDoFileChallenge(ctx context.Context, runtimeConfig RuntimeConfig) (FileChallengeResponse, error) {
	var challengeResponse FileChallengeResponse
	jsonData, err := json.Marshal(runtimeConfig)
	if err != nil {
		return challengeResponse, util.WrapError(err, "marshal runtime config JSON")
	}
	initiateChallengePath := webBaseURL()
	initiateChallengePath.Path = initiateChallengeURLPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, initiateChallengePath.String(), bytes.NewReader(jsonData))
	if err != nil {
		return challengeResponse, util.WrapError(err, "build HTTP request")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return challengeResponse, util.WrapError(err, "do HTTP request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return challengeResponse, util.WrapError(ErrorBadResponseInitiateChallengeEndpoint, strconv.Itoa(resp.StatusCode))
	}

	var query FileChallengeQuery
	err = json.NewDecoder(resp.Body).Decode(&query)
	if err != nil {
		return challengeResponse, util.WrapError(err, "decode response body")
	}

	return doFileChallenges(query, runtimeConfig)
}

// doFileChallenges does file challenges when requested by the
func doFileChallenges(fileChallenges FileChallengeQuery, runtimeConfig RuntimeConfig) (FileChallengeResponse, error) {
	// Build mapping from file to path
	digestPathMap := make(map[string]string)
	for _, file := range runtimeConfig.Files {
		digestPathMap[file.Digest] = file.Path
	}
	for _, file := range runtimeConfig.Weights {
		digestPathMap[file.Digest] = file.Path
	}

	var wg errgroup.Group
	answerMap := make(map[string]string)
	for _, challenge := range fileChallenges.Challenges {
		wg.Go(func() error {
			wrapString := fmt.Sprintf("file challenge for digest %s:", challenge.Digest)
			file, ok := digestPathMap[challenge.Digest]
			if !ok {
				return util.WrapError(ErrorNoSuchDigest, wrapString)
			}
			ans, err := util.SHA256HashFileWithSaltAndRange(file, challenge.Start, challenge.End, challenge.Salt)
			if err != nil {
				return util.WrapError(err, wrapString)
			}
			answerMap[challenge.Digest] = ans
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		return FileChallengeResponse{}, util.WrapError(err, "do file challenges")
	}
	response := FileChallengeResponse{
		ID: fileChallenges.ID,
	}
	for digest, answer := range answerMap {
		response.Challenges = append(response.Challenges, FileChallengeAnswer{
			Digest: digest,
			Hash:   answer,
		})
	}
	return response, nil
}
