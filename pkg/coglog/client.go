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

type BuildLogContext struct {
	started    time.Time
	fast       bool
	localImage bool
}

type PushLogContext struct {
	started    time.Time
	fast       bool
	localImage bool
}

type buildLog struct {
	DurationMs float32 `json:"length_ms"`
	BuildError *string `json:"error"`
	Fast       bool    `json:"fast"`
	LocalImage bool    `json:"local_image"`
}

type pushLog struct {
	DurationMs float32 `json:"length_ms"`
	BuildError *string `json:"error"`
	Fast       bool    `json:"fast"`
	LocalImage bool    `json:"local_image"`
}

func NewClient(client *http.Client) *Client {
	return &Client{
		client: client,
	}
}

func (c *Client) StartBuild(fast bool, localImage bool) BuildLogContext {
	logContext := BuildLogContext{
		started:    time.Now(),
		fast:       fast,
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
		DurationMs: float32(time.Now().Sub(logContext.started).Milliseconds()),
		BuildError: errorStr,
		Fast:       logContext.fast,
		LocalImage: logContext.localImage,
	}

	jsonData, err := json.Marshal(buildLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData)
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) StartPush(fast bool, localImage bool) PushLogContext {
	logContext := PushLogContext{
		started:    time.Now(),
		fast:       fast,
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
		DurationMs: float32(time.Now().Sub(logContext.started).Milliseconds()),
		BuildError: errorStr,
		Fast:       logContext.fast,
		LocalImage: logContext.localImage,
	}

	jsonData, err := json.Marshal(pushLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	err = c.postLog(ctx, jsonData)
	if err != nil {
		console.Warn(err.Error())
		return false
	}

	return true
}

func (c *Client) postLog(ctx context.Context, jsonData []byte) error {
	disabled, err := DisableFromEnvironment()
	if err != nil {
		return err
	}
	if disabled {
		return nil
	}

	url := buildURL()
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

func buildURL() url.URL {
	url := baseURL()
	url.Path = strings.Join([]string{"", "v1", "build"}, "/")
	return url
}
