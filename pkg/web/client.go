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
	"sync"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/replicate/go/types"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/env"
	r8_errors "github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

const (
	pushStartURLPath      = "/api/models/push-start"
	startChallengeURLPath = "/api/models/file-challenge"
)

var (
	ErrorBadResponseNewVersionEndpoint        = errors.New("Bad response from new version endpoint")
	ErrorBadResponsePushStartEndpoint         = errors.New("Bad response from push start endpoint")
	ErrorBadResponseInitiateChallengeEndpoint = errors.New("Bad response from start file challenge endpoint")
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
	Annotations   map[string]string     `json:"annotations"`
	CogConfig     config.Config         `json:"cog_config"`
	CogVersion    string                `json:"cog_version"`
	OpenAPISchema map[string]any        `json:"openapi_schema"`
	RuntimeConfig RuntimeConfig         `json:"runtime_config"`
	Virtual       bool                  `json:"virtual"`
	PushID        string                `json:"push_id"`
	Challenges    []FileChallengeAnswer `json:"file_challenges"`
}

type FileChallengeRequest struct {
	Digest   string `json:"digest"`
	FileType string `json:"file_type"`
}

type FileChallenge struct {
	Salt   string `json:"salt"`
	Start  int    `json:"byte_start"`
	End    int    `json:"byte_end"`
	Digest string `json:"digest"`
	ID     string `json:"challenge_id"`
}

type FileChallengeAnswer struct {
	Digest      string `json:"digest"`
	Hash        string `json:"hash"`
	ChallengeID string `json:"challenge_id"`
}

type VersionError struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer"`
}

type VersionErrors struct {
	Detail string         `json:"detail"`
	Errors []VersionError `json:"errors"`
	Status int            `json:"status"`
	Title  string         `json:"title"`
}

type VersionCreate struct {
	Version string `json:"version"`
}

type CogKey struct {
	Key       string `json:"key"`
	ExpiresAt string `json:"expires_at"`
}

type Keys struct {
	Cog CogKey `json:"cog"`
}

type TokenData struct {
	Keys Keys `json:"keys"`
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

func (c *Client) PostNewVersion(ctx context.Context, image string, weights []File, files []File, fileChallenges []FileChallengeAnswer) error {
	version, err := c.versionFromManifest(ctx, image, weights, files, fileChallenges)
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
	decoder := json.NewDecoder(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusBadRequest {
			var versionErrors VersionErrors
			err = decoder.Decode(&versionErrors)
			if err != nil {
				return err
			}
			return errors.New(versionErrors.Detail)
		}
		return util.WrapError(ErrorBadResponseNewVersionEndpoint, strconv.Itoa(resp.StatusCode))
	}

	var versionCreate VersionCreate
	err = decoder.Decode(&versionCreate)
	if err != nil {
		return err
	}
	console.Infof("New Version: %s", versionCreate.Version)

	return nil
}

func (c *Client) FetchAPIToken(ctx context.Context, entity string) (string, error) {
	tokenUrl := tokenURL(entity)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenUrl.String(), nil)
	if err != nil {
		return "", err
	}

	tokenResp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Bad response: %s attempting to exchange tokens", strconv.Itoa(tokenResp.StatusCode))
	}

	var tokenData TokenData
	err = json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	if err != nil {
		return "", err
	}

	return tokenData.Keys.Cog.Key, nil
}

func (c *Client) versionFromManifest(ctx context.Context, image string, weights []File, files []File, fileChallenges []FileChallengeAnswer) (*Version, error) {
	manifest, err := c.dockerCommand.Inspect(ctx, image)
	if err != nil {
		return nil, util.WrapError(err, "failed to inspect docker image")
	}

	cogConfig, err := readCogConfig(manifest)
	if err != nil {
		return nil, err
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

	// Digests should match whatever digest we are sending in as the
	// runtime config digests
	for i, challenge := range fileChallenges {
		fileChallenges[i] = FileChallengeAnswer{
			Digest:      fmt.Sprintf("sha256:%s", challenge.Digest),
			Hash:        challenge.Hash,
			ChallengeID: challenge.ChallengeID,
		}
	}

	runtimeConfig := RuntimeConfig{
		Weights: prefixedWeights,
		Files:   prefixedFiles,
		Env:     env,
	}

	version := Version{
		Annotations:   manifest.Config.Labels,
		CogConfig:     *cogConfig,
		CogVersion:    manifest.Config.Labels[command.CogVersionLabelKey],
		OpenAPISchema: openAPISchema,
		RuntimeConfig: runtimeConfig,
		Virtual:       true,
		Challenges:    fileChallenges,
	}

	if pushID, ok := manifest.Config.Labels["run.cog.push_id"]; ok {
		version.PushID = pushID
	}

	return &version, nil
}

func (c *Client) InitiateAndDoFileChallenge(ctx context.Context, weights []File, files []File) ([]FileChallengeAnswer, error) {
	var challengeAnswers []FileChallengeAnswer
	var mu sync.Mutex

	var wg errgroup.Group
	for _, item := range files {
		wg.Go(func() error {
			answer, err := c.doSingleFileChallenge(ctx, item, "files")
			if err != nil {
				return util.WrapError(err, fmt.Sprintf("do file challenge for digest %s", item.Digest))
			}
			mu.Lock()
			challengeAnswers = append(challengeAnswers, answer)
			mu.Unlock()
			return nil
		})
	}
	for _, item := range weights {
		wg.Go(func() error {
			answer, err := c.doSingleFileChallenge(ctx, item, "weights")
			if err != nil {
				return util.WrapError(err, fmt.Sprintf("do file challenge for digest %s", item.Digest))
			}
			mu.Lock()
			challengeAnswers = append(challengeAnswers, answer)
			mu.Unlock()
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		return nil, util.WrapError(err, "do file challenges")
	}

	return challengeAnswers, nil
}

// doSingleFileChallenge does a single file challenge. This is expected to be called in a goroutine.
func (c *Client) doSingleFileChallenge(ctx context.Context, file File, fileType string) (FileChallengeAnswer, error) {
	initiateChallengePath := webBaseURL()
	initiateChallengePath.Path = startChallengeURLPath

	answer := FileChallengeAnswer{}

	jsonData, err := json.Marshal(FileChallengeRequest{
		Digest:   file.Digest,
		FileType: fileType,
	})

	if err != nil {
		return answer, util.WrapError(err, "encode request JSON")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, initiateChallengePath.String(), bytes.NewReader(jsonData))
	if err != nil {
		return answer, util.WrapError(err, "build HTTP request")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return answer, util.WrapError(err, "do HTTP request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return answer, util.WrapError(ErrorBadResponseInitiateChallengeEndpoint, strconv.Itoa(resp.StatusCode))
	}

	var challenge FileChallenge
	err = json.NewDecoder(resp.Body).Decode(&challenge)
	if err != nil {
		return answer, util.WrapError(err, "decode response body")
	}

	ans, err := util.SHA256HashFileWithSaltAndRange(file.Path, challenge.Start, challenge.End, challenge.Salt)
	if err != nil {
		return answer, util.WrapError(err, "hash file")
	}
	return FileChallengeAnswer{
		Digest:      file.Digest,
		Hash:        ans,
		ChallengeID: challenge.ID,
	}, nil
}

func newVersionURL(image string) (url.URL, error) {
	imageComponents := strings.Split(image, "/")
	newVersionUrl := webBaseURL()
	if len(imageComponents) != 3 {
		return newVersionUrl, r8_errors.ErrorBadRegistryURL
	}
	if imageComponents[0] != global.ReplicateRegistryHost {
		return newVersionUrl, r8_errors.ErrorBadRegistryHost
	}
	newVersionUrl.Path = strings.Join([]string{"", "api", "models", imageComponents[1], imageComponents[2], "versions"}, "/")
	return newVersionUrl, nil
}

func tokenURL(entity string) url.URL {
	newVersionUrl := webBaseURL()
	newVersionUrl.Path = strings.Join([]string{"", "api", "token", entity}, "/")
	return newVersionUrl
}

func webBaseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   env.WebHostFromEnvironment(),
	}
}

func codeFileName(cogConfig *config.Config, isPredict bool) (string, error) {
	var stubComponents []string
	if isPredict {
		if cogConfig.Predict == "" {
			return "", nil
		}
		stubComponents = strings.Split(cogConfig.Predict, ":")
	} else {
		if cogConfig.Train == "" {
			return "", nil
		}
		stubComponents = strings.Split(cogConfig.Train, ":")
	}

	if len(stubComponents) < 2 {
		return "", errors.New("Code stub components has less than 2 entries.")
	}

	return stubComponents[0], nil
}

func readCode(cogConfig *config.Config, isPredict bool) (string, string, error) {
	codeFile, err := codeFileName(cogConfig, isPredict)
	if err != nil {
		return "", codeFile, err
	}
	if codeFile == "" {
		return "", "", nil
	}

	b, err := os.ReadFile(codeFile)
	if err != nil {
		return "", codeFile, err
	}

	return string(b), codeFile, nil
}

func stripCodeFromStub(cogConfig *config.Config, isPredict bool) (string, error) {
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

	code, _, err := readCode(cogConfig, isPredict)
	return code, err
}

func readCogConfig(manifest *image.InspectResponse) (*config.Config, error) {
	var cogConfig config.Config
	err := json.Unmarshal([]byte(manifest.Config.Labels[command.CogConfigLabelKey]), &cogConfig)
	if err != nil {
		return nil, util.WrapError(err, "failed to get cog config from docker image")
	}

	return &cogConfig, nil
}
