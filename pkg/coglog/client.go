package coglog

import (
	"bytes"
	"context"
	"encoding/json"
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

type buildLog struct {
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

	disabled, err := DisableFromEnvironment()
	if err != nil {
		console.Warn("Failed to read coglog disabled environment variable: " + err.Error())
		return false
	}
	if disabled {
		return false
	}

	jsonData, err := json.Marshal(buildLog)
	if err != nil {
		console.Warn("Failed to marshal JSON for build log: " + err.Error())
		return false
	}

	url := buildURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url.String(), bytes.NewReader(jsonData))
	if err != nil {
		console.Warn(err.Error())
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		console.Warn(err.Error())
		return false
	}
	if resp.StatusCode != http.StatusOK {
		console.Warn("Bad response from build log: " + strconv.Itoa(resp.StatusCode))
		return false
	}
	return true
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
