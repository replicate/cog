package coglog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/util/console"
)

type Client struct {
	client *http.Client
}

type buildLog struct {
	DurationMs float32 `json:"length_ms"`
	BuildError *string `json:"error"`
	Fast       bool    `json:"fast"`
	CogRuntime bool    `json:"cog_runtime"`
	LocalImage bool    `json:"local_image"`
}

type pushLog struct {
	DurationMs float32 `json:"length_ms"`
	BuildError *string `json:"error"`
	Fast       bool    `json:"fast"`
	CogRuntime bool    `json:"cog_runtime"`
	LocalImage bool    `json:"local_image"`
}

type migrateLog struct {
	DurationMs          float32 `json:"length_ms"`
	BuildError          *string `json:"error"`
	Accept              bool    `json:"accept"`
	PythonPackageStatus string  `json:"python_package_status"`
	RunStatus           string  `json:"run_status"`
	PythonPredictStatus string  `json:"python_predict_status"`
	PythonTrainStatus   string  `json:"python_train_status"`
}

type pullLog struct {
	DurationMs float32 `json:"length_ms"`
	BuildError *string `json:"error"`
}

func NewClient(client *http.Client) *Client {
	return &Client{
		client: client,
	}
}

func (c *Client) StartBuild(localImage bool) BuildLogContext {
	logContext := BuildLogContext{
		started:    time.Now(),
		localImage: localImage,
	}
	return logContext
}

func (c *Client) EndBuild(ctx context.Context, err error, logContext BuildLogContext) bool {
	var errorStr *string = nil
	if err != nil {
		errStr := err.Error()
		errorStr = &errStr
	}
	buildLog := buildLog{
		DurationMs: float32(time.Since(logContext.started).Milliseconds()),
		BuildError: errorStr,
		Fast:       logContext.Fast,
		CogRuntime: logContext.CogRuntime,
		LocalImage: logContext.localImage,
	}

	jsonData, err := json.Marshal(buildLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData, "build")
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) StartPush(localImage bool) PushLogContext {
	logContext := PushLogContext{
		started:    time.Now(),
		localImage: localImage,
	}
	return logContext
}

func (c *Client) EndPush(ctx context.Context, err error, logContext PushLogContext) bool {
	var errorStr *string = nil
	if err != nil {
		errStr := err.Error()
		errorStr = &errStr
	}
	pushLog := pushLog{
		DurationMs: float32(time.Since(logContext.started).Milliseconds()),
		BuildError: errorStr,
		Fast:       logContext.Fast,
		CogRuntime: logContext.CogRuntime,
		LocalImage: logContext.localImage,
	}

	jsonData, err := json.Marshal(pushLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData, "push")
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) StartMigrate(accept bool) *MigrateLogContext {
	logContext := NewMigrateLogContext(accept)
	return logContext
}

func (c *Client) EndMigrate(ctx context.Context, err error, logContext *MigrateLogContext) bool {
	var errorStr *string = nil
	if err != nil {
		errStr := err.Error()
		errorStr = &errStr
	}
	migrateLog := migrateLog{
		DurationMs:          float32(time.Since(logContext.started).Milliseconds()),
		BuildError:          errorStr,
		Accept:              logContext.accept,
		PythonPackageStatus: logContext.PythonPackageStatus,
		RunStatus:           logContext.RunStatus,
		PythonPredictStatus: logContext.PythonPredictStatus,
		PythonTrainStatus:   logContext.PythonTrainStatus,
	}

	jsonData, err := json.Marshal(migrateLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData, "migrate")
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) StartPull() PullLogContext {
	logContext := PullLogContext{
		started: time.Now(),
	}
	return logContext
}

func (c *Client) EndPull(ctx context.Context, err error, logContext PullLogContext) bool {
	var errorStr *string = nil
	if err != nil {
		errStr := err.Error()
		errorStr = &errStr
	}
	pushLog := pullLog{
		DurationMs: float32(time.Since(logContext.started).Milliseconds()),
		BuildError: errorStr,
	}

	jsonData, err := json.Marshal(pushLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData, "pull")
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) postLog(ctx context.Context, jsonData []byte, action string) error {
	disabled, err := DisableFromEnvironment()
	if err != nil {
		return err
	}
	if disabled {
		return errors.New("Cog logging disabled")
	}

	url := actionURL(action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url.String(), bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("Bad response from build log: " + strconv.Itoa(resp.StatusCode))
	}
	return nil
}

func baseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   HostFromEnvironment(),
	}
}

func actionURL(action string) url.URL {
	url := baseURL()
	url.Path = strings.Join([]string{"", "v1", action}, "/")
	return url
}
