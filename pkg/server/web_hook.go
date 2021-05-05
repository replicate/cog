package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

type WebHook struct {
	url    *url.URL
	secret string
}

func newWebHook(urlWithSecret string) (*WebHook, error) {
	splitIndex := strings.LastIndex(urlWithSecret, "@")
	if splitIndex == -1 {
		return nil, fmt.Errorf("Web hooks must be in the format <url>@<secret>")
	}
	rawURL := urlWithSecret[:splitIndex]
	secret := urlWithSecret[splitIndex+1:]

	hookURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &WebHook{url: hookURL, secret: secret}, nil
}

func (wh *WebHook) run(user string, name string, mod *model.Model, dir string, logWriter logger.Logger) error {
	modelJSON, err := json.Marshal(mod)
	if err != nil {
		return err
	}
	modelJSONBase64 := base64.StdEncoding.EncodeToString(modelJSON)
	modelPath := fmt.Sprintf("/v1/repos/%s/%s/models/%s", user, name, mod.ID)
	dockerImageCPU := ""
	if artifact, ok := mod.ArtifactFor(model.TargetDockerCPU); ok {
		dockerImageCPU = artifact.URI
	}
	dockerImageGPU := ""
	if artifact, ok := mod.ArtifactFor(model.TargetDockerGPU); ok {
		dockerImageGPU = artifact.URI
	}

	logWriter.Infof("Posting model to %s", wh.url.Host)

	req, err := http.PostForm(wh.url.String(), url.Values{
		"model_id":          {mod.ID},
		"model_path":        {modelPath},
		"model_json_base64": {modelJSONBase64},
		"docker_image_cpu":  {dockerImageCPU},
		"docker_image_gpu":  {dockerImageGPU},
		"memory_usage":      {strconv.FormatUint(mod.Stats.MemoryUsageCPU, 10)},
		"cpu_usage":         {fmt.Sprintf("%.2f", mod.Stats.CPUUsageCPU)},
		"memory_usage_gpu":  {strconv.FormatUint(mod.Stats.MemoryUsageGPU, 10)},
		"cpu_usage_gpu":     {fmt.Sprintf("%.2f", mod.Stats.CPUUsageGPU)},
		"user":              {user},
		"repo_name":         {name},
		"secret":            {wh.secret},
	})
	if err != nil {
		return fmt.Errorf("Model post failed: %w", err)
	}
	if req.StatusCode != http.StatusOK {
		return fmt.Errorf("Model post failed with HTTP status %d", req.StatusCode)
	}
	return nil
}

func (s *Server) runWebHooks(user string, name string, mod *model.Model, dir string, logWriter logger.Logger) error {
	for _, hook := range s.webHooks {
		if err := hook.run(user, name, mod, dir, logWriter); err != nil {
			return err
		}
	}
	return nil
}
